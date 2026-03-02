package blockchain

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// AuctionInfo 链上拍卖信息（与 IAuction.AuctionInfo 对应）
type AuctionInfo struct {
	Seller       common.Address
	NFTContract  common.Address
	TokenID      *big.Int
	StartTime    *big.Int
	EndTime      *big.Int
	MinBid       *big.Int
	PaymentToken common.Address
	Status       uint8
}

// Bid 链上出价信息（与 IAuction.Bid 对应）
type Bid struct {
	Bidder    common.Address
	Amount    *big.Int
	Timestamp *big.Int
	IsETH     bool
}

// AuctionContract 拍卖合约封装
type AuctionContract struct {
	client   *Client
	address  common.Address
	contract *boundContract
}

type boundContract struct {
	abi abi.ABI
}

const iauctionABI = `[{"inputs":[{"internalType":"uint256","name":"auctionId","type":"uint256"}],"name":"getAuction","outputs":[{"components":[{"internalType":"address","name":"seller","type":"address"},{"internalType":"address","name":"nftContract","type":"address"},{"internalType":"uint256","name":"tokenId","type":"uint256"},{"internalType":"uint256","name":"startTime","type":"uint256"},{"internalType":"uint256","name":"endTime","type":"uint256"},{"internalType":"uint256","name":"minBid","type":"uint256"},{"internalType":"address","name":"paymentToken","type":"address"},{"internalType":"uint8","name":"status","type":"uint8"}],"internalType":"struct IAuction.AuctionInfo","name":"","type":"tuple"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"auctionId","type":"uint256"}],"name":"getHighestBid","outputs":[{"components":[{"internalType":"address","name":"bidder","type":"address"},{"internalType":"uint256","name":"amount","type":"uint256"},{"internalType":"uint256","name":"timestamp","type":"uint256"},{"internalType":"bool","name":"isETH","type":"bool"}],"internalType":"struct IAuction.Bid","name":"","type":"tuple"}],"stateMutability":"view","type":"function"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"uint256","name":"auctionId","type":"uint256"},{"indexed":true,"internalType":"address","name":"seller","type":"address"},{"indexed":true,"internalType":"address","name":"nftContract","type":"address"},{"indexed":false,"internalType":"uint256","name":"tokenId","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"startTime","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"endTime","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"minBid","type":"uint256"}],"name":"AuctionCreated","type":"event"}]`

// auctionCreatedTopic is the keccak256 hash of the AuctionCreated event signature
var auctionCreatedTopic = crypto.Keccak256Hash([]byte("AuctionCreated(uint256,address,address,uint256,uint256,uint256,uint256)"))

func NewAuctionContract(client *Client, contractAddress string) (*AuctionContract, error) {
	if client == nil || !client.IsAvailable() {
		return nil, nil
	}
	if contractAddress == "" {
		return nil, nil
	}

	parsedABI, err := abi.JSON(bytes.NewReader([]byte(iauctionABI)))
	if err != nil {
		return nil, err
	}

	return &AuctionContract{
		client:  client,
		address: common.HexToAddress(contractAddress),
		contract: &boundContract{
			abi: parsedABI,
		},
	}, nil
}

func (c *AuctionContract) GetAuction(ctx context.Context, auctionID uint64) (*AuctionInfo, error) {
	if c == nil || c.client == nil {
		return nil, nil
	}

	data, err := c.contract.abi.Pack("getAuction", new(big.Int).SetUint64(auctionID))
	if err != nil {
		return nil, err
	}

	result, err := c.client.CallContract(ctx, ethereum.CallMsg{
		To:   &c.address,
		Data: data,
	}, nil)
	if err != nil {
		return nil, err
	}

	var info AuctionInfo
	err = c.contract.abi.UnpackIntoInterface(&info, "getAuction", result)
	if err != nil {
		return nil, err
	}

	return &info, nil
}

// ParseAuctionCreatedFromReceipt fetches the transaction receipt by txHash and parses the
// AuctionCreated event to extract the auctionId.
func (c *AuctionContract) ParseAuctionCreatedFromReceipt(ctx context.Context, txHash string) (uint64, error) {
	if c == nil || c.client == nil {
		return 0, fmt.Errorf("blockchain client not available")
	}

	receipt, err := c.client.TransactionReceipt(ctx, common.HexToHash(txHash))
	if err != nil {
		return 0, fmt.Errorf("failed to get transaction receipt: %w", err)
	}

	for _, log := range receipt.Logs {
		// Must be from the auction contract and have at least one topic
		if log.Address != c.address || len(log.Topics) == 0 {
			continue
		}
		if log.Topics[0] != auctionCreatedTopic {
			continue
		}
		// auctionId is the first indexed parameter (Topics[1])
		if len(log.Topics) < 2 {
			return 0, fmt.Errorf("AuctionCreated log missing auctionId topic")
		}
		auctionID := new(big.Int).SetBytes(log.Topics[1].Bytes()).Uint64()
		return auctionID, nil
	}

	return 0, fmt.Errorf("AuctionCreated event not found in transaction %s", txHash)
}

// scanBlockChunkSize 单次 FilterLogs 查询的区块数
const scanBlockChunkSize = 2000

// scanChunkDelay 每段请求之间的间隔，避免 Infura 等 RPC 429 限流
const scanChunkDelay = 400 * time.Millisecond

// ScanAuctionIDs 扫描合约从 fromBlock 到最新块的所有 AuctionCreated 事件，返回所有 auctionId 列表。
// 用于历史数据补录。fromBlock 为 0 时从创世块开始；可设 AUCTION_DEPLOY_BLOCK 减少扫描量。
func (c *AuctionContract) ScanAuctionIDs(ctx context.Context, fromBlockStart uint64) ([]uint64, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("blockchain client not available")
	}

	toBlock, err := c.client.BlockNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get block number: %w", err)
	}

	fromBlock := fromBlockStart
	var ids []uint64
	seen := make(map[uint64]bool)

	for fromBlock <= toBlock {
		chunkEnd := fromBlock + scanBlockChunkSize - 1
		if chunkEnd > toBlock {
			chunkEnd = toBlock
		}

		query := ethereum.FilterQuery{
			FromBlock: new(big.Int).SetUint64(fromBlock),
			ToBlock:   new(big.Int).SetUint64(chunkEnd),
			Addresses: []common.Address{c.address},
			Topics:    [][]common.Hash{{auctionCreatedTopic}},
		}

		logs, err := c.client.FilterLogs(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("failed to filter logs (blocks %d-%d): %w", fromBlock, chunkEnd, err)
		}

		for _, log := range logs {
			if err := c.parseAuctionCreatedLog(log, &ids, seen); err != nil {
				continue
			}
		}

		fromBlock = chunkEnd + 1
		if chunkEnd >= toBlock {
			break
		}
		// 避免 RPC 限流（如 Infura 429）
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(scanChunkDelay):
		}
	}

	return ids, nil
}

func (c *AuctionContract) parseAuctionCreatedLog(log types.Log, ids *[]uint64, seen map[uint64]bool) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("insufficient topics")
	}
	auctionID := new(big.Int).SetBytes(log.Topics[1].Bytes()).Uint64()
	if seen[auctionID] {
		return nil
	}
	seen[auctionID] = true
	*ids = append(*ids, auctionID)
	return nil
}

func (c *AuctionContract) GetHighestBid(ctx context.Context, auctionID uint64) (*Bid, error) {
	if c == nil || c.client == nil {
		return nil, nil
	}

	data, err := c.contract.abi.Pack("getHighestBid", new(big.Int).SetUint64(auctionID))
	if err != nil {
		return nil, err
	}

	result, err := c.client.CallContract(ctx, ethereum.CallMsg{
		To:   &c.address,
		Data: data,
	}, nil)
	if err != nil {
		return nil, err
	}

	var bid Bid
	err = c.contract.abi.UnpackIntoInterface(&bid, "getHighestBid", result)
	if err != nil {
		return nil, err
	}

	return &bid, nil
}
