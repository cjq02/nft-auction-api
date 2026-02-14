package model

import (
	"time"
)

type NFTMetadata struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	NFTContract string    `json:"nftContract" gorm:"column:nft_contract;size:42;not null"`
	TokenID     uint64    `json:"tokenId" gorm:"column:token_id;not null"`
	TokenURI    *string   `json:"tokenUri,omitempty" gorm:"column:token_uri;size:512"`
	Name        *string   `json:"name,omitempty" gorm:"size:255"`
	Description *string   `json:"description,omitempty" gorm:"type:text"`
	Image       *string   `json:"image,omitempty" gorm:"size:512"`
	RawJSON     *string   `json:"-" gorm:"column:raw_json;type:text"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func (NFTMetadata) TableName() string {
	return "t_nft_metadata"
}
