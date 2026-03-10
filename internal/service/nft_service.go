package service

import (
	"context"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"gorm.io/gorm"

	"nft-auction-api/internal/blockchain"
	"nft-auction-api/internal/errors"
	"nft-auction-api/internal/metadata"
	"nft-auction-api/internal/model"
)

type NFTService struct {
	db             *gorm.DB
	nftContract    *blockchain.NFTContract
	fetcher        *metadata.Fetcher
	imageCache     *metadata.ImageCache
	nftDeployBlock uint64
}

func NewNFTService(db *gorm.DB, nftContract *blockchain.NFTContract, fetcher *metadata.Fetcher, nftDeployBlock uint64) *NFTService {
	return &NFTService{
		db:             db,
		nftContract:    nftContract,
		fetcher:        fetcher,
		imageCache:     metadata.NewImageCache(),
		nftDeployBlock: nftDeployBlock,
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

// DeleteMetadata 删除指定 NFT 的元数据缓存（销毁时调用，与 t_nft_metadata 同步）
func (s *NFTService) DeleteMetadata(ctx context.Context, nftContract string, tokenID uint64) error {
	return s.db.WithContext(ctx).Where("nft_contract = ? AND token_id = ?", nftContract, tokenID).
		Delete(&model.NFTMetadata{}).Error
}

// GetImageProxy 根据图片 URI（ipfs:// 或 https://）拉取图片并缓存，返回 body 与 Content-Type；用于前端走代理加速
func (s *NFTService) GetImageProxy(ctx context.Context, imageURI string) ([]byte, string, error) {
	if s.fetcher == nil || strings.TrimSpace(imageURI) == "" {
		return nil, "", errors.NewNotFoundError("未配置拉取器或 URI 为空")
	}
	resolved := s.fetcher.ResolveURL(imageURI)
	if data, ct, ok := s.imageCache.Get(resolved); ok {
		return data, ct, nil
	}
	data, ct, err := s.fetcher.FetchImage(resolved)
	if err != nil {
		return nil, "", err
	}
	s.imageCache.Set(resolved, data, ct)
	return data, ct, nil
}

// CountByOwner 返回指定地址在指定合约下持有的 NFT 数量（读 t_nft_ownership）
func (s *NFTService) CountByOwner(ctx context.Context, contract, owner string) (uint64, error) {
	if contract == "" {
		return 0, errors.NewNotFoundError("未指定 contract")
	}
	ownerLower := strings.ToLower(owner)
	var count int64
	if err := s.db.WithContext(ctx).Model(&model.NftOwnership{}).
		Where("nft_contract = ? AND LOWER(owner_address) = ?", contract, ownerLower).
		Count(&count).Error; err != nil {
		return 0, errors.NewDatabaseError(err)
	}
	return uint64(count), nil
}

// GetNFTsOwnedBy 查询指定地址当前持有的 NFT，优先读 t_nft_ownership（由索引器维护），分页返回
func (s *NFTService) GetNFTsOwnedBy(ctx context.Context, contract, owner string, page, limit int) (total uint64, items []MintedNFTItem, err error) {
	if contract == "" {
		return 0, nil, errors.NewNotFoundError("未指定 contract")
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

	ownerLower := strings.ToLower(owner)
	var totalCount int64
	if err := s.db.WithContext(ctx).Model(&model.NftOwnership{}).
		Where("nft_contract = ? AND LOWER(owner_address) = ?", contract, ownerLower).
		Count(&totalCount).Error; err != nil {
		return 0, nil, errors.NewDatabaseError(err)
	}
	total = uint64(totalCount)
	offset := (page - 1) * limit
	if total == 0 || int64(offset) >= totalCount {
		return total, []MintedNFTItem{}, nil
	}

	var rows []model.NftOwnership
	if err := s.db.WithContext(ctx).Where("nft_contract = ? AND LOWER(owner_address) = ?", contract, ownerLower).
		Order("token_id").
		Offset(offset).Limit(limit).
		Find(&rows).Error; err != nil {
		return 0, nil, errors.NewDatabaseError(err)
	}

	nameMap := s.lookupOwnerNames([]string{owner})
	items = make([]MintedNFTItem, 0, len(rows))
	for _, row := range rows {
		item := MintedNFTItem{TokenID: row.TokenID, Owner: row.OwnerAddress}
		if n, ok := nameMap[strings.ToLower(row.OwnerAddress)]; ok {
			item.OwnerName = &n
		}
		if s.nftContract != nil {
			meta, _ := s.GetOrFetchMetadata(ctx, contract, row.TokenID)
			if meta != nil {
				item.TokenURI = meta.TokenURI
				item.Name = meta.Name
				item.Description = meta.Description
				item.Image = meta.Image
			}
		}
		items = append(items, item)
	}
	return total, items, nil
}

type MintedNFTItem struct {
	TokenID     uint64  `json:"tokenId"`
	Owner       string  `json:"owner"`
	OwnerName   *string `json:"ownerName,omitempty"` // 有注册用户名则填充，否则 nil
	TokenURI    *string `json:"tokenUri,omitempty"`
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Image       *string `json:"image,omitempty"`
}

// lookupOwnerNames 批量查询地址列表对应的用户名，返回 map[小写地址]用户名
func (s *NFTService) lookupOwnerNames(addresses []string) map[string]string {
	if len(addresses) == 0 {
		return nil
	}
	var users []model.User
	s.db.Where("LOWER(wallet_address) IN ?", lowered(addresses)).Find(&users)
	m := make(map[string]string, len(users))
	for _, u := range users {
		m[strings.ToLower(u.WalletAddress)] = u.Username
	}
	return m
}

func lowered(addrs []string) []string {
	out := make([]string, len(addrs))
	for i, a := range addrs {
		out[i] = strings.ToLower(a)
	}
	return out
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
	// 先收集所有 owner 地址，再批量查用户名
	type ownerEntry struct {
		tokenID uint64
		owner   common.Address
	}
	entries := make([]ownerEntry, 0, endID-startID+1)
	ownerAddrs := make([]string, 0, endID-startID+1)
	for tokenID := startID; tokenID <= endID; tokenID++ {
		owner, err := s.nftContract.OwnerOf(ctx, contract, tokenID)
		if err != nil {
			continue
		}
		entries = append(entries, ownerEntry{tokenID, owner})
		ownerAddrs = append(ownerAddrs, owner.Hex())
	}
	nameMap := s.lookupOwnerNames(ownerAddrs)

	items = make([]MintedNFTItem, 0, len(entries))
	for _, e := range entries {
		meta, _ := s.GetOrFetchMetadata(ctx, contract, e.tokenID)
		item := MintedNFTItem{TokenID: e.tokenID, Owner: e.owner.Hex()}
		if n, ok := nameMap[strings.ToLower(e.owner.Hex())]; ok {
			item.OwnerName = &n
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
