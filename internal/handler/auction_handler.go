package handler

import (
	"strconv"

	"nft-auction-api/internal/errors"
	"nft-auction-api/internal/logger"
	"nft-auction-api/internal/model"
	"nft-auction-api/internal/response"
	"nft-auction-api/internal/service"

	"github.com/gin-gonic/gin"
)

type AuctionHandler struct {
	auctionService *service.AuctionService
	bidService     *service.BidService
	nftService     *service.NFTService
	logger         *logger.Logger
}

func NewAuctionHandler(
	auctionService *service.AuctionService,
	bidService *service.BidService,
	nftService *service.NFTService,
	appLogger *logger.Logger,
) *AuctionHandler {
	return &AuctionHandler{
		auctionService: auctionService,
		bidService:     bidService,
		nftService:     nftService,
		logger:         appLogger,
	}
}

func (h *AuctionHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	status := c.Query("status")

	items, total, err := h.auctionService.List(page, limit, status)
	if err != nil {
		response.HandleError(c, h.logger, err)
		return
	}

	// 简化列表响应，不包含 bid 和 nft（减少查询）
	var list []gin.H
	for _, item := range items {
		list = append(list, auctionToResponse(&item, nil, nil))
	}

	response.Success(c, gin.H{
		"items": list,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

func (h *AuctionHandler) GetByID(c *gin.Context) {
	auctionID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		appErr := errors.NewValidationError("无效的拍卖ID")
		response.HandleError(c, h.logger, appErr)
		return
	}

	auction, err := h.auctionService.GetByAuctionID(auctionID)
	if err != nil {
		response.HandleError(c, h.logger, err)
		return
	}

	var highestBid *model.BidIndex
	bids, _ := h.bidService.ListByAuctionID(auctionID)
	if len(bids) > 0 {
		highestBid = &bids[0]
	}

	var nft *model.NFTMetadata
	nft, _ = h.nftService.GetMetadata(auction.NFTContract, auction.TokenID)

	response.Success(c, auctionDetailResponse(auction, highestBid, bids, nft))
}

func (h *AuctionHandler) ListByAddress(c *gin.Context) {
	address := c.Param("address")
	if address == "" {
		response.HandleError(c, h.logger, errors.NewValidationError("无效的地址"))
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))

	items, total, err := h.auctionService.ListBySeller(address, page, limit)
	if err != nil {
		response.HandleError(c, h.logger, err)
		return
	}

	var list []gin.H
	for _, item := range items {
		list = append(list, auctionToResponse(&item, nil, nil))
	}

	response.Success(c, gin.H{
		"items": list,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

func (h *AuctionHandler) ListBids(c *gin.Context) {
	auctionID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		appErr := errors.NewValidationError("无效的拍卖ID")
		response.HandleError(c, h.logger, appErr)
		return
	}

	_, err = h.auctionService.GetByAuctionID(auctionID)
	if err != nil {
		response.HandleError(c, h.logger, err)
		return
	}

	bids, err := h.bidService.ListByAuctionID(auctionID)
	if err != nil {
		response.HandleError(c, h.logger, err)
		return
	}

	var list []gin.H
	for _, b := range bids {
		list = append(list, gin.H{
			"bidder":     b.Bidder,
			"amount":     b.Amount,
			"timestamp":  b.BidTimestamp,
			"isEth":      b.IsETH,
			"createdAt":  b.CreatedAt,
		})
	}

	response.Success(c, gin.H{"bids": list})
}

func (h *AuctionHandler) Create(c *gin.Context) {
	var req model.CreateAuctionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		appErr := errors.NewValidationError("请求参数无效: " + err.Error())
		response.HandleError(c, h.logger, appErr)
		return
	}

	params := h.auctionService.PrepareCreateParams(&req)
	response.Success(c, gin.H{
		"message": "请使用钱包调用合约 createAuction 完成创建，以下为调用参数",
		"params":  params,
	})
}

func auctionToResponse(a *model.AuctionIndex, highestBid *model.BidIndex, nft *model.NFTMetadata) gin.H {
	resp := gin.H{
		"auctionId":    a.AuctionID,
		"seller":       a.Seller,
		"nftContract":  a.NFTContract,
		"tokenId":      a.TokenID,
		"startTime":    a.StartTime,
		"endTime":      a.EndTime,
		"minBid":       a.MinBid,
		"paymentToken": a.PaymentToken,
		"status":       a.Status,
	}
	if highestBid != nil {
		resp["highestBid"] = gin.H{
			"bidder":    highestBid.Bidder,
			"amount":    highestBid.Amount,
			"timestamp": highestBid.BidTimestamp,
			"isEth":     highestBid.IsETH,
		}
	}
	if nft != nil {
		resp["nft"] = gin.H{
			"name":  nft.Name,
			"image": nft.Image,
		}
	}
	return resp
}

func auctionDetailResponse(a *model.AuctionIndex, highestBid *model.BidIndex, bids []model.BidIndex, nft *model.NFTMetadata) gin.H {
	resp := auctionToResponse(a, highestBid, nft)

	var bidsList []gin.H
	for _, b := range bids {
		bidsList = append(bidsList, gin.H{
			"bidder":    b.Bidder,
			"amount":    b.Amount,
			"timestamp": b.BidTimestamp,
			"isEth":     b.IsETH,
		})
	}
	resp["bids"] = bidsList

	if nft != nil {
		resp["nft"] = gin.H{
			"tokenURI":   nft.TokenURI,
			"name":       nft.Name,
			"description": nft.Description,
			"image":      nft.Image,
		}
	}

	return resp
}
