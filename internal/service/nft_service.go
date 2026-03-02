package service

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
	"gorm.io/gorm"

	"nft-auction-api/internal/blockchain"
	"nft-auction-api/internal/errors"
	"nft-auction-api/internal/metadata"
	"nft-auction-api/internal/model"
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

// GetNFTsMintedTo 查询铸造给指定地址的所有 NFT（通过链上 NFTMinted 事件日志），分页返回
func (s *NFTService) GetNFTsMintedTo(ctx context.Context, contract, owner string, page, limit int) (total uint64, items []MintedNFTItem, err error) {
	if s.nftContract == nil || contract == "" {
		return 0, nil, errors.NewNotFoundError("NFT 合约未配置或未指定 contract")
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	toAddr := common.HexToAddress(owner)
	records, err := s.nftContract.GetMintedToAddress(ctx, contract, toAddr)
	if err != nil {
		return 0, nil, errors.NewBlockchainError("查询铸造记录失败", err)
	}

	total = uint64(len(records))
	offset := (page - 1) * limit
	if offset >= int(total) {
		return total, []MintedNFTItem{}, nil
	}
	end := offset + limit
	if end > int(total) {
		end = int(total)
	}

	items = make([]MintedNFTItem, 0, end-offset)
	for _, rec := range records[offset:end] {
		meta, _ := s.GetOrFetchMetadata(ctx, contract, rec.TokenID)
		item := MintedNFTItem{TokenID: rec.TokenID, Owner: rec.To.Hex()}
		if meta != nil {
			item.TokenURI = meta.TokenURI
			item.Name = meta.Name
			item.Description = meta.Description
			item.Image = meta.Image
		}
		items = append(items, item)
	}
	return total, items, nil
}
type MintedNFTItem struct {
	TokenID     uint64  `json:"tokenId"`
	Owner       string  `json:"owner"`
	TokenURI    *string `json:"tokenUri,omitempty"`
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Image       *string `json:"image,omitempty"`
}

// ListMintedNFTs 返回指定合约下已铸造 NFT 列表（分页），需链上 totalSupply + ownerOf + 元数据
func (s *NFTService) ListMintedNFTs(ctx context.Context, contract string, page, limit int) (total uint64, items []MintedNFTItem, err error) {
	if s.nftContract == nil || contract == "" {
		return 0, nil, errors.NewNotFoundError("NFT 合约未配置或未指定 contract")
	}
	total, err = s.nftContract.TotalSupply(ctx, contract)
	if err != nil {
		return 0, nil, errors.NewBlockchainError("获取 totalSupply 失败", err)
	}
	if total == 0 {
		return 0, []MintedNFTItem{}, nil
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	offset := (page - 1) * limit
	startID := uint64(offset) + 1
	if startID > total {
		return total, []MintedNFTItem{}, nil
	}
	endID := startID + uint64(limit) - 1
	if endID > total {
		endID = total
	}
	items = make([]MintedNFTItem, 0, endID-startID+1)
	for tokenID := startID; tokenID <= endID; tokenID++ {
		owner, err := s.nftContract.OwnerOf(ctx, contract, tokenID)
		if err != nil {
			continue
		}
		meta, _ := s.GetOrFetchMetadata(ctx, contract, tokenID)
		item := MintedNFTItem{
			TokenID: tokenID,
			Owner:   owner.Hex(),
		}
		if meta != nil {
			item.TokenURI = meta.TokenURI
			item.Name = meta.Name
			item.Description = meta.Description
			item.Image = meta.Image
		}
		items = append(items, item)
	}
	return total, items, nil
}
