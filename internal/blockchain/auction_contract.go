package blockchain

import (
	"bytes"
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
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

const iauctionABI = `[{"inputs":[{"internalType":"uint256","name":"auctionId","type":"uint256"}],"name":"getAuction","outputs":[{"components":[{"internalType":"address","name":"seller","type":"address"},{"internalType":"address","name":"nftContract","type":"address"},{"internalType":"uint256","name":"tokenId","type":"uint256"},{"internalType":"uint256","name":"startTime","type":"uint256"},{"internalType":"uint256","name":"endTime","type":"uint256"},{"internalType":"uint256","name":"minBid","type":"uint256"},{"internalType":"address","name":"paymentToken","type":"address"},{"internalType":"uint8","name":"status","type":"uint8"}],"internalType":"struct IAuction.AuctionInfo","name":"","type":"tuple"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"auctionId","type":"uint256"}],"name":"getHighestBid","outputs":[{"components":[{"internalType":"address","name":"bidder","type":"address"},{"internalType":"uint256","name":"amount","type":"uint256"},{"internalType":"uint256","name":"timestamp","type":"uint256"},{"internalType":"bool","name":"isETH","type":"bool"}],"internalType":"struct IAuction.Bid","name":"","type":"tuple"}],"stateMutability":"view","type":"function"}]`

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
