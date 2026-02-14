package model

import (
	"time"
)

type User struct {
	ID            uint      `json:"id" gorm:"primaryKey"`
	Username      string    `json:"username" gorm:"size:100;uniqueIndex;not null"`
	PasswordHash  string    `json:"-" gorm:"column:password_hash;size:255;not null"`
	Email         string    `json:"email" gorm:"size:100;uniqueIndex;not null"`
	WalletAddress *string   `json:"walletAddress,omitempty" gorm:"size:42;uniqueIndex"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

func (User) TableName() string {
	return "t_users"
}

type RegisterRequest struct {
	Username      string  `json:"username" binding:"required,min=3,max=50"`
	Password      string  `json:"password" binding:"required,min=6,max=100"`
	Email         string  `json:"email" binding:"required,email"`
	WalletAddress *string `json:"walletAddress,omitempty"`
}

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type UserResponse struct {
	ID            uint      `json:"id"`
	Username      string    `json:"username"`
	Email         string    `json:"email"`
	WalletAddress *string   `json:"walletAddress,omitempty"`
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
