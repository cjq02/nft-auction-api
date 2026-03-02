-- 更新 t_users：username、wallet_address 必填，email 选填
-- MySQL 8.0+
-- 用法: mysql -u root -p nft_auction < scripts/sql/003_update_t_users_required_fields.sql
--
-- 若表中已有 wallet_address 为 NULL 的行，请先手工为这些行填上有效地址或删除后再执行本脚本，
-- 否则第二条 ALTER 会报错。

USE nft_auction;

-- 1. email 改为选填（允许 NULL）
ALTER TABLE t_users
  MODIFY COLUMN email VARCHAR(100) NULL COMMENT '选填';

-- 2. wallet_address 改为必填（NOT NULL）
ALTER TABLE t_users
  MODIFY COLUMN wallet_address VARCHAR(42) NOT NULL COMMENT '必填，以太坊地址 0x + 40 位十六进制';
