-- NFT Auction API 数据库初始化脚本
-- MySQL 8.0+
-- 用法: mysql -u root -p < scripts/sql/001_create_database.sql

-- 创建数据库
CREATE DATABASE IF NOT EXISTS nft_auction
  DEFAULT CHARACTER SET utf8mb4
  DEFAULT COLLATE utf8mb4_unicode_ci;

USE nft_auction;

-- ============================================
-- 用户表
-- ============================================
CREATE TABLE IF NOT EXISTS t_users (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  username VARCHAR(100) NOT NULL,
  password_hash VARCHAR(255) NOT NULL,
  email VARCHAR(100) NOT NULL,
  wallet_address VARCHAR(42) NULL COMMENT '以太坊地址 0x + 40 位十六进制',
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_t_users_username (username),
  UNIQUE KEY uk_t_users_email (email),
  UNIQUE KEY uk_t_users_wallet (wallet_address),
  KEY idx_t_users_wallet (wallet_address)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='用户表';

-- ============================================
-- 拍卖索引表
-- ============================================
CREATE TABLE IF NOT EXISTS t_auction_index (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  auction_id BIGINT UNSIGNED NOT NULL COMMENT '链上 auctionId',
  seller VARCHAR(42) NOT NULL COMMENT '卖家地址',
  nft_contract VARCHAR(42) NOT NULL COMMENT 'NFT 合约地址',
  token_id BIGINT UNSIGNED NOT NULL COMMENT 'NFT tokenId',
  start_time BIGINT NOT NULL COMMENT '开始时间戳',
  end_time BIGINT NOT NULL COMMENT '结束时间戳',
  min_bid VARCHAR(78) NOT NULL COMMENT '最低出价 USD，18 位小数',
  payment_token VARCHAR(42) NULL COMMENT '支付代币地址，NULL 表示 ETH',
  status VARCHAR(20) NOT NULL DEFAULT 'Active' COMMENT 'Active/Ended/Cancelled',
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_t_auction_index_auction_id (auction_id),
  KEY idx_t_auction_index_seller (seller),
  KEY idx_t_auction_index_status (status),
  KEY idx_t_auction_index_end_time (end_time),
  KEY idx_t_auction_index_nft (nft_contract, token_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='拍卖索引表';

-- ============================================
-- 出价索引表
-- ============================================
CREATE TABLE IF NOT EXISTS t_bid_index (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  auction_id BIGINT UNSIGNED NOT NULL COMMENT '拍卖 ID',
  bidder VARCHAR(42) NOT NULL COMMENT '出价者地址',
  amount VARCHAR(78) NOT NULL COMMENT '出价金额',
  bid_timestamp BIGINT NOT NULL COMMENT '出价时间戳',
  is_eth TINYINT(1) NOT NULL DEFAULT 1 COMMENT '1=ETH, 0=ERC20',
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_t_bid_index_auction (auction_id),
  KEY idx_t_bid_index_bidder (bidder)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='出价索引表';

-- ============================================
-- NFT 元数据缓存表
-- ============================================
CREATE TABLE IF NOT EXISTS t_nft_metadata (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  nft_contract VARCHAR(42) NOT NULL,
  token_id BIGINT UNSIGNED NOT NULL,
  token_uri VARCHAR(512) NULL,
  name VARCHAR(255) NULL,
  description TEXT NULL,
  image VARCHAR(512) NULL,
  raw_json TEXT NULL COMMENT '原始 JSON 元数据',
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_t_nft_metadata_contract_token (nft_contract, token_id),
  KEY idx_t_nft_metadata_contract (nft_contract)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='NFT 元数据缓存';
