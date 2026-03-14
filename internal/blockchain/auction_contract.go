package blockchain

import (
	"bytes"
	"context"
	"fmt"
	"log"
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

const iauctionABI = `[{"inputs":[{"internalType":"uint256","name":"auctionId","type":"uint256"}],"name":"getAuction","outputs":[{"components":[{"internalType":"address","name":"seller","type":"address"},{"internalType":"address","name":"nftContract","type":"address"},{"internalType":"uint256","name":"tokenId","type":"uint256"},{"internalType":"uint256","name":"startTime","type":"uint256"},{"internalType":"uint256","name":"endTime","type":"uint256"},{"internalType":"uint256","name":"minBid","type":"uint256"},{"internalType":"address","name":"paymentToken","type":"address"},{"internalType":"uint8","name":"status","type":"uint8"}],"internalType":"struct IAuction.AuctionInfo","name":"","type":"tuple"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"auctionId","type":"uint256"}],"name":"getHighestBid","outputs":[{"components":[{"internalType":"address","name":"bidder","type":"address"},{"internalType":"uint256","name":"amount","type":"uint256"},{"internalType":"uint256","name":"timestamp","type":"uint256"},{"internalType":"bool","name":"isETH","type":"bool"}],"internalType":"struct IAuction.Bid","name":"","type":"tuple"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"feeRate","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"feeRecipient","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"uint256","name":"auctionId","type":"uint256"},{"indexed":true,"internalType":"address","name":"seller","type":"address"},{"indexed":true,"internalType":"address","name":"nftContract","type":"address"},{"indexed":false,"internalType":"uint256","name":"tokenId","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"startTime","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"endTime","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"minBid","type":"uint256"}],"name":"AuctionCreated","type":"event"}]`

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

// getAuctionResultLen  getAuction 返回一个 tuple，8 个槽位各 32 字节
const getAuctionResultLen = 8 * 32

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
	if len(result) < getAuctionResultLen {
		return nil, fmt.Errorf("getAuction result too short: got %d bytes", len(result))
	}

	// 手动解析 tuple(seller, nftContract, tokenId, startTime, endTime, minBid, paymentToken, status)
	// 避免 UnpackIntoInterface 对单输出 tuple 的 reflect 问题
	info := &AuctionInfo{
		Seller:       common.BytesToAddress(result[0:32][12:32]),
		NFTContract:  common.BytesToAddress(result[32:64][12:32]),
		TokenID:      new(big.Int).SetBytes(result[64:96]),
		StartTime:    new(big.Int).SetBytes(result[96:128]),
		EndTime:      new(big.Int).SetBytes(result[128:160]),
		MinBid:       new(big.Int).SetBytes(result[160:192]),
		PaymentToken: common.BytesToAddress(result[192:224][12:32]),
		Status:       uint8(result[255]), // 第 8 槽位 uint8 右对齐
	}
	return info, nil
}

// ParseAuctionCreatedFromReceipt fetches the transaction receipt by txHash and parses the
// AuctionCreated event to extract the auctionId.
func (c *AuctionContract) ParseAuctionCreatedFromReceipt(ctx context.Context, txHash string) (uint64, error) {
	if c == nil || c.client == nil {
		log.Printf("[auction_contract] ParseAuctionCreatedFromReceipt: blockchain client not available")
		return 0, fmt.Errorf("blockchain client not available")
	}

	receipt, err := c.client.TransactionReceipt(ctx, common.HexToHash(txHash))
	if err != nil {
		log.Printf("[auction_contract] ParseAuctionCreatedFromReceipt txHash=%s get_receipt_err=%v", txHash, err)
		return 0, fmt.Errorf("failed to get transaction receipt: %w", err)
	}
	log.Printf("[auction_contract] ParseAuctionCreatedFromReceipt txHash=%s receipt_status=%d logs_count=%d contract=%s", txHash, receipt.Status, len(receipt.Logs), c.address.Hex())

	for _, l := range receipt.Logs {
		if l.Address != c.address || len(l.Topics) == 0 {
			continue
		}
		if l.Topics[0] != auctionCreatedTopic {
			continue
		}
		if len(l.Topics) < 2 {
			log.Printf("[auction_contract] ParseAuctionCreatedFromReceipt txHash=%s AuctionCreated log missing auctionId topic", txHash)
			return 0, fmt.Errorf("AuctionCreated log missing auctionId topic")
		}
		auctionID := new(big.Int).SetBytes(l.Topics[1].Bytes()).Uint64()
		log.Printf("[auction_contract] ParseAuctionCreatedFromReceipt txHash=%s found auctionId=%d", txHash, auctionID)
		return auctionID, nil
	}

	log.Printf("[auction_contract] ParseAuctionCreatedFromReceipt txHash=%s AuctionCreated event not found (logs=%d, contract=%s)", txHash, len(receipt.Logs), c.address.Hex())
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
		log.Printf("[auction_contract] ScanAuctionIDs: blockchain client not available")
		return nil, fmt.Errorf("blockchain client not available")
	}

	toBlock, err := c.client.BlockNumber(ctx)
	if err != nil {
		log.Printf("[auction_contract] ScanAuctionIDs get_block_number_err=%v", err)
		return nil, fmt.Errorf("failed to get block number: %w", err)
	}
	log.Printf("[auction_contract] ScanAuctionIDs start fromBlock=%d toBlock=%d contract=%s", fromBlockStart, toBlock, c.address.Hex())

	fromBlock := fromBlockStart
	var ids []uint64
	seen := make(map[uint64]bool)
	chunkNum := 0

	for fromBlock <= toBlock {
		chunkEnd := fromBlock + scanBlockChunkSize - 1
		if chunkEnd > toBlock {
			chunkEnd = toBlock
		}
		chunkNum++

		query := ethereum.FilterQuery{
			FromBlock: new(big.Int).SetUint64(fromBlock),
			ToBlock:   new(big.Int).SetUint64(chunkEnd),
			Addresses: []common.Address{c.address},
			Topics:    [][]common.Hash{{auctionCreatedTopic}},
		}

		logs, err := c.client.FilterLogs(ctx, query)
		if err != nil {
			log.Printf("[auction_contract] ScanAuctionIDs filter_logs_failed chunk=%d blocks=%d-%d err=%v", chunkNum, fromBlock, chunkEnd, err)
			return nil, fmt.Errorf("failed to filter logs (blocks %d-%d): %w", fromBlock, chunkEnd, err)
		}
		for _, l := range logs {
			if err := c.parseAuctionCreatedLog(l, &ids, seen); err != nil {
				continue
			}
		}
		if len(logs) > 0 {
			log.Printf("[auction_contract] ScanAuctionIDs chunk=%d blocks=%d-%d logs=%d total_ids=%d", chunkNum, fromBlock, chunkEnd, len(logs), len(ids))
		}

		fromBlock = chunkEnd + 1
		if chunkEnd >= toBlock {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(scanChunkDelay):
		}
	}

	log.Printf("[auction_contract] ScanAuctionIDs done fromBlock=%d toBlock=%d chunks=%d total_ids=%d", fromBlockStart, toBlock, chunkNum, len(ids))
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

// GetFeeRate 读取合约当前手续费率（万分之一，如 250 表示 2.5%）。
func (c *AuctionContract) GetFeeRate(ctx context.Context) (*big.Int, error) {
	if c == nil || c.client == nil {
		return nil, nil
	}
	data, err := c.contract.abi.Pack("feeRate")
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
	var rate big.Int
	if err := c.contract.abi.UnpackIntoInterface(&rate, "feeRate", result); err != nil {
		return nil, err
	}
	return &rate, nil
}

// GetFeeRecipient 读取合约手续费接收地址。
func (c *AuctionContract) GetFeeRecipient(ctx context.Context) (common.Address, error) {
	if c == nil || c.client == nil {
		return common.Address{}, fmt.Errorf("blockchain not available")
	}
	data, err := c.contract.abi.Pack("feeRecipient")
	if err != nil {
		return common.Address{}, err
	}
	result, err := c.client.CallContract(ctx, ethereum.CallMsg{
		To:   &c.address,
		Data: data,
	}, nil)
	if err != nil {
		return common.Address{}, err
	}
	var recipient common.Address
	if err := c.contract.abi.UnpackIntoInterface(&recipient, "feeRecipient", result); err != nil {
		return common.Address{}, err
	}
	return recipient, nil
}

// FeeRateBps 手续费率基数（万分之一）
const FeeRateBps = 10000

// ComputeFee 根据成交金额和合约手续费率计算手续费：fee = amount * feeRate / 10000。
// 与合约中 calculateFee 的 V1 逻辑一致（不依赖 isETH/paymentToken）。
func ComputeFee(amount, feeRate *big.Int) *big.Int {
	if amount == nil || feeRate == nil || feeRate.Sign() == 0 {
		return new(big.Int)
	}
	fee := new(big.Int).Mul(amount, feeRate)
	return fee.Div(fee, big.NewInt(FeeRateBps))
}

// GetFeeForAuction 获取某场拍卖的手续费估算（当前费率 × 当前最高出价）。仅读链上，不依赖索引。
// 若拍卖无出价或合约不可用，返回 nil, nil。
func (c *AuctionContract) GetFeeForAuction(ctx context.Context, auctionID uint64) (*big.Int, error) {
	if c == nil || c.client == nil {
		return nil, nil
	}
	bid, err := c.GetHighestBid(ctx, auctionID)
	if err != nil || bid == nil || bid.Amount == nil || bid.Amount.Sign() == 0 {
		return nil, err
	}
	rate, err := c.GetFeeRate(ctx)
	if err != nil || rate == nil {
		return nil, err
	}
	return ComputeFee(bid.Amount, rate), nil
}

// GetMinBidEth 根据指定拍卖合约的 Chainlink 价格将 minBid（USD）换算为 ETH 展示字符串。
// auctionContractAddr 可为任意部署的拍卖合约地址（与 c.address 可不同）。
func (c *AuctionContract) GetMinBidEth(ctx context.Context, auctionContractAddr string, minBidUSD *big.Int) (string, error) {
	if c == nil || c.client == nil {
		return "", fmt.Errorf("blockchain not available")
	}
	return GetMinBidEth(ctx, c.client, auctionContractAddr, minBidUSD)
}

// GetEthPrice8 读取本合约配置的 ETH/USD 价格（Chainlink 8 位小数）。用于手续费折 USD。
func (c *AuctionContract) GetEthPrice8(ctx context.Context) (*big.Int, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("blockchain not available")
	}
	return GetEthPrice8(ctx, c.client, c.address.Hex())
}

// GetTokenPrice8 读取本合约配置的某代币 USD 价格（8 位小数）。用于手续费折 USD。
func (c *AuctionContract) GetTokenPrice8(ctx context.Context, tokenAddr string) (*big.Int, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("blockchain not available")
	}
	return GetTokenPrice8(ctx, c.client, c.address.Hex(), tokenAddr)
}
