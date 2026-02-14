package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nft-auction-api/internal/errors"
	"nft-auction-api/internal/logger"
)

type Response struct {
	Code    int         `json:"code"`
	Data    interface{} `json:"data"`
	Message string      `json:"message"`
}

const (
	ErrCodeValidationFailed = 1001
	ErrCodeAuthFailed       = 1002
	ErrCodeDatabaseError    = 2001
)

func Success(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Response{Code: 0, Data: data})
}

func SuccessWithMessage(c *gin.Context, data interface{}, message string) {
	c.JSON(http.StatusOK, Response{Code: 0, Data: data, Message: message})
}

func Error(c *gin.Context, code int, msg string) {
	c.JSON(http.StatusOK, Response{Code: code, Message: msg})
}

func HandleError(c *gin.Context, appLogger *logger.Logger, err error) {
	if err == nil {
		return
	}

	appErr, ok := errors.IsAppError(err)
	if !ok {
		appErr = errors.WrapError(err)
	}

	if appErr.HTTPStatus >= http.StatusInternalServerError {
		appLogger.Error("错误: %s, 原始错误: %v, 路径: %s, 方法: %s",
			appErr.Message, appErr.Err, c.Request.URL.Path, c.Request.Method)
	} else if appErr.HTTPStatus >= http.StatusBadRequest {
		appLogger.Warn("客户端错误: %s, 路径: %s, 方法: %s",
			appErr.Message, c.Request.URL.Path, c.Request.Method)
	}

	c.JSON(appErr.HTTPStatus, Response{
		Code:    appErr.Code,
		Message: appErr.Message,
	})
}

func HandleGormError(c *gin.Context, appLogger *logger.Logger, err error, resourceName string) {
	if err == nil {
		return
	}

	if err == gorm.ErrRecordNotFound {
		appErr := errors.NewNotFoundError(resourceName + "不存在")
		appLogger.Warn("资源不存在: %s, 路径: %s", resourceName, c.Request.URL.Path)
		c.JSON(appErr.HTTPStatus, Response{
			Code:    appErr.Code,
			Message: appErr.Message,
		})
		return
	}

	appErr := errors.NewDatabaseError(err)
	appLogger.Error("数据库错误: %v, 路径: %s, 方法: %s", err, c.Request.URL.Path, c.Request.Method)
	c.JSON(appErr.HTTPStatus, Response{
		Code:    appErr.Code,
		Message: appErr.Message,
	})
}
