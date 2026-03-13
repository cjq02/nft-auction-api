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

// GetHighestBidsForAuctions 批量获取多个拍卖的最高出价（按 amount 最大的一条）。
// contract 为空时不过滤合约；通常列表已按单一合约筛选。
func (s *BidService) GetHighestBidsForAuctions(auctionIDs []uint64, contract string) (map[uint64]*model.BidIndex, error) {
	result := make(map[uint64]*model.BidIndex, len(auctionIDs))
	if len(auctionIDs) == 0 {
		return result, nil
	}

	var bids []model.BidIndex
	query := s.db.Where("auction_id IN ?", auctionIDs)
	if contract != "" {
		query = query.Where("auction_contract = ?", contract)
	}
	// 按 auction_id、amount 从大到小排序，保证第一条为最高出价
	if err := query.Order("auction_id ASC, amount DESC").Find(&bids).Error; err != nil {
		return nil, errors.NewDatabaseError(err)
	}

	for _, b := range bids {
		if _, ok := result[b.AuctionID]; !ok {
			// 第一条即为该拍卖最高出价
			copy := b
			result[b.AuctionID] = &copy
		}
	}
	return result, nil
}

func (s *BidService) CreateIndex(item *model.BidIndex) error {
	return s.db.Create(item).Error
}
