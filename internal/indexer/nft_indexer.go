package indexer

import (
	"context"
	"log"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nft-auction-api/internal/blockchain"
	"nft-auction-api/internal/model"
)

// NFTMetadataPrefiller 用于在铸造/销毁时同步 t_nft_metadata 缓存
type NFTMetadataPrefiller interface {
	GetOrFetchMetadata(ctx context.Context, nftContract string, tokenID uint64) (*model.NFTMetadata, error)
	DeleteMetadata(ctx context.Context, nftContract string, tokenID uint64) error
}

// NFTIndexer 订阅 NFT 合约的铸造/销毁事件；铸造时顺带预填 t_nft_metadata
type NFTIndexer struct {
	db              *gorm.DB
	nftContract     *blockchain.NFTContract
	contractAddress string
	deployBlock     uint64
	metadataPrefill NFTMetadataPrefiller
	listener        *blockchain.NFTEventListener
}

// NewNFTIndexer 创建 NFT 事件索引器；wsClient 需为 WebSocket；metadataPrefill 可选，用于铸造时预填元数据缓存
func NewNFTIndexer(
	db *gorm.DB,
	wsClient *blockchain.Client,
	nftContract *blockchain.NFTContract,
	nftContractAddress string,
	deployBlock uint64,
	metadataPrefill NFTMetadataPrefiller,
) *NFTIndexer {
	if wsClient == nil || !wsClient.IsAvailable() || nftContractAddress == "" {
		return nil
	}
	idx := &NFTIndexer{
		db:              db,
		nftContract:     nftContract,
		contractAddress: nftContractAddress,
		deployBlock:     deployBlock,
		metadataPrefill: metadataPrefill,
	}
	handlers := blockchain.NFTEventHandlers{
		OnNFTMinted: idx.onNFTMinted,
		OnNFTBurned: idx.onNFTBurned,
	}
	idx.listener = blockchain.NewNFTEventListener(wsClient, nftContractAddress, handlers)
	return idx
}

func (i *NFTIndexer) IsAvailable() bool {
	return i != nil && i.listener != nil
}

// Start 阻塞直到 ctx 取消，应在 goroutine 中调用
func (i *NFTIndexer) Start(ctx context.Context) {
	if !i.IsAvailable() {
		log.Printf("[nft_indexer] listener not available, skipping")
		return
	}
	fromBlock := i.loadCheckpoint()
	log.Printf("[nft_indexer] starting fromBlock=%d", fromBlock)
	i.listener.Run(ctx, fromBlock, i.saveCheckpoint)
}

func (i *NFTIndexer) loadCheckpoint() uint64 {
	var state model.IndexerState
	if err := i.db.Where("contract_address = ?", i.contractAddress).First(&state).Error; err != nil {
		return i.deployBlock
	}
	if state.LastIndexedBlock == 0 {
		return i.deployBlock
	}
	return state.LastIndexedBlock + 1
}

func (i *NFTIndexer) saveCheckpoint(block uint64) {
	state := model.IndexerState{ContractAddress: i.contractAddress, LastIndexedBlock: block}
	if err := i.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "contract_address"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_indexed_block"}),
	}).Create(&state).Error; err != nil {
		log.Printf("[nft_indexer] saveCheckpoint failed block=%d err=%v", block, err)
	}
}

func (i *NFTIndexer) onNFTMinted(ctx context.Context, e blockchain.NFTMintedEvent) {
	log.Printf("[nft_indexer] NFTMinted tokenId=%d to=%s block=%d", e.TokenID, e.To.Hex(), e.BlockNumber)
	if i.metadataPrefill != nil {
		if _, err := i.metadataPrefill.GetOrFetchMetadata(ctx, i.contractAddress, e.TokenID); err != nil {
			log.Printf("[nft_indexer] metadata prefill tokenId=%d err=%v", e.TokenID, err)
		}
	}
}

func (i *NFTIndexer) onNFTBurned(ctx context.Context, e blockchain.NFTBurnedEvent) {
	log.Printf("[nft_indexer] NFTBurned tokenId=%d block=%d", e.TokenID, e.BlockNumber)
	if i.metadataPrefill != nil {
		if err := i.metadataPrefill.DeleteMetadata(ctx, i.contractAddress, e.TokenID); err != nil {
			log.Printf("[nft_indexer] metadata delete tokenId=%d err=%v", e.TokenID, err)
		}
	}
}
