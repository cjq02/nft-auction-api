package service

import (
	"context"
	"log"
	"math/big"

	"nft-auction-api/internal/blockchain"
	"nft-auction-api/internal/errors"
	"nft-auction-api/internal/model"

	"github.com/ethereum/go-ethereum/common"
	"gorm.io/gorm"
)

type AuctionService struct {
	db                  *gorm.DB
	auctionContract     *blockchain.AuctionContract
	defaultContractAddr string // 默认拍卖合约地址，用于查询时 contract 为空或历史数据
}

func NewAuctionService(db *gorm.DB, auctionContract *blockchain.AuctionContract, defaultContractAddr string) *AuctionService {
	return &AuctionService{
		db:                  db,
		auctionContract:     auctionContract,
		defaultContractAddr: defaultContractAddr,
	}
}

func (s *AuctionService) resolveContract(contract string) string {
	if contract != "" {
		return contract
	}
	return s.defaultContractAddr
}

// Counts 返回拍卖总数、进行中、已结束数量（用于数据概览）
func (s *AuctionService) Counts() (total, active, ended int64, err error) {
	if err = s.db.Model(&model.AuctionIndex{}).Count(&total).Error; err != nil {
		return 0, 0, 0, errors.NewDatabaseError(err)
	}
	if err = s.db.Model(&model.AuctionIndex{}).Where("status = ?", model.AuctionStatusActive).Count(&active).Error; err != nil {
		return 0, 0, 0, errors.NewDatabaseError(err)
	}
	if err = s.db.Model(&model.AuctionIndex{}).Where("status = ?", model.AuctionStatusEnded).Count(&ended).Error; err != nil {
		return 0, 0, 0, errors.NewDatabaseError(err)
	}
	return total, active, ended, nil
}

func (s *AuctionService) List(page, limit int, status, contractFilter string) ([]model.AuctionIndex, int64, error) {
	var items []model.AuctionIndex
	var total int64

	query := s.db.Model(&model.AuctionIndex{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	contract := s.resolveContract(contractFilter)
	if contract != "" {
		query = query.Where("auction_contract = ?", contract)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, errors.NewDatabaseError(err)
	}

	offset := (page - 1) * limit
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	if err := query.Order("created_at DESC").Limit(limit).Offset(offset).Find(&items).Error; err != nil {
		return nil, 0, errors.NewDatabaseError(err)
	}

	return items, total, nil
}

func (s *AuctionService) GetByAuctionID(auctionID uint64, contract string) (*model.AuctionIndex, error) {
	contract = s.resolveContract(contract)
	var item model.AuctionIndex
	query := s.db.Where("auction_id = ?", auctionID)
	if contract != "" {
		query = query.Where("auction_contract = ?", contract)
	}
	if err := query.First(&item).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			if s.auctionContract != nil && contract != "" {
				chainInfo, err := s.auctionContract.GetAuction(context.Background(), auctionID)
				if err != nil {
					return nil, errors.NewBlockchainError("获取链上拍卖失败", err)
				}
				if chainInfo != nil && chainInfo.Seller != (common.Address{}) {
					return s.chainInfoToIndex(auctionID, chainInfo, contract), nil
				}
			}
			return nil, errors.NewNotFoundError("拍卖不存在")
		}
		return nil, errors.NewDatabaseError(err)
	}
	return &item, nil
}

func (s *AuctionService) chainInfoToIndex(auctionID uint64, info *blockchain.AuctionInfo, contractAddress string) *model.AuctionIndex {
	var paymentToken *string
	if info.PaymentToken != (common.Address{}) && info.PaymentToken.Hex() != "0x0000000000000000000000000000000000000000" {
		addr := info.PaymentToken.Hex()
		paymentToken = &addr
	}

	status := model.AuctionStatusActive
	switch info.Status {
	case 1:
		status = model.AuctionStatusEnded
	case 2:
		status = model.AuctionStatusCancelled
	}

	return &model.AuctionIndex{
		AuctionContract: contractAddress,
		AuctionID:       auctionID,
		Seller:          info.Seller.Hex(),
		NFTContract:     info.NFTContract.Hex(),
		TokenID:         info.TokenID.Uint64(),
		StartTime:       info.StartTime.Int64(),
		EndTime:         info.EndTime.Int64(),
		MinBid:          info.MinBid.String(),
		PaymentToken:    paymentToken,
		Status:          status,
	}
}

func (s *AuctionService) ListBySeller(seller string, page, limit int, contractFilter string) ([]model.AuctionIndex, int64, error) {
	var items []model.AuctionIndex
	var total int64

	query := s.db.Model(&model.AuctionIndex{}).Where("seller = ?", seller)
	contract := s.resolveContract(contractFilter)
	if contract != "" {
		query = query.Where("auction_contract = ?", contract)
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, errors.NewDatabaseError(err)
	}

	offset := (page - 1) * limit
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 10
	}

	if err := query.Order("created_at DESC").Limit(limit).Offset(offset).Find(&items).Error; err != nil {
		return nil, 0, errors.NewDatabaseError(err)
	}

	return items, total, nil
}

func (s *AuctionService) CreateIndex(item *model.AuctionIndex) error {
	return s.db.Create(item).Error
}

func (s *AuctionService) UpdateStatus(contractAddress string, auctionID uint64, status model.AuctionStatus) error {
	contractAddress = s.resolveContract(contractAddress)
	return s.db.Model(&model.AuctionIndex{}).Where("auction_id = ? AND auction_contract = ?", auctionID, contractAddress).Update("status", status).Error
}

// BackfillResult 补录结果
type BackfillResult struct {
	FoundOnChain int // 链上扫描到的 AuctionCreated 数量
	Added        int // 本次新写入 DB 的数量
}

// BackfillFromChain 扫描链上所有历史 AuctionCreated 事件，将 DB 中缺失的拍卖补录进来。
// contractAddress 为当前监听的合约地址；startBlock 为 0 时从创世块扫。
func (s *AuctionService) BackfillFromChain(ctx context.Context, contractAddress string, startBlock uint64) (*BackfillResult, error) {
	contractAddress = s.resolveContract(contractAddress)
	log.Printf("[auction_sync] BackfillFromChain start contract=%s startBlock=%d", contractAddress, startBlock)
	if s.auctionContract == nil {
		log.Printf("[auction_sync] BackfillFromChain error: auction contract not configured")
		return nil, errors.NewBlockchainError("拍卖合约未配置", nil)
	}

	ids, err := s.auctionContract.ScanAuctionIDs(ctx, startBlock)
	if err != nil {
		log.Printf("[auction_sync] BackfillFromChain scan_failed startBlock=%d err=%v", startBlock, err)
		return nil, errors.NewBlockchainError("扫描链上事件失败: "+err.Error(), err)
	}
	log.Printf("[auction_sync] BackfillFromChain scan_done startBlock=%d foundOnChain=%d", startBlock, len(ids))

	result := &BackfillResult{FoundOnChain: len(ids)}
	for _, auctionID := range ids {
		var existing model.AuctionIndex
		if dbErr := s.db.Where("auction_contract = ? AND auction_id = ?", contractAddress, auctionID).First(&existing).Error; dbErr == nil {
			continue // 已存在，跳过
		}

		chainInfo, chainErr := s.auctionContract.GetAuction(ctx, auctionID)
		if chainErr != nil || chainInfo == nil {
			log.Printf("[auction_sync] BackfillFromChain skip_auction auctionId=%d get_auction_err=%v", auctionID, chainErr)
			continue
		}

		item := s.chainInfoToIndex(auctionID, chainInfo, contractAddress)
		if createErr := s.db.Create(item).Error; createErr == nil {
			result.Added++
			log.Printf("[auction_sync] BackfillFromChain added auctionId=%d", auctionID)
		} else {
			log.Printf("[auction_sync] BackfillFromChain db_create_failed auctionId=%d err=%v", auctionID, createErr)
		}
	}
	log.Printf("[auction_sync] BackfillFromChain done startBlock=%d foundOnChain=%d added=%d", startBlock, result.FoundOnChain, result.Added)
	return result, nil
}

// IndexFromAuctionID 从链上读取 auctionId 对应的拍卖信息并写入数据库（幂等）。
// contractAddress 为当前监听的拍卖合约地址。
func (s *AuctionService) IndexFromAuctionID(ctx context.Context, contractAddress string, auctionID uint64) (*model.AuctionIndex, error) {
	contractAddress = s.resolveContract(contractAddress)
	// 幂等：若已存在则直接返回
	var existing model.AuctionIndex
	if dbErr := s.db.Where("auction_contract = ? AND auction_id = ?", contractAddress, auctionID).First(&existing).Error; dbErr == nil {
		log.Printf("[auction_sync] IndexFromAuctionID already_exists auctionId=%d", auctionID)
		return &existing, nil
	}

	if s.auctionContract == nil {
		return nil, errors.NewBlockchainError("拍卖合约未配置", nil)
	}

	chainInfo, err := s.auctionContract.GetAuction(ctx, auctionID)
	if err != nil || chainInfo == nil {
		log.Printf("[auction_sync] IndexFromAuctionID get_auction_failed auctionId=%d err=%v", auctionID, err)
		return nil, errors.NewBlockchainError("从链上获取拍卖信息失败", err)
	}
	log.Printf("[auction_sync] IndexFromAuctionID chain_info_ok auctionId=%d seller=%s", auctionID, chainInfo.Seller.Hex())

	item := s.chainInfoToIndex(auctionID, chainInfo, contractAddress)
	if createErr := s.db.Create(item).Error; createErr != nil {
		log.Printf("[auction_sync] IndexFromAuctionID db_create_failed auctionId=%d err=%v", auctionID, createErr)
		return nil, errors.NewDatabaseError(createErr)
	}
	log.Printf("[auction_sync] IndexFromAuctionID db_created auctionId=%d", auctionID)
	return item, nil
}

// IndexFromTxHash 从 txHash 解析 AuctionCreated 事件，向链上查询完整数据后写入数据库（幂等）
// 使用默认合约地址作为 auction_contract。
func (s *AuctionService) IndexFromTxHash(ctx context.Context, txHash string) (*model.AuctionIndex, error) {
	log.Printf("[auction_sync] IndexFromTxHash start txHash=%s", txHash)
	if s.auctionContract == nil {
		return nil, errors.NewBlockchainError("拍卖合约未配置", nil)
	}

	auctionID, err := s.auctionContract.ParseAuctionCreatedFromReceipt(ctx, txHash)
	if err != nil {
		log.Printf("[auction_sync] IndexFromTxHash parse_failed txHash=%s err=%v", txHash, err)
		return nil, errors.NewBlockchainError("解析交易事件失败", err)
	}
	log.Printf("[auction_sync] IndexFromTxHash parsed auctionId=%d", auctionID)
	return s.IndexFromAuctionID(ctx, s.defaultContractAddr, auctionID)
}

// GetMinBidEth 根据链上价格将 minBid（USD，18 位小数字符串）换算为 ETH 展示字符串。
// 若链未配置或调用失败则返回空字符串，调用方可不展示 ETH 或做降级。
func (s *AuctionService) GetMinBidEth(ctx context.Context, auctionContractAddr, minBidUSD string) (string, error) {
	if s.auctionContract == nil || auctionContractAddr == "" || minBidUSD == "" {
		return "", nil
	}
	minBid := new(big.Int)
	if _, ok := minBid.SetString(minBidUSD, 10); !ok {
		return "", nil
	}
	if minBid.Sign() <= 0 {
		return "", nil
	}
	return s.auctionContract.GetMinBidEth(ctx, auctionContractAddr, minBid)
}

