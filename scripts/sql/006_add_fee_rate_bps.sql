-- 为 t_auction_index 增加实际使用的费率（基点），便于展示历史拍卖的手续费计算方式
-- 用法: mysql -u root -p nft_auction < scripts/sql/006_add_fee_rate_bps.sql

USE nft_auction;

ALTER TABLE t_auction_index
  ADD COLUMN fee_rate_bps INT UNSIGNED NULL DEFAULT NULL COMMENT '实际使用的费率(基点)，如250=2.5%' AFTER fee_is_eth;
