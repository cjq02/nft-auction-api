package main

import (
	"context"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"gorm.io/gorm"

	"nft-auction-api/internal/app"
	"nft-auction-api/internal/blockchain"
	"nft-auction-api/internal/config"
	"nft-auction-api/internal/indexer"
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

	// HTTP client — for contract reads (getAuction, tokenURI, etc.)
	var bcClient *blockchain.Client
	var auctionContract *blockchain.AuctionContract
	var nftContract *blockchain.NFTContract

	if bcConfig.RPCURL != "" {
		bcClient, err = blockchain.NewClient(bcConfig.RPCURL)
		if err != nil {
			log.Printf("Warning: HTTP blockchain client init failed: %v", err)
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
	auctionService := service.NewAuctionService(db.DB, auctionContract, bcConfig.AuctionContractAddress)
	bidService := service.NewBidService(db.DB)
	nftService := service.NewNFTService(db.DB, nftContract, metadataFetcher)

	// WebSocket client — for event listener (SubscribeFilterLogs)
	// Run in a separate goroutine; cancelled via context on shutdown.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if bcConfig.WSRPCUrl != "" {
		wsClient, wsErr := blockchain.NewClient(bcConfig.WSRPCUrl)
		if wsErr != nil {
			log.Printf("Warning: WebSocket blockchain client init failed: %v — event listener disabled", wsErr)
		} else {
			defer wsClient.Close()
			auctionIdx := indexer.NewAuctionIndexer(
				db.DB,
				wsClient,
				auctionService,
				bidService,
				bcConfig.AuctionContractAddress,
				bcConfig.AuctionDeployBlock,
			)
			if auctionIdx.IsAvailable() {
				go auctionIdx.Start(ctx)
				log.Printf("Auction indexer started (WS: %s)", bcConfig.WSRPCUrl)
			}
			nftIdx := indexer.NewNFTIndexer(
				db.DB,
				wsClient,
				nftContract,
				bcConfig.NFTContractAddress,
				bcConfig.NFTDeployBlock, // 必须设置 NFT_DEPLOY_BLOCK，否则从 0 扫易触发 RPC internal error
				nftService, // 铸造时预填 t_nft_metadata
			)
			if nftIdx.IsAvailable() {
				go nftIdx.Start(ctx)
				log.Printf("NFT indexer started (contract: %s)", bcConfig.NFTContractAddress)
			}
		}
	} else {
		log.Printf("WS_RPC_URL not set — event listener disabled (set it to enable real-time indexing)")
	}

	r := app.SetupRouter(db.DB, userService, auctionService, bidService, nftService, nftContract, bcConfig.NFTContractAddress, bcConfig.AuctionDeployBlock, bcConfig.AuctionContractAddress, appConfig, appLogger)

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
		&model.IndexerState{},
	)
}
