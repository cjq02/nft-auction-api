package main

import (
	"io"
	"log"
	"os"

	"github.com/joho/godotenv"
	"gorm.io/gorm"

	"nft-auction-api/internal/app"
	"nft-auction-api/internal/blockchain"
	"nft-auction-api/internal/config"
	"nft-auction-api/internal/logger"
	"nft-auction-api/internal/metadata"
	"nft-auction-api/internal/model"
	"nft-auction-api/internal/service"
)

func main() {
	env := os.Getenv("GO_ENV")
	if env == "" {
		env = os.Getenv("APP_ENV")
	}
	if env == "" {
		env = "development"
	}

	envFile := ".env." + env
	if err := godotenv.Load(envFile); err != nil {
		if err := godotenv.Load(".env.local"); err != nil {
			_ = godotenv.Load()
		}
	}

	db, err := initDatabase()
	if err != nil {
		log.Fatal("Failed to initialize database:", err)
	}
	defer db.Close()

	logDir := os.Getenv("LOG_DIR")
	if logDir == "" {
		logDir = "./logs"
	}
	appLogger, err := logger.NewLogger(logDir)
	if err != nil {
		log.Fatal("Failed to initialize logger:", err)
	}
	defer appLogger.Close()

	log.SetOutput(io.MultiWriter(os.Stdout, appLogger.GetWriter()))

	appConfig := config.NewAppConfig(appLogger)
	bcConfig := config.NewBlockchainConfig()

	var bcClient *blockchain.Client
	var auctionContract *blockchain.AuctionContract
	var nftContract *blockchain.NFTContract

	if bcConfig.RPCURL != "" {
		var err error
		bcClient, err = blockchain.NewClient(bcConfig.RPCURL)
		if err != nil {
			log.Printf("Warning: Blockchain client init failed: %v, continuing without chain reads", err)
		} else if bcClient != nil {
			defer bcClient.Close()
			if bcConfig.AuctionContractAddress != "" {
				auctionContract, _ = blockchain.NewAuctionContract(bcClient, bcConfig.AuctionContractAddress)
			}
			nftContract, _ = blockchain.NewNFTContract(bcClient)
		}
	}

	metadataFetcher := metadata.NewFetcher(bcConfig.IPFSGateway)

	userService := service.NewUserService(db.DB)
	auctionService := service.NewAuctionService(db.DB, auctionContract)
	bidService := service.NewBidService(db.DB)
	nftService := service.NewNFTService(db.DB, nftContract, metadataFetcher)

	r := app.SetupRouter(userService, auctionService, bidService, nftService, nftContract, bcConfig.NFTContractAddress, appConfig, appLogger)

	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "9080"
	}

	log.Printf("Server starting on port %s", port)
	_ = appConfig
	if err := r.Run(":" + port); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}

func initDatabase() (*config.Database, error) {
	dbConfig := config.NewDatabaseConfig()
	db, err := config.NewDatabase(dbConfig)
	if err != nil {
		return nil, err
	}

	if err := autoMigrate(db.DB); err != nil {
		return nil, err
	}

	log.Println("Database connected successfully")
	return db, nil
}

func autoMigrate(db *gorm.DB) error {
	log.Println("Running auto-migration...")
	return db.AutoMigrate(
		&model.User{},
		&model.AuctionIndex{},
		&model.BidIndex{},
		&model.NFTMetadata{},
	)
}
