package model

import (
	"time"
)

type AuctionStatus string

const (
	AuctionStatusActive    AuctionStatus = "Active"
	AuctionStatusEnded     AuctionStatus = "Ended"
	AuctionStatusCancelled AuctionStatus = "Cancelled"
)

type AuctionIndex struct {
	ID            uint          `json:"id" gorm:"primaryKey"`
	AuctionID     uint64        `json:"auctionId" gorm:"column:auction_id;uniqueIndex;not null"`
	Seller        string        `json:"seller" gorm:"size:42;not null"`
	NFTContract   string        `json:"nftContract" gorm:"column:nft_contract;size:42;not null"`
	TokenID       uint64        `json:"tokenId" gorm:"column:token_id;not null"`
	StartTime     int64         `json:"startTime" gorm:"column:start_time;not null"`
	EndTime       int64         `json:"endTime" gorm:"column:end_time;not null"`
	MinBid        string        `json:"minBid" gorm:"column:min_bid;size:78;not null"`
	PaymentToken  *string       `json:"paymentToken,omitempty" gorm:"column:payment_token;size:42"`
	Status        AuctionStatus `json:"status" gorm:"size:20;not null;default:Active"`
	CreatedAt     time.Time     `json:"createdAt"`
	UpdatedAt     time.Time     `json:"updatedAt"`
}

func (AuctionIndex) TableName() string {
	return "t_auction_index"
}

// CreateAuctionRequest 前端交易上链确认后上报 txHash，后端自行从链上读取数据写库
type CreateAuctionRequest struct {
	TxHash string `json:"txHash" binding:"required"`
}
