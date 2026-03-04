package app

import (
	"os"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nft-auction-api/internal/blockchain"
	"nft-auction-api/internal/config"
	"nft-auction-api/internal/handler"
	"nft-auction-api/internal/logger"
	"nft-auction-api/internal/middleware"
	"nft-auction-api/internal/service"
)

func SetupRouter(
	db *gorm.DB,
	userService *service.UserService,
	auctionService *service.AuctionService,
	bidService *service.BidService,
	nftService *service.NFTService,
	nftContract *blockchain.NFTContract,
	nftContractAddress string,
	backfillStartBlock uint64,
	defaultAuctionContractAddress string,
	appConfig *config.AppConfig,
	appLogger *logger.Logger,
) *gin.Engine {
	if os.Getenv("APP_ENV") == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.CORSMiddleware())

	secretKey := os.Getenv("JWT_SECRET_KEY")
	if secretKey == "" {
		secretKey = "nft-auction-secret-key-change-in-production"
	}
	jwtService := service.NewJWTService(secretKey)
	authMiddleware := middleware.NewAuthMiddleware(secretKey)

	userHandler := handler.NewUserHandler(userService, jwtService, appLogger)
	auctionHandler := handler.NewAuctionHandler(auctionService, bidService, nftService, backfillStartBlock, defaultAuctionContractAddress, appLogger)
	nftHandler := handler.NewNFTHandler(nftService, nftContractAddress, appLogger)
	overviewHandler := handler.NewOverviewHandler(db, auctionService, nftContract, nftContractAddress, appLogger)

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	r.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "pong"})
	})

	api := r.Group("/api")
	{
		auth := api.Group("/auth")
		{
			auth.POST("/wallet", userHandler.ConnectWallet)   // 前端连接钱包：POST { "walletAddress": "0x..." }，返回 user + token
			auth.POST("/register", userHandler.Register)     // 可选，传统注册
			auth.POST("/login", userHandler.Login)           // 可选，传统登录
			auth.POST("/logout", authMiddleware.JWTAuth(), userHandler.Logout)
		}

		users := api.Group("/users")
		{
			users.GET("/list", userHandler.List)
			users.GET("/me", authMiddleware.JWTAuth(), userHandler.GetProfile)
			users.PATCH("/me", authMiddleware.JWTAuth(), userHandler.UpdateProfile) // 修改当前用户资料
			users.GET("/:address/auctions", auctionHandler.ListByAddress)
		}

		auctions := api.Group("/auctions")
		{
			auctions.GET("", auctionHandler.List)
			auctions.GET("/:id", auctionHandler.GetByID)
			auctions.GET("/:id/bids", auctionHandler.ListBids)
			// POST "" (sync via txHash) removed — replaced by WebSocket event listener
		}

		nfts := api.Group("/nfts")
		{
			nfts.GET("/list", nftHandler.List)
			nfts.GET("/:contract/:tokenId", nftHandler.GetMetadata)
		}

		api.GET("/overview", overviewHandler.GetOverview)

		// 管理员补录：将链上历史拍卖同步到数据库
		admin := api.Group("/admin")
		{
			admin.POST("/backfill-auctions", auctionHandler.Backfill)
		}
	}

	return r
}
