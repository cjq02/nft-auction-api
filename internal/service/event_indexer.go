package service

import (
	"context"
	"log"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nft-auction-api/internal/blockchain"
	"nft-auction-api/internal/model"
)

// EventIndexer wires the blockchain EventListener to the database.
// It handles AuctionCreated, BidPlaced, AuctionEnded, and AuctionCancelled events.
type EventIndexer struct {
	db             *gorm.DB
	auctionService *AuctionService
	listener       *blockchain.EventListener
	deployBlock    uint64
}

// NewEventIndexer builds the indexer and configures event handlers.
// wsClient must be a WebSocket client (wss://); if nil the indexer is a no-op.
func NewEventIndexer(
	db *gorm.DB,
	wsClient *blockchain.Client,
	auctionService *AuctionService,
	contractAddress string,
	deployBlock uint64,
) *EventIndexer {
	idx := &EventIndexer{
		db:             db,
		auctionService: auctionService,
		deployBlock:    deployBlock,
	}

	handlers := blockchain.EventHandlers{
		OnAuctionCreated:   idx.onAuctionCreated,
		OnBidPlaced:        idx.onBidPlaced,
		OnAuctionEnded:     idx.onAuctionEnded,
		OnAuctionCancelled: idx.onAuctionCancelled,
	}
	idx.listener = blockchain.NewEventListener(wsClient, contractAddress, handlers)
	return idx
}

// IsAvailable reports whether the underlying WebSocket listener is ready.
func (i *EventIndexer) IsAvailable() bool {
	return i != nil && i.listener != nil
}

// Start loads the persisted checkpoint and runs the listener.
// It blocks until ctx is cancelled; call it in a goroutine.
func (i *EventIndexer) Start(ctx context.Context) {
	if !i.IsAvailable() {
		log.Printf("[event_indexer] WebSocket listener not available, skipping")
		return
	}
	fromBlock := i.loadCheckpoint()
	log.Printf("[event_indexer] starting fromBlock=%d", fromBlock)
	i.listener.Run(ctx, fromBlock, i.saveCheckpoint)
}

// -------- checkpoint (t_indexer_state, single row ID=1) --------

func (i *EventIndexer) loadCheckpoint() uint64 {
	var state model.IndexerState
	if err := i.db.First(&state).Error; err != nil {
		// no checkpoint yet — start from deploy block
		return i.deployBlock
	}
	if state.LastIndexedBlock == 0 {
		return i.deployBlock
	}
	// resume from the block AFTER the last fully-processed one
	return state.LastIndexedBlock + 1
}

func (i *EventIndexer) saveCheckpoint(block uint64) {
	state := model.IndexerState{ID: 1, LastIndexedBlock: block}
	if err := i.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_indexed_block"}),
	}).Create(&state).Error; err != nil {
		log.Printf("[event_indexer] saveCheckpoint failed block=%d err=%v", block, err)
	}
}

// -------- event handlers --------

func (i *EventIndexer) onAuctionCreated(ctx context.Context, e blockchain.AuctionCreatedEvent) {
	log.Printf("[event_indexer] AuctionCreated auctionId=%d seller=%s block=%d",
		e.AuctionID, e.Seller.Hex(), e.BlockNumber)

	if _, err := i.auctionService.IndexFromAuctionID(ctx, e.AuctionID); err != nil {
		log.Printf("[event_indexer] AuctionCreated index_failed auctionId=%d err=%v", e.AuctionID, err)
	}
}

func (i *EventIndexer) onBidPlaced(ctx context.Context, e blockchain.BidPlacedEvent) {
	log.Printf("[event_indexer] BidPlaced auctionId=%d bidder=%s amount=%s isETH=%v block=%d",
		e.AuctionID, e.Bidder.Hex(), e.Amount.String(), e.IsETH, e.BlockNumber)

	// idempotency — skip if this tx is already recorded
	var existing model.BidIndex
	if err := i.db.Where("tx_hash = ?", e.TxHash).First(&existing).Error; err == nil {
		return
	}

	bid := &model.BidIndex{
		AuctionID:    e.AuctionID,
		Bidder:       e.Bidder.Hex(),
		Amount:       e.Amount.String(),
		BidTimestamp: e.BlockTime,
		IsETH:        e.IsETH,
		TxHash:       e.TxHash,
	}
	if err := i.db.Create(bid).Error; err != nil {
		log.Printf("[event_indexer] BidPlaced db_create_failed auctionId=%d err=%v", e.AuctionID, err)
	}
}

func (i *EventIndexer) onAuctionEnded(ctx context.Context, e blockchain.AuctionEndedEvent) {
	log.Printf("[event_indexer] AuctionEnded auctionId=%d winner=%s block=%d",
		e.AuctionID, e.Winner.Hex(), e.BlockNumber)

	if err := i.db.Model(&model.AuctionIndex{}).
		Where("auction_id = ?", e.AuctionID).
		Update("status", model.AuctionStatusEnded).Error; err != nil {
		log.Printf("[event_indexer] AuctionEnded update_failed auctionId=%d err=%v", e.AuctionID, err)
	}
}

func (i *EventIndexer) onAuctionCancelled(ctx context.Context, e blockchain.AuctionCancelledEvent) {
	log.Printf("[event_indexer] AuctionCancelled auctionId=%d block=%d", e.AuctionID, e.BlockNumber)

	if err := i.db.Model(&model.AuctionIndex{}).
		Where("auction_id = ?", e.AuctionID).
		Update("status", model.AuctionStatusCancelled).Error; err != nil {
		log.Printf("[event_indexer] AuctionCancelled update_failed auctionId=%d err=%v", e.AuctionID, err)
	}
}

// Ensure time import is used (for potential future use; also keeps the import clean).
var _ = time.Second
