package config

type BlockchainConfig struct {
	RPCURL                 string
	AuctionContractAddress string
	NFTContractAddress     string
	IPFSGateway            string
}

func NewBlockchainConfig() *BlockchainConfig {
	return &BlockchainConfig{
		RPCURL:                 getEnv("RPC_URL", ""),
		AuctionContractAddress: getEnv("AUCTION_CONTRACT_ADDRESS", ""),
		NFTContractAddress:     getEnv("NFT_CONTRACT_ADDRESS", ""),
		IPFSGateway:            getEnv("IPFS_GATEWAY", "https://gateway.pinata.cloud/ipfs/"),
	}
}
