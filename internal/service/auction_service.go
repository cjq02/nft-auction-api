package service

import (
	"context"

	"nft-auction-api/internal/blockchain"
	"nft-auction-api/internal/errors"
	"nft-auction-api/internal/model"

	"github.com/ethereum/go-ethereum/common"
	"gorm.io/gorm"
)

type AuctionService struct {
	db             *gorm.DB
	auctionContract *blockchain.AuctionContract
}

func NewAuctionService(db *gorm.DB, auctionContract *blockchain.AuctionContract) *AuctionService {
	return &AuctionService{
		db:              db,
		auctionContract: auctionContract,
	}
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

func (s *AuctionService) List(page, limit int, status string) ([]model.AuctionIndex, int64, error) {
	var items []model.AuctionIndex
	var total int64

	query := s.db.Model(&model.AuctionIndex{})
	if status != "" {
		query = query.Where("status = ?", status)
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

func (s *AuctionService) GetByAuctionID(auctionID uint64) (*model.AuctionIndex, error) {
	var item model.AuctionIndex
	if err := s.db.Where("auction_id = ?", auctionID).First(&item).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			if s.auctionContract != nil {
				chainInfo, err := s.auctionContract.GetAuction(context.Background(), auctionID)
				if err != nil {
					return nil, errors.NewBlockchainError("获取链上拍卖失败", err)
				}
				if chainInfo != nil && chainInfo.Seller != (common.Address{}) {
					return s.chainInfoToIndex(auctionID, chainInfo), nil
				}
			}
			return nil, errors.NewNotFoundError("拍卖不存在")
		}
		return nil, errors.NewDatabaseError(err)
	}
	return &item, nil
}

func (s *AuctionService) chainInfoToIndex(auctionID uint64, info *blockchain.AuctionInfo) *model.AuctionIndex {
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
		AuctionID:    auctionID,
		Seller:       info.Seller.Hex(),
		NFTContract:  info.NFTContract.Hex(),
		TokenID:      info.TokenID.Uint64(),
		StartTime:    info.StartTime.Int64(),
		EndTime:      info.EndTime.Int64(),
		MinBid:       info.MinBid.String(),
		PaymentToken: paymentToken,
		Status:       status,
	}
}

func (s *AuctionService) ListBySeller(seller string, page, limit int) ([]model.AuctionIndex, int64, error) {
	var items []model.AuctionIndex
	var total int64

	query := s.db.Model(&model.AuctionIndex{}).Where("seller = ?", seller)
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

func (s *AuctionService) UpdateStatus(auctionID uint64, status model.AuctionStatus) error {
	return s.db.Model(&model.AuctionIndex{}).Where("auction_id = ?", auctionID).Update("status", status).Error
}

// BackfillFromChain 扫描链上所有历史 AuctionCreated 事件，将 DB 中缺失的拍卖补录进来。
// 返回本次新增的拍卖数量。
func (s *AuctionService) BackfillFromChain(ctx context.Context) (int, error) {
	if s.auctionContract == nil {
		return 0, errors.NewBlockchainError("拍卖合约未配置", nil)
	}

	ids, err := s.auctionContract.ScanAuctionIDs(ctx)
	if err != nil {
		return 0, errors.NewBlockchainError("扫描链上事件失败", err)
	}

	added := 0
	for _, auctionID := range ids {
		var existing model.AuctionIndex
		if dbErr := s.db.Where("auction_id = ?", auctionID).First(&existing).Error; dbErr == nil {
			continue // 已存在，跳过
		}

		chainInfo, chainErr := s.auctionContract.GetAuction(ctx, auctionID)
		if chainErr != nil || chainInfo == nil {
			continue
		}

		item := s.chainInfoToIndex(auctionID, chainInfo)
		if createErr := s.db.Create(item).Error; createErr == nil {
			added++
		}
	}

	return added, nil
}

// IndexFromTxHash 从 txHash 解析 AuctionCreated 事件，向链上查询完整数据后写入数据库（幂等）
func (s *AuctionService) IndexFromTxHash(ctx context.Context, txHash string) (*model.AuctionIndex, error) {
	if s.auctionContract == nil {
		return nil, errors.NewBlockchainError("拍卖合约未配置", nil)
	}

	auctionID, err := s.auctionContract.ParseAuctionCreatedFromReceipt(ctx, txHash)
	if err != nil {
		return nil, errors.NewBlockchainError("解析交易事件失败", err)
	}

	// 幂等：若已存在则直接返回
	var existing model.AuctionIndex
	if dbErr := s.db.Where("auction_id = ?", auctionID).First(&existing).Error; dbErr == nil {
		return &existing, nil
	}

	chainInfo, err := s.auctionContract.GetAuction(ctx, auctionID)
	if err != nil || chainInfo == nil {
		return nil, errors.NewBlockchainError("从链上获取拍卖信息失败", err)
	}

	item := s.chainInfoToIndex(auctionID, chainInfo)
	if createErr := s.db.Create(item).Error; createErr != nil {
		return nil, errors.NewDatabaseError(createErr)
	}

	return item, nil
}

