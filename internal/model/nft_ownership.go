package model

import "time"

// NftOwnership 记录 NFT 当前持有人，由索引器监听 Transfer 事件维护；查「我的 NFT」走此表
type NftOwnership struct {
	NftContract   string    `gorm:"column:nft_contract;size:42;primaryKey;not null"`
	TokenID       uint64    `gorm:"column:token_id;primaryKey;not null"`
	OwnerAddress  string    `gorm:"column:owner_address;size:42;not null;index:idx_owner"`
	UpdatedAt     time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (NftOwnership) TableName() string {
	return "t_nft_ownership"
}
