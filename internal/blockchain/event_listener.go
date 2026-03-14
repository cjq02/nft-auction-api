package blockchain

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// -------- parsed event structs --------

type AuctionCreatedEvent struct {
	AuctionID   uint64
	Seller      common.Address
	NFTContract common.Address
	TokenID     *big.Int
	StartTime   *big.Int
	EndTime     *big.Int
	MinBid      *big.Int
	BlockNumber uint64
	TxHash      string
}

type BidPlacedEvent struct {
	AuctionID   uint64
	Bidder      common.Address
	Amount      *big.Int
	IsETH       bool
	BlockTime   int64 // from block header
	BlockNumber uint64
	TxHash      string
}

type AuctionEndedEvent struct {
	AuctionID   uint64
	Winner      common.Address
	FinalPrice  *big.Int
	BlockNumber uint64
	TxHash      string
}

type AuctionCancelledEvent struct {
	AuctionID   uint64
	BlockNumber uint64
	TxHash      string
}

// FeeCollectedEvent 手续费收取事件（与合约 FeeCollected 一致）
type FeeCollectedEvent struct {
	AuctionID   uint64
	Recipient   common.Address
	Amount      *big.Int
	IsETH       bool
	FeeRateBps  uint64   // 实际使用的费率（基点），V2 动态手续费后新增
	BlockNumber uint64
	TxHash      string
}

// EventHandlers holds callbacks for each event type; nil callbacks are skipped.
type EventHandlers struct {
	OnAuctionCreated   func(ctx context.Context, e AuctionCreatedEvent)
	OnBidPlaced        func(ctx context.Context, e BidPlacedEvent)
	OnAuctionEnded     func(ctx context.Context, e AuctionEndedEvent)
	OnAuctionCancelled func(ctx context.Context, e AuctionCancelledEvent)
	OnFeeCollected     func(ctx context.Context, e FeeCollectedEvent)
}

// -------- event topic hashes --------

var (
	// AuctionCreated(uint256 indexed,address indexed,address indexed,uint256,uint256,uint256,uint256)
	listenerAuctionCreatedTopic = crypto.Keccak256Hash([]byte("AuctionCreated(uint256,address,address,uint256,uint256,uint256,uint256)"))
	// BidPlaced(uint256 indexed,address indexed,uint256,bool)
	bidPlacedTopic = crypto.Keccak256Hash([]byte("BidPlaced(uint256,address,uint256,bool)"))
	// AuctionEnded(uint256 indexed,address indexed,uint256)
	auctionEndedTopic = crypto.Keccak256Hash([]byte("AuctionEnded(uint256,address,uint256)"))
	// AuctionCancelled(uint256 indexed)
	auctionCancelledTopic = crypto.Keccak256Hash([]byte("AuctionCancelled(uint256)"))
	// FeeCollected(uint256 indexed, address indexed, uint256 amount, bool isETH, uint256 feeRateBps)
	feeCollectedTopic = crypto.Keccak256Hash([]byte("FeeCollected(uint256,address,uint256,bool,uint256)"))
)

// -------- EventListener --------

// EventListener subscribes to contract events via WebSocket (SubscribeFilterLogs).
// On disconnect it automatically reconnects and backfills any missed blocks.
// wsClient must be dialled with a wss:// URL.
type EventListener struct {
	wsClient       *Client
	contractAddr   common.Address
	handlers       EventHandlers
	reconnectDelay time.Duration
}

func NewEventListener(wsClient *Client, contractAddress string, handlers EventHandlers) *EventListener {
	if wsClient == nil || !wsClient.IsAvailable() || contractAddress == "" {
		return nil
	}
	return &EventListener{
		wsClient:       wsClient,
		contractAddr:   common.HexToAddress(contractAddress),
		handlers:       handlers,
		reconnectDelay: 5 * time.Second,
	}
}

// Run blocks until ctx is cancelled.
// fromBlock is the first block to process (inclusive); callers restore it from a
// persisted checkpoint so no events are missed across restarts.
// onBlockProcessed is called after each successfully handled block batch so the
// caller can persist the checkpoint.
func (l *EventListener) Run(ctx context.Context, fromBlock uint64, onBlockProcessed func(uint64)) {
	if l == nil {
		return
	}
	log.Printf("[event_listener] started contractAddr=%s fromBlock=%d (WebSocket)",
		l.contractAddr.Hex(), fromBlock)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[event_listener] stopped")
			return
		default:
		}

		next, err := l.session(ctx, fromBlock, onBlockProcessed)
		fromBlock = next

		if ctx.Err() != nil {
			return
		}
		log.Printf("[event_listener] reconnecting in %s (err=%v)", l.reconnectDelay, err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(l.reconnectDelay):
		}
	}
}

// session runs one WebSocket subscription lifetime:
//  1. backfill logs from [fromBlock, latestBlock]
//  2. subscribe for new logs
//  3. stream until the subscription drops or ctx is cancelled
//
// Returns the next fromBlock the caller should use (latest processed + 1).
func (l *EventListener) session(ctx context.Context, fromBlock uint64, onBlockProcessed func(uint64)) (uint64, error) {
	latestBlock, err := l.wsClient.BlockNumber(ctx)
	if err != nil {
		return fromBlock, fmt.Errorf("get block number: %w", err)
	}

	// Step 1 — backfill missed historical range
	if fromBlock <= latestBlock {
		if err := l.backfill(ctx, fromBlock, latestBlock, onBlockProcessed); err != nil {
			return fromBlock, fmt.Errorf("backfill [%d,%d]: %w", fromBlock, latestBlock, err)
		}
	}
	nextFrom := latestBlock + 1

	// Step 2 — subscribe for new events (from latestBlock+1 onwards)
	filterQuery := ethereum.FilterQuery{
		Addresses: []common.Address{l.contractAddr},
		Topics: [][]common.Hash{{
			listenerAuctionCreatedTopic,
			bidPlacedTopic,
			auctionEndedTopic,
			auctionCancelledTopic,
			feeCollectedTopic,
		}},
	}

	logsCh := make(chan types.Log, 64)
	sub, err := l.wsClient.SubscribeFilterLogs(ctx, filterQuery, logsCh)
	if err != nil {
		return nextFrom, fmt.Errorf("SubscribeFilterLogs: %w", err)
	}
	defer sub.Unsubscribe()
	log.Printf("[event_listener] subscription active, listening from block %d", nextFrom)

	// Step 3 — stream events
	for {
		select {
		case <-ctx.Done():
			return nextFrom, nil

		case err := <-sub.Err():
			return nextFrom, fmt.Errorf("subscription dropped: %w", err)

		case lg := <-logsCh:
			l.dispatchLog(ctx, lg)
			if lg.BlockNumber >= nextFrom {
				nextFrom = lg.BlockNumber + 1
			}
			if onBlockProcessed != nil {
				onBlockProcessed(lg.BlockNumber)
			}
		}
	}
}

// backfill fetches logs in [from, to] using chunked FilterLogs to respect RPC limits.
func (l *EventListener) backfill(ctx context.Context, from, to uint64, onBlockProcessed func(uint64)) error {
	if from > to {
		return nil
	}
	log.Printf("[event_listener] backfill from=%d to=%d", from, to)

	filterQuery := ethereum.FilterQuery{
		Addresses: []common.Address{l.contractAddr},
		Topics: [][]common.Hash{{
			listenerAuctionCreatedTopic,
			bidPlacedTopic,
			auctionEndedTopic,
			auctionCancelledTopic,
			feeCollectedTopic,
		}},
	}

	cur := from
	for cur <= to {
		end := cur + scanBlockChunkSize - 1
		if end > to {
			end = to
		}

		filterQuery.FromBlock = new(big.Int).SetUint64(cur)
		filterQuery.ToBlock = new(big.Int).SetUint64(end)

		logs, err := l.wsClient.FilterLogs(ctx, filterQuery)
		if err != nil {
			return fmt.Errorf("FilterLogs [%d,%d]: %w", cur, end, err)
		}
		for _, lg := range logs {
			l.dispatchLog(ctx, lg)
		}
		if len(logs) > 0 {
			log.Printf("[event_listener] backfill chunk [%d,%d] logs=%d", cur, end, len(logs))
		}
		if onBlockProcessed != nil {
			onBlockProcessed(end)
		}

		cur = end + 1
		if cur <= to {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(scanChunkDelay):
			}
		}
	}
	return nil
}

// -------- log dispatch & parsing --------

func (l *EventListener) dispatchLog(ctx context.Context, lg types.Log) {
	if len(lg.Topics) == 0 {
		return
	}
	switch lg.Topics[0] {
	case listenerAuctionCreatedTopic:
		l.handleAuctionCreated(ctx, lg)
	case bidPlacedTopic:
		l.handleBidPlaced(ctx, lg)
	case auctionEndedTopic:
		l.handleAuctionEnded(ctx, lg)
	case auctionCancelledTopic:
		l.handleAuctionCancelled(ctx, lg)
	case feeCollectedTopic:
		l.handleFeeCollected(ctx, lg)
	}
}

// AuctionCreated: topics[1]=auctionId, topics[2]=seller, topics[3]=nftContract
// data: tokenId(32) | startTime(32) | endTime(32) | minBid(32)
func (l *EventListener) handleAuctionCreated(ctx context.Context, lg types.Log) {
	if len(lg.Topics) < 4 || len(lg.Data) < 4*32 {
		log.Printf("[event_listener] AuctionCreated malformed tx=%s", lg.TxHash.Hex())
		return
	}
	if l.handlers.OnAuctionCreated == nil {
		return
	}
	l.handlers.OnAuctionCreated(ctx, AuctionCreatedEvent{
		AuctionID:   new(big.Int).SetBytes(lg.Topics[1].Bytes()).Uint64(),
		Seller:      common.BytesToAddress(lg.Topics[2].Bytes()),
		NFTContract: common.BytesToAddress(lg.Topics[3].Bytes()),
		TokenID:     new(big.Int).SetBytes(lg.Data[0:32]),
		StartTime:   new(big.Int).SetBytes(lg.Data[32:64]),
		EndTime:     new(big.Int).SetBytes(lg.Data[64:96]),
		MinBid:      new(big.Int).SetBytes(lg.Data[96:128]),
		BlockNumber: lg.BlockNumber,
		TxHash:      lg.TxHash.Hex(),
	})
}

// BidPlaced: topics[1]=auctionId, topics[2]=bidder
// data: amount(32) | isETH(32, right-aligned bool)
func (l *EventListener) handleBidPlaced(ctx context.Context, lg types.Log) {
	if len(lg.Topics) < 3 || len(lg.Data) < 2*32 {
		log.Printf("[event_listener] BidPlaced malformed tx=%s", lg.TxHash.Hex())
		return
	}
	if l.handlers.OnBidPlaced == nil {
		return
	}

	// extra RPC call to get block timestamp
	var blockTime int64
	if header, err := l.wsClient.HeaderByNumber(ctx, new(big.Int).SetUint64(lg.BlockNumber)); err == nil && header != nil {
		blockTime = int64(header.Time)
	}

	// bool: ABI encodes as a full 32-byte slot, value in the last byte
	isETH := lg.Data[63] != 0

	l.handlers.OnBidPlaced(ctx, BidPlacedEvent{
		AuctionID:   new(big.Int).SetBytes(lg.Topics[1].Bytes()).Uint64(),
		Bidder:      common.BytesToAddress(lg.Topics[2].Bytes()),
		Amount:      new(big.Int).SetBytes(lg.Data[0:32]),
		IsETH:       isETH,
		BlockTime:   blockTime,
		BlockNumber: lg.BlockNumber,
		TxHash:      lg.TxHash.Hex(),
	})
}

// AuctionEnded: topics[1]=auctionId, topics[2]=winner
// data: finalPrice(32)
func (l *EventListener) handleAuctionEnded(ctx context.Context, lg types.Log) {
	if len(lg.Topics) < 3 {
		log.Printf("[event_listener] AuctionEnded malformed tx=%s", lg.TxHash.Hex())
		return
	}
	if l.handlers.OnAuctionEnded == nil {
		return
	}
	var finalPrice *big.Int
	if len(lg.Data) >= 32 {
		finalPrice = new(big.Int).SetBytes(lg.Data[0:32])
	} else {
		finalPrice = big.NewInt(0)
	}
	l.handlers.OnAuctionEnded(ctx, AuctionEndedEvent{
		AuctionID:   new(big.Int).SetBytes(lg.Topics[1].Bytes()).Uint64(),
		Winner:      common.BytesToAddress(lg.Topics[2].Bytes()),
		FinalPrice:  finalPrice,
		BlockNumber: lg.BlockNumber,
		TxHash:      lg.TxHash.Hex(),
	})
}

// AuctionCancelled: topics[1]=auctionId
func (l *EventListener) handleAuctionCancelled(ctx context.Context, lg types.Log) {
	if len(lg.Topics) < 2 {
		return
	}
	if l.handlers.OnAuctionCancelled == nil {
		return
	}
	l.handlers.OnAuctionCancelled(ctx, AuctionCancelledEvent{
		AuctionID:   new(big.Int).SetBytes(lg.Topics[1].Bytes()).Uint64(),
		BlockNumber: lg.BlockNumber,
		TxHash:      lg.TxHash.Hex(),
	})
}

// FeeCollected: topics[1]=auctionId, topics[2]=recipient; data=amount(32), isETH(32), feeRateBps(32)
// 旧合约可能只有 64 字节 data，无 feeRateBps，此时 FeeRateBps 填 0
func (l *EventListener) handleFeeCollected(ctx context.Context, lg types.Log) {
	if len(lg.Topics) < 3 || len(lg.Data) < 64 {
		log.Printf("[event_listener] FeeCollected malformed tx=%s", lg.TxHash.Hex())
		return
	}
	if l.handlers.OnFeeCollected == nil {
		return
	}
	isETH := lg.Data[63] != 0
	var feeRateBps uint64
	if len(lg.Data) >= 96 {
		feeRateBps = new(big.Int).SetBytes(lg.Data[64:96]).Uint64()
	}
	l.handlers.OnFeeCollected(ctx, FeeCollectedEvent{
		AuctionID:   new(big.Int).SetBytes(lg.Topics[1].Bytes()).Uint64(),
		Recipient:   common.BytesToAddress(lg.Topics[2].Bytes()),
		Amount:      new(big.Int).SetBytes(lg.Data[0:32]),
		IsETH:       isETH,
		FeeRateBps:  feeRateBps,
		BlockNumber: lg.BlockNumber,
		TxHash:      lg.TxHash.Hex(),
	})
}
