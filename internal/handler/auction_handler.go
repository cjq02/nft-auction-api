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
	auctionService      *service.AuctionService
	bidService          *service.BidService
	nftService          *service.NFTService
	backfillStartBlock  uint64
	logger              *logger.Logger
}

func NewAuctionHandler(
	auctionService *service.AuctionService,
	bidService *service.BidService,
	nftService *service.NFTService,
	backfillStartBlock uint64,
	appLogger *logger.Logger,
) *AuctionHandler {
	return &AuctionHandler{
		auctionService:     auctionService,
		bidService:         bidService,
		nftService:         nftService,
		backfillStartBlock: backfillStartBlock,
		logger:             appLogger,
	}
}

func (h *AuctionHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	status := c.Query("status")

	items, total, err := h.auctionService.List(page, limit, status)
	if err != nil {
		h.logger.Error("auction_list failed page=%d limit=%d status=%s err=%v", page, limit, status, err)
		response.HandleError(c, h.logger, err)
		return
	}
	if total == 0 {
		h.logger.Info("auction_list empty page=%d limit=%d status=%s (DB has no matching records)", page, limit, status)
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
	nft, _ = h.nftService.GetOrFetchMetadata(c.Request.Context(), auction.NFTContract, auction.TokenID)

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
		h.logger.Warn("auction_create bind_failed err=%v", err)
		appErr := errors.NewValidationError("请求参数无效: " + err.Error())
		response.HandleError(c, h.logger, appErr)
		return
	}
	h.logger.Info("auction_create request txHash=%s", req.TxHash)

	item, err := h.auctionService.IndexFromTxHash(c.Request.Context(), req.TxHash)
	if err != nil {
		h.logger.Error("auction_create failed txHash=%s err=%v", req.TxHash, err)
		response.HandleError(c, h.logger, err)
		return
	}
	h.logger.Info("auction_create ok txHash=%s auctionId=%d", req.TxHash, item.AuctionID)
	response.Success(c, auctionToResponse(item, nil, nil))
}

func (h *AuctionHandler) Backfill(c *gin.Context) {
	h.logger.Info("backfill start startBlock=%d", h.backfillStartBlock)
	result, err := h.auctionService.BackfillFromChain(c.Request.Context(), h.backfillStartBlock)
	if err != nil {
		h.logger.Error("backfill failed startBlock=%d err=%v", h.backfillStartBlock, err)
		response.HandleError(c, h.logger, err)
		return
	}
	h.logger.Info("backfill done startBlock=%d foundOnChain=%d added=%d", h.backfillStartBlock, result.FoundOnChain, result.Added)
	response.Success(c, gin.H{
		"foundOnChain": result.FoundOnChain,
		"added":        result.Added,
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
