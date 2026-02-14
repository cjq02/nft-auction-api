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

func (s *AuctionService) PrepareCreateParams(req *model.CreateAuctionRequest) map[string]interface{} {
	return map[string]interface{}{
		"nftContract":  req.NFTContract,
		"tokenId":     req.TokenID,
		"duration":    req.Duration,
		"minBidUSD":   req.MinBidUSD,
		"paymentToken": req.PaymentToken,
	}
}

