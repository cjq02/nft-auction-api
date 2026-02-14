package model

import (
	"time"
)

type BidIndex struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	AuctionID   uint64    `json:"auctionId" gorm:"column:auction_id;not null"`
	Bidder      string    `json:"bidder" gorm:"size:42;not null"`
	Amount      string    `json:"amount" gorm:"size:78;not null"`
	BidTimestamp int64    `json:"bidTimestamp" gorm:"column:bid_timestamp;not null"`
	IsETH       bool      `json:"isEth" gorm:"column:is_eth;not null"`
	CreatedAt   time.Time `json:"createdAt"`
}

func (BidIndex) TableName() string {
	return "t_bid_index"
}
