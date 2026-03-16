package indexer

import (
	"context"
	"log"
	"sync"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ethereum/go-ethereum/common"
	"nft-auction-api/internal/blockchain"
	"nft-auction-api/internal/model"
)

// NFTMetadataPrefiller 用于在铸造/销毁时同步 t_nft_metadata 缓存
type NFTMetadataPrefiller interface {
	GetOrFetchMetadata(ctx context.Context, nftContract string, tokenID uint64) (*model.NFTMetadata, error)
	DeleteMetadata(ctx context.Context, nftContract string, tokenID uint64) error
}

// NFTIndexer 订阅 NFT 合约的铸造/销毁/Transfer 事件；维护 t_nft_metadata 与 t_nft_ownership
type NFTIndexer struct {
	db              *gorm.DB
	nftContract     *blockchain.NFTContract
	contractAddress string
	deployBlock     uint64
	metadataPrefill NFTMetadataPrefiller
	listener        *blockchain.NFTEventListener
	backfillOnce    sync.Once
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
		OnTransfer:  idx.onTransfer,
	}
	idx.listener = blockchain.NewNFTEventListener(wsClient, nftContractAddress, handlers)
	return idx
}

func (i *NFTIndexer) IsAvailable() bool {
	return i != nil && i.listener != nil
}

// Start 阻塞直到 ctx 取消，应在 goroutine 中调用；启动前先回填 t_nft_ownership
func (i *NFTIndexer) Start(ctx context.Context) {
	if !i.IsAvailable() {
		log.Printf("[nft_indexer] listener not available, skipping")
		return
	}
	i.backfillOnce.Do(func() { i.backfillOwnership(ctx) })
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

// saveCheckpoint 仅向前推进 checkpoint，避免乱序日志导致 checkpoint 回退进而漏事件
func (i *NFTIndexer) saveCheckpoint(block uint64) {
	res := i.db.Model(&model.IndexerState{}).
		Where("contract_address = ? AND (last_indexed_block < ? OR last_indexed_block = 0)", i.contractAddress, block).
		Update("last_indexed_block", block)
	if res.Error != nil {
		log.Printf("[nft_indexer] saveCheckpoint failed block=%d err=%v", block, res.Error)
		return
	}
	if res.RowsAffected > 0 {
		return
	}
	var state model.IndexerState
	if err := i.db.Where("contract_address = ?", i.contractAddress).First(&state).Error; err != nil {
		state = model.IndexerState{ContractAddress: i.contractAddress, LastIndexedBlock: block}
		_ = i.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "contract_address"}},
			DoUpdates: clause.AssignmentColumns([]string{"last_indexed_block"}),
		}).Create(&state).Error
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

func (i *NFTIndexer) onTransfer(ctx context.Context, from, to common.Address, tokenID uint64) {
	contract := i.contractAddress
	zero := common.Address{}
	if to == zero {
		if err := i.db.Where("nft_contract = ? AND token_id = ?", contract, tokenID).Delete(&model.NftOwnership{}).Error; err != nil {
			log.Printf("[nft_indexer] ownership delete tokenId=%d err=%v", tokenID, err)
		}
		return
	}
	row := model.NftOwnership{
		NftContract:  contract,
		TokenID:      tokenID,
		OwnerAddress: to.Hex(),
	}
	if err := i.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "nft_contract"}, {Name: "token_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"owner_address", "updated_at"}),
	}).Create(&row).Error; err != nil {
		log.Printf("[nft_indexer] ownership upsert tokenId=%d err=%v", tokenID, err)
	}
}

// backfillOwnership 按链上 ownerOf 回填 t_nft_ownership（tokenId 1..nextTokenId-1），仅执行一次
func (i *NFTIndexer) backfillOwnership(ctx context.Context) {
	if i.nftContract == nil {
		return
	}
	next, err := i.nftContract.NextTokenId(ctx, i.contractAddress)
	if err != nil || next == 0 {
		log.Printf("[nft_indexer] backfillOwnership skip: nextTokenId err=%v", err)
		return
	}
	log.Printf("[nft_indexer] backfillOwnership contract=%s maxTokenId=%d", i.contractAddress, next-1)
	for tokenID := uint64(1); tokenID < next; tokenID++ {
		if ctx.Err() != nil {
			return
		}
		owner, err := i.nftContract.OwnerOf(ctx, i.contractAddress, tokenID)
		if err != nil {
			continue
		}
		if owner == (common.Address{}) {
			continue
		}
		row := model.NftOwnership{NftContract: i.contractAddress, TokenID: tokenID, OwnerAddress: owner.Hex()}
		if err := i.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "nft_contract"}, {Name: "token_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"owner_address", "updated_at"}),
		}).Create(&row).Error; err != nil {
			log.Printf("[nft_indexer] backfillOwnership upsert tokenId=%d err=%v", tokenID, err)
		}
	}
	log.Printf("[nft_indexer] backfillOwnership done")
}
