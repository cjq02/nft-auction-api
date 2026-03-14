// Package indexer 负责链上事件订阅与同步，将事件转化为对 service 的调用或索引表写入。
// 与 service 分层：service 只做业务逻辑与数据访问；indexer 只做「何时触发」（事件驱动）。
package indexer

import (
	"context"
	"log"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nft-auction-api/internal/blockchain"
	"nft-auction-api/internal/model"
	"nft-auction-api/internal/service"
)

// AuctionIndexer 订阅拍卖合约事件，将链上事件同步到 DB（通过调用 service 层）
type AuctionIndexer struct {
	db              *gorm.DB
	auctionService  *service.AuctionService
	bidService      *service.BidService
	listener        *blockchain.EventListener
	contractAddress string
	deployBlock     uint64
}

// NewAuctionIndexer 构建拍卖事件索引器；wsClient 需为 WebSocket (wss://)
func NewAuctionIndexer(
	db *gorm.DB,
	wsClient *blockchain.Client,
	auctionService *service.AuctionService,
	bidService *service.BidService,
	contractAddress string,
	deployBlock uint64,
) *AuctionIndexer {
	if wsClient == nil || !wsClient.IsAvailable() || contractAddress == "" {
		return nil
	}
	idx := &AuctionIndexer{
		db:              db,
		auctionService:  auctionService,
		bidService:      bidService,
		contractAddress: contractAddress,
		deployBlock:     deployBlock,
	}
	handlers := blockchain.EventHandlers{
		OnAuctionCreated:   idx.onAuctionCreated,
		OnBidPlaced:        idx.onBidPlaced,
		OnAuctionEnded:     idx.onAuctionEnded,
		OnAuctionCancelled: idx.onAuctionCancelled,
		OnFeeCollected:     idx.onFeeCollected,
	}
	idx.listener = blockchain.NewEventListener(wsClient, contractAddress, handlers)
	return idx
}

func (i *AuctionIndexer) IsAvailable() bool {
	return i != nil && i.listener != nil
}

// Start 阻塞直到 ctx 取消，应在 goroutine 中调用
func (i *AuctionIndexer) Start(ctx context.Context) {
	if !i.IsAvailable() {
		log.Printf("[auction_indexer] WebSocket listener not available, skipping")
		return
	}
	fromBlock := i.loadCheckpoint()
	log.Printf("[auction_indexer] starting fromBlock=%d", fromBlock)
	i.listener.Run(ctx, fromBlock, i.saveCheckpoint)
}

func (i *AuctionIndexer) loadCheckpoint() uint64 {
	var state model.IndexerState
	if err := i.db.Where("contract_address = ?", i.contractAddress).First(&state).Error; err != nil {
		return i.deployBlock
	}
	if state.LastIndexedBlock == 0 {
		return i.deployBlock
	}
	return state.LastIndexedBlock + 1
}

func (i *AuctionIndexer) saveCheckpoint(block uint64) {
	state := model.IndexerState{ContractAddress: i.contractAddress, LastIndexedBlock: block}
	if err := i.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "contract_address"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_indexed_block"}),
	}).Create(&state).Error; err != nil {
		log.Printf("[auction_indexer] saveCheckpoint failed block=%d err=%v", block, err)
	}
}

func (i *AuctionIndexer) onAuctionCreated(ctx context.Context, e blockchain.AuctionCreatedEvent) {
	log.Printf("[auction_indexer] AuctionCreated auctionId=%d seller=%s block=%d",
		e.AuctionID, e.Seller.Hex(), e.BlockNumber)
	if _, err := i.auctionService.IndexFromAuctionID(ctx, i.contractAddress, e.AuctionID); err != nil {
		log.Printf("[auction_indexer] AuctionCreated index_failed auctionId=%d err=%v", e.AuctionID, err)
	}
}

func (i *AuctionIndexer) onBidPlaced(ctx context.Context, e blockchain.BidPlacedEvent) {
	log.Printf("[auction_indexer] BidPlaced auctionId=%d bidder=%s amount=%s isETH=%v block=%d",
		e.AuctionID, e.Bidder.Hex(), e.Amount.String(), e.IsETH, e.BlockNumber)
	var existing model.BidIndex
	if err := i.db.Where("tx_hash = ?", e.TxHash).First(&existing).Error; err == nil {
		return
	}
	bid := &model.BidIndex{
		AuctionContract: i.contractAddress,
		AuctionID:       e.AuctionID,
		Bidder:          e.Bidder.Hex(),
		Amount:          e.Amount.String(),
		BidTimestamp:    e.BlockTime,
		IsETH:           e.IsETH,
		TxHash:          e.TxHash,
	}
	if err := i.bidService.CreateIndex(bid); err != nil {
		log.Printf("[auction_indexer] BidPlaced create_failed auctionId=%d err=%v", e.AuctionID, err)
	}
}

func (i *AuctionIndexer) onAuctionEnded(ctx context.Context, e blockchain.AuctionEndedEvent) {
	log.Printf("[auction_indexer] AuctionEnded auctionId=%d winner=%s block=%d",
		e.AuctionID, e.Winner.Hex(), e.BlockNumber)
	if err := i.auctionService.UpdateStatus(i.contractAddress, e.AuctionID, model.AuctionStatusEnded); err != nil {
		log.Printf("[auction_indexer] AuctionEnded update_failed auctionId=%d err=%v", e.AuctionID, err)
	}
}

func (i *AuctionIndexer) onAuctionCancelled(ctx context.Context, e blockchain.AuctionCancelledEvent) {
	log.Printf("[auction_indexer] AuctionCancelled auctionId=%d block=%d", e.AuctionID, e.BlockNumber)
	if err := i.auctionService.UpdateStatus(i.contractAddress, e.AuctionID, model.AuctionStatusCancelled); err != nil {
		log.Printf("[auction_indexer] AuctionCancelled update_failed auctionId=%d err=%v", e.AuctionID, err)
	}
}

func (i *AuctionIndexer) onFeeCollected(ctx context.Context, e blockchain.FeeCollectedEvent) {
	log.Printf("[auction_indexer] FeeCollected auctionId=%d recipient=%s amount=%s isETH=%v feeRateBps=%d block=%d",
		e.AuctionID, e.Recipient.Hex(), e.Amount.String(), e.IsETH, e.FeeRateBps, e.BlockNumber)
	if err := i.auctionService.UpdateFeeCollected(i.contractAddress, e.AuctionID, e.Amount.String(), e.IsETH, e.FeeRateBps); err != nil {
		log.Printf("[auction_indexer] FeeCollected update_failed auctionId=%d err=%v", e.AuctionID, err)
	}
}

var _ = time.Second
