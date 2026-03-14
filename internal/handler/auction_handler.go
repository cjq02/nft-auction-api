package handler

import (
	"strconv"
	"strings"

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
	userService         *service.UserService
	backfillStartBlock  uint64
	defaultContractAddr string // 默认拍卖合约，用于 Backfill 与 ListBids 时解析空 contract
	logger              *logger.Logger
}

func NewAuctionHandler(
	auctionService *service.AuctionService,
	bidService *service.BidService,
	nftService *service.NFTService,
	userService *service.UserService,
	backfillStartBlock uint64,
	defaultContractAddr string,
	appLogger *logger.Logger,
) *AuctionHandler {
	return &AuctionHandler{
		auctionService:      auctionService,
		bidService:          bidService,
		nftService:          nftService,
		userService:         userService,
		backfillStartBlock:  backfillStartBlock,
		defaultContractAddr: defaultContractAddr,
		logger:              appLogger,
	}
}

func (h *AuctionHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	status := c.Query("status")
	contract := c.Query("contract")

	items, total, err := h.auctionService.List(page, limit, status, contract)
	if err != nil {
		h.logger.Error("auction_list failed page=%d limit=%d status=%s err=%v", page, limit, status, err)
		response.HandleError(c, h.logger, err)
		return
	}
	if total == 0 {
		h.logger.Info("auction_list empty page=%d limit=%d status=%s (DB has no matching records)", page, limit, status)
	}

	// 批量查卖家账户名称
	sellerAddrs := make([]string, 0, len(items))
	seen := make(map[string]struct{})
	for _, item := range items {
		lower := strings.ToLower(item.Seller)
		if _, ok := seen[lower]; !ok {
			seen[lower] = struct{}{}
			sellerAddrs = append(sellerAddrs, item.Seller)
		}
	}
	sellerNames := h.userService.GetUsernamesByAddresses(sellerAddrs)

	// 批量查最高出价（仅对当前列表中的拍卖）
	auctionIDs := make([]uint64, 0, len(items))
	for _, item := range items {
		auctionIDs = append(auctionIDs, item.AuctionID)
	}
	// 列表接口中的最高出价主要用于前端展示；当未指定 contract 时，使用默认合约地址
	highestByID, _ := h.bidService.GetHighestBidsForAuctions(auctionIDs, contract)

	// 列表响应：附带 NFT 元数据、卖家名称与最高出价
	var list []gin.H
	for _, item := range items {
		hb := highestByID[item.AuctionID]
		// 若拍卖支付方式为 ERC20，则忽略 isEth=true 的最高出价（防止历史 ETH 拍卖残留干扰）
		if hb != nil && item.PaymentToken != nil && *item.PaymentToken != "" && *item.PaymentToken != "0x0000000000000000000000000000000000000000" && hb.IsETH {
			hb = nil
		}
		nft, _ := h.nftService.GetOrFetchMetadata(c.Request.Context(), item.NFTContract, item.TokenID)
		resp := auctionToResponse(&item, hb, nft)
		if n := sellerNames[strings.ToLower(item.Seller)]; n != "" {
			resp["sellerName"] = n
		}
		list = append(list, resp)
	}

	response.Success(c, gin.H{
		"items": list,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// Stats 返回平台统计数据（首页使用）：进行中拍卖总数、有出价的拍卖数、最高价合计（wei）
func (h *AuctionHandler) Stats(c *gin.Context) {
	contract := c.Query("contract")

	totalAuctions, bidCount, totalHighestBidWei, err := h.auctionService.StatsForActive(contract)
	if err != nil {
		response.HandleError(c, h.logger, err)
		return
	}

	totalValue := "0"
	if totalHighestBidWei != nil {
		totalValue = totalHighestBidWei.String()
	}

	response.Success(c, gin.H{
		"totalAuctions": totalAuctions,
		"bidCount":      bidCount,
		"totalValue":    totalValue,
	})
}

func (h *AuctionHandler) GetByID(c *gin.Context) {
	auctionID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		appErr := errors.NewValidationError("无效的拍卖ID")
		response.HandleError(c, h.logger, appErr)
		return
	}

	contract := c.Query("contract")
	auction, err := h.auctionService.GetByAuctionID(auctionID, contract)
	if err != nil {
		response.HandleError(c, h.logger, err)
		return
	}

	bidContract := auction.AuctionContract
	if bidContract == "" {
		bidContract = h.defaultContractAddr
	}
	var highestBid *model.BidIndex
	bids, _ := h.bidService.ListByAuctionID(auctionID, bidContract)
	if len(bids) > 0 {
		highestBid = &bids[0]
	}

	var nft *model.NFTMetadata
	nft, _ = h.nftService.GetOrFetchMetadata(c.Request.Context(), auction.NFTContract, auction.TokenID)

	addrs := make([]string, 0, 1+len(bids))
	addrs = append(addrs, auction.Seller)
	for _, b := range bids {
		addrs = append(addrs, b.Bidder)
	}
	bidderNames := h.userService.GetUsernamesByAddresses(addrs)
	var minBidEth string
	if auction.PaymentToken == nil || *auction.PaymentToken == "" || *auction.PaymentToken == "0x0000000000000000000000000000000000000000" {
		minBidEth, _ = h.auctionService.GetMinBidEth(c.Request.Context(), bidContract, auction.MinBid)
	}
	response.Success(c, auctionDetailResponse(auction, highestBid, bids, nft, bidderNames, minBidEth))
}

func (h *AuctionHandler) ListByAddress(c *gin.Context) {
	address := c.Param("address")
	if address == "" {
		response.HandleError(c, h.logger, errors.NewValidationError("无效的地址"))
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	contract := c.Query("contract")

	items, total, err := h.auctionService.ListBySeller(address, page, limit, contract)
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

	contract := c.Query("contract")
	auction, err := h.auctionService.GetByAuctionID(auctionID, contract)
	if err != nil {
		response.HandleError(c, h.logger, err)
		return
	}
	bidContract := auction.AuctionContract
	if bidContract == "" {
		bidContract = h.defaultContractAddr
	}
	bids, err := h.bidService.ListByAuctionID(auctionID, bidContract)
	if err != nil {
		response.HandleError(c, h.logger, err)
		return
	}

	addrs := make([]string, 0, len(bids))
	for _, b := range bids {
		addrs = append(addrs, b.Bidder)
	}
	names := h.userService.GetUsernamesByAddresses(addrs)

	var list []gin.H
	for _, b := range bids {
		item := gin.H{
			"bidder":     b.Bidder,
			"amount":     b.Amount,
			"timestamp":  b.BidTimestamp,
			"isEth":      b.IsETH,
			"createdAt":  b.CreatedAt,
		}
		if n := names[strings.ToLower(b.Bidder)]; n != "" {
			item["bidderName"] = n
		}
		list = append(list, item)
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
	contract := h.defaultContractAddr
	if c.Query("contract") != "" {
		contract = c.Query("contract")
	}
	result, err := h.auctionService.BackfillFromChain(c.Request.Context(), contract, h.backfillStartBlock)
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
		"auctionId":       a.AuctionID,
		"auctionContract": a.AuctionContract,
		"seller":          a.Seller,
		"nftContract":     a.NFTContract,
		"tokenId":         a.TokenID,
		"startTime":       a.StartTime,
		"endTime":         a.EndTime,
		"minBid":          a.MinBid,
		"paymentToken":    a.PaymentToken,
		"status":          a.Status,
	}
	if a.FeeAmount != nil {
		resp["feeAmount"] = a.FeeAmount
	}
	if a.FeeIsETH != nil {
		resp["feeIsETH"] = a.FeeIsETH
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

func auctionDetailResponse(a *model.AuctionIndex, highestBid *model.BidIndex, bids []model.BidIndex, nft *model.NFTMetadata, bidderNames map[string]string, minBidEth string) gin.H {
	resp := auctionToResponse(a, highestBid, nft)
	if minBidEth != "" {
		resp["minBidEth"] = minBidEth
	}
	if n := bidderNames[strings.ToLower(a.Seller)]; n != "" {
		resp["sellerName"] = n
	}

	var bidsList []gin.H
	for _, b := range bids {
		item := gin.H{
			"bidder":    b.Bidder,
			"amount":    b.Amount,
			"timestamp": b.BidTimestamp,
			"isEth":     b.IsETH,
		}
		if n := bidderNames[strings.ToLower(b.Bidder)]; n != "" {
			item["bidderName"] = n
		}
		bidsList = append(bidsList, item)
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
