package handler

import (
	"strconv"

	"nft-auction-api/internal/errors"
	"nft-auction-api/internal/logger"
	"nft-auction-api/internal/response"
	"nft-auction-api/internal/service"

	"github.com/gin-gonic/gin"
)

type NFTHandler struct {
	nftService *service.NFTService
	logger     *logger.Logger
}

func NewNFTHandler(nftService *service.NFTService, appLogger *logger.Logger) *NFTHandler {
	return &NFTHandler{
		nftService: nftService,
		logger:     appLogger,
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

	metadata, err := h.nftService.GetMetadata(contract, tokenID)
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
