package service

import (
	"nft-auction-api/internal/errors"
	"nft-auction-api/internal/model"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type UserService struct {
	db *gorm.DB
}

func NewUserService(db *gorm.DB) *UserService {
	return &UserService{db: db}
}

func (s *UserService) Register(req *model.RegisterRequest) (*model.User, error) {
	var existingUser model.User
	if err := s.db.Where("username = ?", req.Username).First(&existingUser).Error; err == nil {
		return nil, errors.NewValidationError("用户名已存在")
	} else if err != gorm.ErrRecordNotFound {
		return nil, errors.NewDatabaseError(err)
	}

	if err := s.db.Where("email = ?", req.Email).First(&existingUser).Error; err == nil {
		return nil, errors.NewValidationError("邮箱已存在")
	} else if err != gorm.ErrRecordNotFound {
		return nil, errors.NewDatabaseError(err)
	}

	if req.WalletAddress != nil && *req.WalletAddress != "" {
		if err := s.db.Where("wallet_address = ?", *req.WalletAddress).First(&existingUser).Error; err == nil {
			return nil, errors.NewValidationError("钱包地址已绑定")
		} else if err != gorm.ErrRecordNotFound {
			return nil, errors.NewDatabaseError(err)
		}
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, errors.NewInternalError("密码加密失败", err)
	}

	user := &model.User{
		Username:      req.Username,
		PasswordHash:  string(passwordHash),
		Email:         req.Email,
		WalletAddress: req.WalletAddress,
	}

	if err := s.db.Create(user).Error; err != nil {
		return nil, errors.NewDatabaseError(err)
	}

	return user, nil
}

func (s *UserService) Login(req *model.LoginRequest) (*model.User, error) {
	var user model.User
	if err := s.db.Where("username = ?", req.Username).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errors.NewAuthError("用户名或密码错误")
		}
		return nil, errors.NewDatabaseError(err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return nil, errors.NewAuthError("用户名或密码错误")
	}

	return &user, nil
}

func (s *UserService) GetByID(id uint) (*model.User, error) {
	var user model.User
	if err := s.db.First(&user, id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}
