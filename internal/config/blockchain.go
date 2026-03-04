package config

import "strconv"

type BlockchainConfig struct {
	RPCURL                 string
	WSRPCUrl               string // WebSocket URL (wss://), required for event listener
	AuctionContractAddress string
	NFTContractAddress     string
	IPFSGateway            string
	// AuctionDeployBlock 拍卖合约部署区块；事件监听器从此块开始补扫
	AuctionDeployBlock uint64
	// NFTDeployBlock NFT 合约部署区块；索引器与已销毁数回填均从此块开始，避免从 0 扫导致 RPC internal error
	NFTDeployBlock uint64
}

func NewBlockchainConfig() *BlockchainConfig {
	auctionBlock, _ := strconv.ParseUint(getEnv("AUCTION_DEPLOY_BLOCK", "0"), 10, 64)
	nftBlock, _ := strconv.ParseUint(getEnv("NFT_DEPLOY_BLOCK", "0"), 10, 64)
	return &BlockchainConfig{
		RPCURL:                 getEnv("RPC_URL", ""),
		WSRPCUrl:               getEnv("WS_RPC_URL", ""),
		AuctionContractAddress: getEnv("AUCTION_CONTRACT_ADDRESS", ""),
		NFTContractAddress:     getEnv("NFT_CONTRACT_ADDRESS", ""),
		IPFSGateway:            getEnv("IPFS_GATEWAY", "https://gateway.pinata.cloud/ipfs/"),
		AuctionDeployBlock:     auctionBlock,
		NFTDeployBlock:         nftBlock,
	}
}
