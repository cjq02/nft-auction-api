package service

import (
	"nft-auction-api/internal/errors"
	"nft-auction-api/internal/model"

	"gorm.io/gorm"
)

type BidService struct {
	db *gorm.DB
}

func NewBidService(db *gorm.DB) *BidService {
	return &BidService{db: db}
}

// ListByAuctionID 按拍卖合约 + auction_id 查出价列表；contract 为空时不过滤合约（兼容旧数据）。
func (s *BidService) ListByAuctionID(auctionID uint64, contract string) ([]model.BidIndex, error) {
	var items []model.BidIndex
	query := s.db.Where("auction_id = ?", auctionID)
	if contract != "" {
		query = query.Where("auction_contract = ?", contract)
	}
	if err := query.Order("bid_timestamp DESC").Find(&items).Error; err != nil {
		return nil, errors.NewDatabaseError(err)
	}
	return items, nil
}

func (s *BidService) CreateIndex(item *model.BidIndex) error {
	return s.db.Create(item).Error
}
