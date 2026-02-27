package service

import (
	"context"

	"nft-auction-api/internal/blockchain"
	"nft-auction-api/internal/errors"
	"nft-auction-api/internal/metadata"
	"nft-auction-api/internal/model"

	"gorm.io/gorm"
)

type NFTService struct {
	db          *gorm.DB
	nftContract *blockchain.NFTContract
	fetcher     *metadata.Fetcher
}

func NewNFTService(db *gorm.DB, nftContract *blockchain.NFTContract, fetcher *metadata.Fetcher) *NFTService {
	return &NFTService{
		db:          db,
		nftContract: nftContract,
		fetcher:     fetcher,
	}
}

// GetMetadata 仅从缓存读取，不存在则返回错误
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

// GetOrFetchMetadata 先查缓存；若无且已配置链上与拉取器，则从链上取 tokenURI、拉取元数据并写入缓存后返回
func (s *NFTService) GetOrFetchMetadata(ctx context.Context, nftContract string, tokenID uint64) (*model.NFTMetadata, error) {
	item, err := s.GetMetadata(nftContract, tokenID)
	if err == nil {
		return item, nil
	}
	appErr, ok := errors.IsAppError(err)
	if !ok || appErr.Code != errors.ErrCodeNotFound {
		return nil, err
	}

	if s.nftContract == nil || s.fetcher == nil {
		return nil, errors.NewNotFoundError("NFT 元数据不存在")
	}

	uri, err := s.nftContract.TokenURI(ctx, nftContract, tokenID)
	if err != nil {
		return nil, errors.NewBlockchainError("获取 tokenURI 失败", err)
	}
	if uri == "" {
		return nil, errors.NewNotFoundError("NFT 元数据不存在")
	}

	parsed, err := s.fetcher.Fetch(uri)
	if err != nil {
		return nil, errors.NewInternalError("拉取元数据失败: "+err.Error(), err)
	}

	item = &model.NFTMetadata{
		NFTContract: nftContract,
		TokenID:    tokenID,
		RawJSON:    &parsed.RawJSON,
	}
	if uri != "" {
		item.TokenURI = &uri
	}
	if parsed.Name != "" {
		item.Name = &parsed.Name
	}
	if parsed.Description != "" {
		item.Description = &parsed.Description
	}
	if parsed.Image != "" {
		item.Image = &parsed.Image
	}

	if err := s.UpsertMetadata(item); err != nil {
		return nil, errors.NewDatabaseError(err)
	}
	return item, nil
}

func (s *NFTService) UpsertMetadata(item *model.NFTMetadata) error {
	return s.db.Where("nft_contract = ? AND token_id = ?", item.NFTContract, item.TokenID).
		Assign(item).
		FirstOrCreate(item).Error
}
