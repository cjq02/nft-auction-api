package service

import (
	"crypto/rand"
	"encoding/hex"
	"strings"

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

	if req.Email != "" {
		if err := s.db.Where("email = ?", req.Email).First(&existingUser).Error; err == nil {
			return nil, errors.NewValidationError("邮箱已存在")
		} else if err != gorm.ErrRecordNotFound {
			return nil, errors.NewDatabaseError(err)
		}
	}

	if err := s.db.Where("wallet_address = ?", req.WalletAddress).First(&existingUser).Error; err == nil {
		return nil, errors.NewValidationError("钱包地址已绑定")
	} else if err != gorm.ErrRecordNotFound {
		return nil, errors.NewDatabaseError(err)
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, errors.NewInternalError("密码加密失败", err)
	}

	var email *string
	if req.Email != "" {
		email = &req.Email
	}
	user := &model.User{
		Username:      req.Username,
		PasswordHash:  string(passwordHash),
		Email:         email,
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

// List 返回所有用户（仅 id、username、wallet_address），供铸造页下拉等使用
func (s *UserService) List() ([]*model.UserResponse, error) {
	var users []model.User
	if err := s.db.Select("id", "username", "wallet_address", "created_at", "updated_at").Order("username").Find(&users).Error; err != nil {
		return nil, errors.NewDatabaseError(err)
	}
	out := make([]*model.UserResponse, 0, len(users))
	for i := range users {
		out = append(out, users[i].ToResponse())
	}
	return out, nil
}

// ConnectOrCreateByWallet 根据钱包地址查找用户，不存在则创建（无密码，仅钱包登录）
func (s *UserService) ConnectOrCreateByWallet(walletAddress string) (*model.User, error) {
	var user model.User
	if err := s.db.Where("wallet_address = ?", walletAddress).First(&user).Error; err == nil {
		return &user, nil
	} else if err != gorm.ErrRecordNotFound {
		return nil, errors.NewDatabaseError(err)
	}

	// 新用户：用钱包地址作为 username，密码为随机不可用哈希（仅支持钱包登录）
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return nil, errors.NewInternalError("生成随机密码失败", err)
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(hex.EncodeToString(randomBytes)), bcrypt.DefaultCost)
	if err != nil {
		return nil, errors.NewInternalError("密码加密失败", err)
	}

	user = model.User{
		Username:      walletAddress, // 唯一且可识别
		PasswordHash:  string(passwordHash),
		Email:         nil,
		WalletAddress: walletAddress,
	}
	if err := s.db.Create(&user).Error; err != nil {
		return nil, errors.NewDatabaseError(err)
	}
	return &user, nil
}

// UpdateProfile 更新用户资料（仅 username、email），不修改钱包地址
func (s *UserService) UpdateProfile(userID uint, req *model.UpdateProfileRequest) (*model.User, error) {
	var user model.User
	if err := s.db.First(&user, userID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errors.NewNotFoundError("用户不存在")
		}
		return nil, errors.NewDatabaseError(err)
	}

	if req.Username != nil {
		username := strings.TrimSpace(*req.Username)
		if len(username) < 3 || len(username) > 50 {
			return nil, errors.NewValidationError("用户名长度需 3～50 个字符")
		}
		var existing model.User
		if err := s.db.Where("username = ? AND id != ?", username, userID).First(&existing).Error; err == nil {
			return nil, errors.NewValidationError("用户名已被使用")
		} else if err != gorm.ErrRecordNotFound {
			return nil, errors.NewDatabaseError(err)
		}
		user.Username = username
	}

	if req.Email != nil {
		email := strings.TrimSpace(*req.Email)
		if email != "" {
			var existing model.User
			if err := s.db.Where("email = ? AND id != ?", email, userID).First(&existing).Error; err == nil {
				return nil, errors.NewValidationError("邮箱已被使用")
			} else if err != gorm.ErrRecordNotFound {
				return nil, errors.NewDatabaseError(err)
			}
		}
		if email == "" {
			user.Email = nil
		} else {
			user.Email = &email
		}
	}

	if err := s.db.Save(&user).Error; err != nil {
		return nil, errors.NewDatabaseError(err)
	}
	return &user, nil
}
