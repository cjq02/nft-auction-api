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

func (s *BidService) ListByAuctionID(auctionID uint64) ([]model.BidIndex, error) {
	var items []model.BidIndex
	if err := s.db.Where("auction_id = ?", auctionID).Order("bid_timestamp DESC").Find(&items).Error; err != nil {
		return nil, errors.NewDatabaseError(err)
	}
	return items, nil
}

func (s *BidService) CreateIndex(item *model.BidIndex) error {
	return s.db.Create(item).Error
}
