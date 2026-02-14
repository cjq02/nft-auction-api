package errors

import (
	"errors"
	"net/http"
)

type AppError struct {
	Code       int
	HTTPStatus int
	Message    string
	Err        error
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Message
}

func (e *AppError) Unwrap() error {
	return e.Err
}

const (
	ErrCodeValidationFailed   = 1001
	ErrCodeAuthFailed         = 1002
	ErrCodeForbidden          = 1003
	ErrCodeNotFound           = 1004
	ErrCodeDatabaseError      = 2001
	ErrCodeInternalError      = 2002
	ErrCodeRPCConnectionError = 3001
	ErrCodeContractCallError  = 3002
)

func NewValidationError(message string) *AppError {
	return &AppError{
		Code:       ErrCodeValidationFailed,
		HTTPStatus: http.StatusBadRequest,
		Message:    message,
	}
}

func NewAuthError(message string) *AppError {
	return &AppError{
		Code:       ErrCodeAuthFailed,
		HTTPStatus: http.StatusUnauthorized,
		Message:    message,
	}
}

func NewForbiddenError(message string) *AppError {
	return &AppError{
		Code:       ErrCodeForbidden,
		HTTPStatus: http.StatusForbidden,
		Message:    message,
	}
}

func NewNotFoundError(message string) *AppError {
	return &AppError{
		Code:       ErrCodeNotFound,
		HTTPStatus: http.StatusNotFound,
		Message:    message,
	}
}

func NewDatabaseError(err error) *AppError {
	return &AppError{
		Code:       ErrCodeDatabaseError,
		HTTPStatus: http.StatusInternalServerError,
		Message:    "数据库操作失败",
		Err:        err,
	}
}

func NewInternalError(message string, err error) *AppError {
	return &AppError{
		Code:       ErrCodeInternalError,
		HTTPStatus: http.StatusInternalServerError,
		Message:    message,
		Err:        err,
	}
}

func NewBlockchainError(message string, err error) *AppError {
	return &AppError{
		Code:       ErrCodeContractCallError,
		HTTPStatus: http.StatusBadGateway,
		Message:    message,
		Err:        err,
	}
}

func IsAppError(err error) (*AppError, bool) {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr, true
	}
	return nil, false
}

func WrapError(err error) *AppError {
	if err == nil {
		return nil
	}
	if appErr, ok := IsAppError(err); ok {
		return appErr
	}
	return NewDatabaseError(err)
}
