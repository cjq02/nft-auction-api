package config

import "strconv"

type BlockchainConfig struct {
	RPCURL                 string
	WSRPCUrl               string // WebSocket URL (wss://), required for event listener
	AuctionContractAddress string
	NFTContractAddress     string
	IPFSGateway            string
	// AuctionDeployBlock 拍卖合约部署区块；事件监听器从此块开始补扫，减少首次同步量
	AuctionDeployBlock uint64
}

func NewBlockchainConfig() *BlockchainConfig {
	block, _ := strconv.ParseUint(getEnv("AUCTION_DEPLOY_BLOCK", "0"), 10, 64)
	return &BlockchainConfig{
		RPCURL:                 getEnv("RPC_URL", ""),
		WSRPCUrl:               getEnv("WS_RPC_URL", ""),
		AuctionContractAddress: getEnv("AUCTION_CONTRACT_ADDRESS", ""),
		NFTContractAddress:     getEnv("NFT_CONTRACT_ADDRESS", ""),
		IPFSGateway:            getEnv("IPFS_GATEWAY", "https://gateway.pinata.cloud/ipfs/"),
		AuctionDeployBlock:     block,
	}
}
