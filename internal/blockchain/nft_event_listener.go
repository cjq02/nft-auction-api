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
)

// -------- NFT 事件结构 --------

type NFTMintedEvent struct {
	TokenID     uint64
	To          common.Address
	TokenURI    string
	BlockNumber uint64
	TxHash      string
}

type NFTBurnedEvent struct {
	TokenID     uint64
	BlockNumber uint64
	TxHash      string
}

// NFTEventHandlers 供 NFT 合约事件订阅使用
type NFTEventHandlers struct {
	OnNFTMinted func(ctx context.Context, e NFTMintedEvent)
	OnNFTBurned func(ctx context.Context, e NFTBurnedEvent)
	// OnTransfer ERC721 Transfer(from, to, tokenId)；铸造/转账/销毁都会触发，用于维护 t_nft_ownership
	OnTransfer func(ctx context.Context, from, to common.Address, tokenID uint64)
}

// -------- NFTEventListener --------

// NFTEventListener 订阅 NFT 合约的 NFTMinted、NFTBurned 事件
type NFTEventListener struct {
	wsClient       *Client
	contractAddr   common.Address
	handlers       NFTEventHandlers
	reconnectDelay time.Duration
}

func NewNFTEventListener(wsClient *Client, nftContractAddress string, handlers NFTEventHandlers) *NFTEventListener {
	if wsClient == nil || !wsClient.IsAvailable() || nftContractAddress == "" {
		return nil
	}
	return &NFTEventListener{
		wsClient:       wsClient,
		contractAddr:   common.HexToAddress(nftContractAddress),
		handlers:       handlers,
		reconnectDelay: 5 * time.Second,
	}
}

// Run 阻塞直到 ctx 取消；fromBlock 为起始区块（含），onBlockProcessed 在每批处理完后回调以便持久化 checkpoint
func (l *NFTEventListener) Run(ctx context.Context, fromBlock uint64, onBlockProcessed func(uint64)) {
	if l == nil {
		return
	}
	log.Printf("[nft_event_listener] started contractAddr=%s fromBlock=%d", l.contractAddr.Hex(), fromBlock)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[nft_event_listener] stopped")
			return
		default:
		}

		next, err := l.session(ctx, fromBlock, onBlockProcessed)
		fromBlock = next

		if ctx.Err() != nil {
			return
		}
		log.Printf("[nft_event_listener] reconnecting in %s (err=%v)", l.reconnectDelay, err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(l.reconnectDelay):
		}
	}
}

func (l *NFTEventListener) session(ctx context.Context, fromBlock uint64, onBlockProcessed func(uint64)) (uint64, error) {
	latestBlock, err := l.wsClient.BlockNumber(ctx)
	if err != nil {
		return fromBlock, fmt.Errorf("get block number: %w", err)
	}

	if fromBlock <= latestBlock {
		if err := l.backfill(ctx, fromBlock, latestBlock, onBlockProcessed); err != nil {
			return fromBlock, fmt.Errorf("backfill [%d,%d]: %w", fromBlock, latestBlock, err)
		}
	}
	nextFrom := latestBlock + 1

	filterQuery := ethereum.FilterQuery{
		Addresses: []common.Address{l.contractAddr},
		Topics: [][]common.Hash{{
			nftMintedEventSig,
			nftBurnedEventSig,
			erc721TransferEventSig,
		}},
	}

	logsCh := make(chan types.Log, 64)
	sub, err := l.wsClient.SubscribeFilterLogs(ctx, filterQuery, logsCh)
	if err != nil {
		return nextFrom, fmt.Errorf("SubscribeFilterLogs: %w", err)
	}
	defer sub.Unsubscribe()
	log.Printf("[nft_event_listener] subscription active, from block %d", nextFrom)

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

// nftBackfillChunkSize NFT 回填时单次 FilterLogs 的区块数（偏小以降低 RPC internal error）
const nftBackfillChunkSize = 1000

func (l *NFTEventListener) backfill(ctx context.Context, from, to uint64, onBlockProcessed func(uint64)) error {
	if from > to {
		return nil
	}
	log.Printf("[nft_event_listener] backfill from=%d to=%d", from, to)

	filterQuery := ethereum.FilterQuery{
		Addresses: []common.Address{l.contractAddr},
		Topics: [][]common.Hash{{
			nftMintedEventSig,
			nftBurnedEventSig,
			erc721TransferEventSig,
		}},
	}

	cur := from
	for cur <= to {
		end := cur + nftBackfillChunkSize - 1
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
			log.Printf("[nft_event_listener] backfill chunk [%d,%d] logs=%d", cur, end, len(logs))
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

func (l *NFTEventListener) dispatchLog(ctx context.Context, lg types.Log) {
	if len(lg.Topics) == 0 {
		return
	}
	switch lg.Topics[0] {
	case nftMintedEventSig:
		l.handleNFTMinted(ctx, lg)
	case nftBurnedEventSig:
		l.handleNFTBurned(ctx, lg)
	case erc721TransferEventSig:
		l.handleTransfer(ctx, lg)
	}
}

// Transfer(address indexed from, address indexed to, uint256 indexed tokenId) — topics[1]=from, [2]=to, [3]=tokenId
func (l *NFTEventListener) handleTransfer(ctx context.Context, lg types.Log) {
	if len(lg.Topics) < 4 || l.handlers.OnTransfer == nil {
		return
	}
	from := common.BytesToAddress(lg.Topics[1].Bytes())
	to := common.BytesToAddress(lg.Topics[2].Bytes())
	tokenID := new(big.Int).SetBytes(lg.Topics[3].Bytes())
	if !tokenID.IsUint64() {
		return
	}
	l.handlers.OnTransfer(ctx, from, to, tokenID.Uint64())
}

// NFTMinted(address indexed to, uint256 indexed tokenId, string tokenURI)
// topics[1]=to, topics[2]=tokenId, data=tokenURI
func (l *NFTEventListener) handleNFTMinted(ctx context.Context, lg types.Log) {
	if len(lg.Topics) < 3 {
		log.Printf("[nft_event_listener] NFTMinted malformed tx=%s", lg.TxHash.Hex())
		return
	}
	if l.handlers.OnNFTMinted == nil {
		return
	}
	tokenID := new(big.Int).SetBytes(lg.Topics[2].Bytes())
	if !tokenID.IsUint64() {
		return
	}
	tokenURI := ""
	if len(lg.Data) >= 64 {
		// ABI string: offset(32) then at offset: length(32) + utf8 bytes
		length := new(big.Int).SetBytes(lg.Data[32:64]).Uint64()
		if length > 0 && uint64(len(lg.Data)) >= 64+length {
			tokenURI = string(lg.Data[64 : 64+length])
		}
	}
	l.handlers.OnNFTMinted(ctx, NFTMintedEvent{
		TokenID:     tokenID.Uint64(),
		To:          common.BytesToAddress(lg.Topics[1].Bytes()),
		TokenURI:    tokenURI,
		BlockNumber: lg.BlockNumber,
		TxHash:      lg.TxHash.Hex(),
	})
}

// NFTBurned(uint256 indexed tokenId) — topics[1]=tokenId
func (l *NFTEventListener) handleNFTBurned(ctx context.Context, lg types.Log) {
	if len(lg.Topics) < 2 {
		return
	}
	if l.handlers.OnNFTBurned == nil {
		return
	}
	tokenID := new(big.Int).SetBytes(lg.Topics[1].Bytes())
	if !tokenID.IsUint64() {
		return
	}
	l.handlers.OnNFTBurned(ctx, NFTBurnedEvent{
		TokenID:     tokenID.Uint64(),
		BlockNumber: lg.BlockNumber,
		TxHash:      lg.TxHash.Hex(),
	})
}
