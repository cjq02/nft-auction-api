package handler

import (
	"net/url"
	"strconv"

	"nft-auction-api/internal/errors"
	"nft-auction-api/internal/logger"
	"nft-auction-api/internal/response"
	"nft-auction-api/internal/service"

	"github.com/gin-gonic/gin"
)

type NFTHandler struct {
	nftService              *service.NFTService
	defaultNFTContractAddr  string
	logger                  *logger.Logger
}

func NewNFTHandler(nftService *service.NFTService, defaultNFTContractAddr string, appLogger *logger.Logger) *NFTHandler {
	return &NFTHandler{
		nftService:             nftService,
		defaultNFTContractAddr: defaultNFTContractAddr,
		logger:                 appLogger,
	}
}

func (h *NFTHandler) GetMetadata(c *gin.Context) {
	contract := c.Param("contract")
	tokenIDStr := c.Param("tokenId")

	tokenID, err := strconv.ParseUint(tokenIDStr, 10, 64)
	if err != nil {
		appErr := errors.NewValidationError("无效的 tokenId")
		response.HandleError(c, h.logger, appErr)
		return
	}

	metadata, err := h.nftService.GetOrFetchMetadata(c.Request.Context(), contract, tokenID)
	if err != nil {
		response.HandleError(c, h.logger, err)
		return
	}

	response.Success(c, gin.H{
		"nftContract": metadata.NFTContract,
		"tokenId":     metadata.TokenID,
		"tokenUri":    metadata.TokenURI,
		"name":        metadata.Name,
		"description": metadata.Description,
		"image":       metadata.Image,
	})
}

// GetImage 图片代理：GET /api/nfts/image?u=<url-encoded-uri>，拉取 ipfs/https 图片并缓存，加速前端展示
func (h *NFTHandler) GetImage(c *gin.Context) {
	raw := c.Query("u")
	if raw == "" {
		response.HandleError(c, h.logger, errors.NewValidationError("缺少参数 u"))
		return
	}
	imageURI, err := url.QueryUnescape(raw)
	if err != nil {
		imageURI = raw
	}
	data, contentType, err := h.nftService.GetImageProxy(c.Request.Context(), imageURI)
	if err != nil {
		response.HandleError(c, h.logger, err)
		return
	}
	c.Header("Cache-Control", "public, max-age=86400")
	c.Data(200, contentType, data)
}

// List 已铸造 NFT 列表：GET /api/nfts/list?contract=0x...&owner=0x...&page=1&limit=20
// contract 可选，不传则用配置的 NFT_CONTRACT_ADDRESS；owner 可选，传则只返回该地址持有的 NFT
func (h *NFTHandler) List(c *gin.Context) {
	contract := c.Query("contract")
	if contract == "" {
		contract = h.defaultNFTContractAddr
	}
	if contract == "" {
		response.HandleError(c, h.logger, errors.NewValidationError("请指定 contract 或配置 NFT_CONTRACT_ADDRESS"))
		return
	}
	owner := c.Query("owner")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	var (
		total uint64
		items interface{}
		err   error
	)
	if owner != "" {
		total, items, err = h.nftService.GetNFTsOwnedBy(c.Request.Context(), contract, owner, page, limit)
	} else {
		total, items, err = h.nftService.ListMintedNFTs(c.Request.Context(), contract, page, limit)
	}
	if err != nil {
		response.HandleError(c, h.logger, err)
		return
	}

	response.Success(c, gin.H{
		"total": total,
		"page":  page,
		"limit": limit,
		"items": items,
	})
}
