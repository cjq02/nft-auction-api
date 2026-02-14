package config

type BlockchainConfig struct {
	RPCURL                 string
	AuctionContractAddress string
	NFTContractAddress     string
}

func NewBlockchainConfig() *BlockchainConfig {
	return &BlockchainConfig{
		RPCURL:                 getEnv("RPC_URL", ""),
		AuctionContractAddress: getEnv("AUCTION_CONTRACT_ADDRESS", ""),
		NFTContractAddress:     getEnv("NFT_CONTRACT_ADDRESS", ""),
	}
}
