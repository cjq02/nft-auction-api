package handler

import (
	"nft-auction-api/internal/blockchain"
	"nft-auction-api/internal/logger"
	"nft-auction-api/internal/response"
	"nft-auction-api/internal/service"

	"github.com/gin-gonic/gin"
)

type OverviewHandler struct {
	auctionService     *service.AuctionService
	nftContract        *blockchain.NFTContract
	nftContractAddress string
	logger             *logger.Logger
}

func NewOverviewHandler(
	auctionService *service.AuctionService,
	nftContract *blockchain.NFTContract,
	nftContractAddress string,
	appLogger *logger.Logger,
) *OverviewHandler {
	return &OverviewHandler{
		auctionService:     auctionService,
		nftContract:        nftContract,
		nftContractAddress: nftContractAddress,
		logger:             appLogger,
	}
}

// GetOverview 返回数据概览：拍卖数量（总数/进行中/已结束）、NFT 总供应量等
func (h *OverviewHandler) GetOverview(c *gin.Context) {
	ctx := c.Request.Context()

	total, active, ended, err := h.auctionService.Counts()
	if err != nil {
		response.HandleError(c, h.logger, err)
		return
	}

	data := gin.H{
		"auction": gin.H{
			"total":  total,
			"active": active,
			"ended":  ended,
		},
		"nft": gin.H{
			"totalSupply": nil,
			"nextTokenId": nil,
		},
	}

	if h.nftContract != nil && h.nftContractAddress != "" {
		supply, err := h.nftContract.TotalSupply(ctx, h.nftContractAddress)
		if err != nil {
			h.logger.Warn("获取 NFT totalSupply 失败: %v", err)
		} else {
			data["nft"].(gin.H)["totalSupply"] = supply
		}
		nextID, err := h.nftContract.NextTokenId(ctx, h.nftContractAddress)
		if err != nil {
			h.logger.Warn("获取 NFT nextTokenId 失败: %v", err)
		} else {
			data["nft"].(gin.H)["nextTokenId"] = nextID
		}
	}

	response.Success(c, data)
}
