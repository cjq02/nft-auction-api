package model

import (
	"time"
)

type User struct {
	ID            uint      `json:"id" gorm:"primaryKey"`
	Username      string    `json:"username" gorm:"size:100;uniqueIndex;not null"`
	PasswordHash  string    `json:"-" gorm:"column:password_hash;size:255;not null"`
	Email         *string   `json:"email,omitempty" gorm:"size:100;uniqueIndex"`           // 选填
	WalletAddress string    `json:"walletAddress" gorm:"column:wallet_address;size:42;uniqueIndex;not null"` // 必填
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

func (User) TableName() string {
	return "t_users"
}

type RegisterRequest struct {
	Username      string `json:"username" binding:"required,min=3,max=50"`
	Password      string `json:"password" binding:"required,min=6,max=100"`
	Email         string `json:"email,omitempty"` // 选填，若填则需符合邮箱格式
	WalletAddress string `json:"walletAddress" binding:"required"`
}

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// ConnectWalletRequest 前端连接钱包后调用，仅传钱包地址即可获得 JWT（无用户名密码）
type ConnectWalletRequest struct {
	WalletAddress string `json:"walletAddress" binding:"required"`
}

// UpdateProfileRequest 修改当前用户资料（仅支持 username、email，不修改钱包地址）
type UpdateProfileRequest struct {
	Username *string `json:"username,omitempty"` // 选填，3-50 字符，唯一
	Email    *string `json:"email,omitempty"`    // 选填，传空字符串表示清空
}

type UserResponse struct {
	ID            uint      `json:"id"`
	Username      string    `json:"username"`
	Email         *string   `json:"email,omitempty"`
	WalletAddress string    `json:"walletAddress"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

func (u *User) ToResponse() *UserResponse {
	return &UserResponse{
		ID:            u.ID,
		Username:      u.Username,
		Email:         u.Email,
		WalletAddress: u.WalletAddress,
		CreatedAt:     u.CreatedAt,
		UpdatedAt:     u.UpdatedAt,
	}
}
