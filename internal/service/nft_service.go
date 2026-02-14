package service

import (
	"nft-auction-api/internal/errors"
	"nft-auction-api/internal/model"

	"gorm.io/gorm"
)

type NFTService struct {
	db *gorm.DB
}

func NewNFTService(db *gorm.DB) *NFTService {
	return &NFTService{db: db}
}

func (s *NFTService) GetMetadata(nftContract string, tokenID uint64) (*model.NFTMetadata, error) {
	var item model.NFTMetadata
	if err := s.db.Where("nft_contract = ? AND token_id = ?", nftContract, tokenID).First(&item).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errors.NewNotFoundError("NFT 元数据不存在")
		}
		return nil, errors.NewDatabaseError(err)
	}
	return &item, nil
}

func (s *NFTService) UpsertMetadata(item *model.NFTMetadata) error {
	return s.db.Where("nft_contract = ? AND token_id = ?", item.NFTContract, item.TokenID).
		Assign(item).
		FirstOrCreate(item).Error
}
