package config

import "strconv"

type BlockchainConfig struct {
	RPCURL                 string
	AuctionContractAddress string
	NFTContractAddress     string
	IPFSGateway            string
	// AuctionDeployBlock 拍卖合约部署区块；设置后 backfill 只从该块扫到最新，减少请求与限流
	AuctionDeployBlock uint64
}

func NewBlockchainConfig() *BlockchainConfig {
	block, _ := strconv.ParseUint(getEnv("AUCTION_DEPLOY_BLOCK", "0"), 10, 64)
	return &BlockchainConfig{
		RPCURL:                 getEnv("RPC_URL", ""),
		AuctionContractAddress: getEnv("AUCTION_CONTRACT_ADDRESS", ""),
		NFTContractAddress:     getEnv("NFT_CONTRACT_ADDRESS", ""),
		IPFSGateway:            getEnv("IPFS_GATEWAY", "https://gateway.pinata.cloud/ipfs/"),
		AuctionDeployBlock:     block,
	}
}
