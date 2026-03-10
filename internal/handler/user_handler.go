package handler

import (
	"nft-auction-api/internal/errors"
	"nft-auction-api/internal/logger"
	"nft-auction-api/internal/model"
	"nft-auction-api/internal/response"
	"nft-auction-api/internal/service"

	"github.com/gin-gonic/gin"
)

type UserHandler struct {
	userService         *service.UserService
	jwtService          *service.JWTService
	auctionService      *service.AuctionService
	nftService          *service.NFTService
	nftContractAddress  string
	auctionContractAddr string
	logger              *logger.Logger
}

func NewUserHandler(
	userService *service.UserService,
	jwtService *service.JWTService,
	auctionService *service.AuctionService,
	nftService *service.NFTService,
	nftContractAddress string,
	auctionContractAddr string,
	appLogger *logger.Logger,
) *UserHandler {
	return &UserHandler{
		userService:         userService,
		jwtService:          jwtService,
		auctionService:      auctionService,
		nftService:          nftService,
		nftContractAddress:  nftContractAddress,
		auctionContractAddr: auctionContractAddr,
		logger:              appLogger,
	}
}

func (h *UserHandler) Register(c *gin.Context) {
	var req model.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		appErr := errors.NewValidationError("请求参数无效: " + err.Error())
		response.HandleError(c, h.logger, appErr)
		return
	}

	user, err := h.userService.Register(&req)
	if err != nil {
		response.HandleError(c, h.logger, err)
		return
	}

	h.logger.Info("新用户注册: %s (ID: %d)", req.Username, user.ID)
	response.SuccessWithMessage(c, user.ToResponse(), "注册成功")
}

func (h *UserHandler) Login(c *gin.Context) {
	var req model.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		appErr := errors.NewValidationError("请求参数无效: " + err.Error())
		response.HandleError(c, h.logger, appErr)
		return
	}

	user, err := h.userService.Login(&req)
	if err != nil {
		h.logger.Warn("登录失败: 用户名 %s", req.Username)
		response.HandleError(c, h.logger, err)
		return
	}

	token, err := h.jwtService.GenerateToken(user.ID)
	if err != nil {
		appErr := errors.NewInternalError("生成令牌失败", err)
		response.HandleError(c, h.logger, appErr)
		return
	}

	h.logger.Info("用户登录成功: %s (ID: %d)", user.Username, user.ID)
	response.SuccessWithMessage(c, gin.H{
		"user":  user.ToResponse(),
		"token": token,
	}, "登录成功")
}

// ConnectWallet 前端连接钱包后调用：传钱包地址，查或建用户并返回 JWT（无用户名密码）
func (h *UserHandler) ConnectWallet(c *gin.Context) {
	var req model.ConnectWalletRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		appErr := errors.NewValidationError("请求参数无效: " + err.Error())
		response.HandleError(c, h.logger, appErr)
		return
	}

	user, err := h.userService.ConnectOrCreateByWallet(req.WalletAddress)
	if err != nil {
		response.HandleError(c, h.logger, err)
		return
	}

	token, err := h.jwtService.GenerateToken(user.ID)
	if err != nil {
		appErr := errors.NewInternalError("生成令牌失败", err)
		response.HandleError(c, h.logger, appErr)
		return
	}

	h.logger.Info("钱包连接: %s (ID: %d)", req.WalletAddress, user.ID)
	response.Success(c, gin.H{
		"user":  user.ToResponse(),
		"token": token,
	})
}

func (h *UserHandler) Logout(c *gin.Context) {
	userIDInterface, _ := c.Get("userID")
	if userID, ok := userIDInterface.(uint); ok {
		h.logger.Info("用户退出登录: ID %d", userID)
	}
	response.SuccessWithMessage(c, gin.H{}, "退出登录成功")
}

// List 用户列表（用户名+钱包地址）；若配置了 NFT/拍卖合约，则附带每用户的 nftCount、auctionCount（正在拍卖数）
func (h *UserHandler) List(c *gin.Context) {
	list, err := h.userService.List()
	if err != nil {
		response.HandleError(c, h.logger, err)
		return
	}
	ctx := c.Request.Context()
	withStats := h.nftContractAddress != "" && h.auctionContractAddr != ""
	if withStats {
		out := make([]gin.H, 0, len(list))
		for _, u := range list {
			item := gin.H{
				"id":             u.ID,
				"username":       u.Username,
				"email":          u.Email,
				"walletAddress":  u.WalletAddress,
				"createdAt":      u.CreatedAt,
				"updatedAt":      u.UpdatedAt,
			}
			nftCount, errNft := h.nftService.CountByOwner(ctx, h.nftContractAddress, u.WalletAddress)
			if errNft == nil {
				item["nftCount"] = nftCount
			}
			auctionCount, errAuc := h.auctionService.CountActiveBySeller(u.WalletAddress, h.auctionContractAddr)
			if errAuc == nil {
				item["auctionCount"] = auctionCount
			}
			out = append(out, item)
		}
		response.Success(c, gin.H{"list": out})
		return
	}
	response.Success(c, gin.H{"list": list})
}

func (h *UserHandler) GetProfile(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		appErr := errors.NewAuthError("未找到用户信息")
		response.HandleError(c, h.logger, appErr)
		return
	}

	u, err := h.userService.GetByID(userID.(uint))
	if err != nil {
		response.HandleGormError(c, h.logger, err, "用户")
		return
	}

	response.Success(c, u.ToResponse())
}

// UpdateProfile 修改当前用户资料（username、email），需登录
func (h *UserHandler) UpdateProfile(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		appErr := errors.NewAuthError("未找到用户信息")
		response.HandleError(c, h.logger, appErr)
		return
	}

	var req model.UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		appErr := errors.NewValidationError("请求参数无效: " + err.Error())
		response.HandleError(c, h.logger, appErr)
		return
	}
	if req.Username == nil && req.Email == nil {
		appErr := errors.NewValidationError("请至少提供 username 或 email 之一")
		response.HandleError(c, h.logger, appErr)
		return
	}

	u, err := h.userService.UpdateProfile(userID.(uint), &req)
	if err != nil {
		response.HandleError(c, h.logger, err)
		return
	}

	h.logger.Info("用户更新资料: ID %d", u.ID)
	response.Success(c, u.ToResponse())
}
