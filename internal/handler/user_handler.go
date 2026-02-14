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
	userService *service.UserService
	jwtService  *service.JWTService
	logger      *logger.Logger
}

func NewUserHandler(userService *service.UserService, jwtService *service.JWTService, appLogger *logger.Logger) *UserHandler {
	return &UserHandler{
		userService: userService,
		jwtService:  jwtService,
		logger:      appLogger,
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

func (h *UserHandler) Logout(c *gin.Context) {
	userIDInterface, _ := c.Get("userID")
	if userID, ok := userIDInterface.(uint); ok {
		h.logger.Info("用户退出登录: ID %d", userID)
	}
	response.SuccessWithMessage(c, gin.H{}, "退出登录成功")
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
