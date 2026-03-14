package blockchain

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// ethPriceFeed()、tokenPriceFeeds(address)、latestRoundData() 的 4 字节 selector
var (
	selectorEthPriceFeed     = crypto.Keccak256([]byte("ethPriceFeed()"))[:4]
	selectorTokenPriceFeeds  = crypto.Keccak256([]byte("tokenPriceFeeds(address)"))[:4]
	selectorLatestRoundData  = crypto.Keccak256([]byte("latestRoundData()"))[:4]
)

// GetMinBidEth 根据拍卖合约的 Chainlink 价格将 minBid（USD，18 位小数）换算为 ETH 展示字符串。
// auctionContractAddr 为该拍卖所在合约地址；client 需与链连通。
func GetMinBidEth(ctx context.Context, client *Client, auctionContractAddr string, minBidUSD *big.Int) (string, error) {
	if client == nil || !client.IsAvailable() || minBidUSD == nil || minBidUSD.Sign() <= 0 {
		return "", fmt.Errorf("invalid client or minBid")
	}
	addr := common.HexToAddress(auctionContractAddr)

	// 1. 读拍卖合约的 ethPriceFeed 地址
	feedAddr, err := callEthPriceFeed(ctx, client, addr)
	if err != nil {
		return "", fmt.Errorf("ethPriceFeed: %w", err)
	}

	// 2. 读 Chainlink 价格（8 位小数）
	price8, err := callLatestRoundData(ctx, client.Client, feedAddr)
	if err != nil {
		return "", fmt.Errorf("latestRoundData: %w", err)
	}
	if price8 == nil || price8.Sign() <= 0 {
		return "", fmt.Errorf("invalid price from feed")
	}

	// 3. minBidEthWei = minBidUSD * 1e8 / price8（与合约 PriceConverter 一致）
	//    minBidUSD 为 18 位小数，price8 为 8 位小数
	oneE8 := big.NewInt(1e8)
	minBidEthWei := new(big.Int).Mul(minBidUSD, oneE8)
	minBidEthWei.Div(minBidEthWei, price8)

	return formatWeiToEth(minBidEthWei), nil
}

func callEthPriceFeed(ctx context.Context, client *Client, auctionContract common.Address) (common.Address, error) {
	msg := ethereum.CallMsg{
		To:   &auctionContract,
		Data: selectorEthPriceFeed,
	}
	result, err := client.CallContract(ctx, msg, nil)
	if err != nil {
		return common.Address{}, err
	}
	if len(result) < 32 {
		return common.Address{}, fmt.Errorf("ethPriceFeed result too short")
	}
	return common.BytesToAddress(result[12:32]), nil
}

func callLatestRoundData(ctx context.Context, client *ethclient.Client, feedAddr common.Address) (*big.Int, error) {
	msg := ethereum.CallMsg{
		To:   &feedAddr,
		Data: selectorLatestRoundData,
	}
	result, err := client.CallContract(ctx, msg, nil)
	if err != nil {
		return nil, err
	}
	// 返回 (uint80, int256, uint256, uint256, uint80)，每槽 32 字节，answer 为第 2 个
	if len(result) < 64 {
		return nil, fmt.Errorf("latestRoundData result too short")
	}
	answer := new(big.Int).SetBytes(result[32:64])
	return answer, nil
}

// GetEthPrice8 读取拍卖合约配置的 ETH/USD 价格（8 位小数）。用于手续费折 USD。
func GetEthPrice8(ctx context.Context, client *Client, auctionContractAddr string) (*big.Int, error) {
	if client == nil || !client.IsAvailable() || auctionContractAddr == "" {
		return nil, fmt.Errorf("invalid client or auction address")
	}
	addr := common.HexToAddress(auctionContractAddr)
	feedAddr, err := callEthPriceFeed(ctx, client, addr)
	if err != nil {
		return nil, err
	}
	return callLatestRoundData(ctx, client.Client, feedAddr)
}

// callTokenPriceFeed 读取拍卖合约的 tokenPriceFeeds(token) 返回预言机地址。
func callTokenPriceFeed(ctx context.Context, client *Client, auctionContract common.Address, tokenAddr string) (common.Address, error) {
	if client == nil || !client.IsAvailable() || tokenAddr == "" {
		return common.Address{}, fmt.Errorf("invalid client or token address")
	}
	token := common.HexToAddress(tokenAddr)
	data := append(selectorTokenPriceFeeds, common.LeftPadBytes(token.Bytes(), 32)...)
	msg := ethereum.CallMsg{
		To:   &auctionContract,
		Data: data,
	}
	result, err := client.CallContract(ctx, msg, nil)
	if err != nil {
		return common.Address{}, err
	}
	if len(result) < 32 {
		return common.Address{}, fmt.Errorf("tokenPriceFeeds result too short")
	}
	return common.BytesToAddress(result[12:32]), nil
}

// GetTokenPrice8 读取拍卖合约配置的某代币 USD 价格（8 位小数）。用于手续费折 USD。
func GetTokenPrice8(ctx context.Context, client *Client, auctionContractAddr, tokenAddr string) (*big.Int, error) {
	if client == nil || !client.IsAvailable() || auctionContractAddr == "" || tokenAddr == "" {
		return nil, fmt.Errorf("invalid client or address")
	}
	addr := common.HexToAddress(auctionContractAddr)
	feedAddr, err := callTokenPriceFeed(ctx, client, addr, tokenAddr)
	if err != nil {
		return nil, err
	}
	return callLatestRoundData(ctx, client.Client, feedAddr)
}

func formatWeiToEth(wei *big.Int) string {
	if wei == nil || wei.Sign() < 0 {
		return "0"
	}
	oneE18 := big.NewInt(1e18)
	div := new(big.Int).Div(wei, oneE18)
	rem := new(big.Int).Rem(wei, oneE18)
	if rem.Sign() == 0 {
		return div.String()
	}
	// 保留最多 6 位小数，去掉尾随 0
	remStr := rem.String()
	for len(remStr) < 18 {
		remStr = "0" + remStr
	}
	if len(remStr) > 18 {
		remStr = remStr[:18]
	}
	for len(remStr) > 0 && remStr[len(remStr)-1] == '0' {
		remStr = remStr[:len(remStr)-1]
	}
	return div.String() + "." + remStr
}
