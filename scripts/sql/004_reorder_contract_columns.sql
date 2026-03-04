-- 调整合约相关字段顺序：将 auction_contract / contract_address 移到 id、auction_id 之后，并设为 NOT NULL
-- MySQL 8.0+
-- 用法: mysql -u root -p nft_auction < scripts/sql/004_reorder_contract_columns.sql
--
-- 执行前请确保旧合约数据已清空，且所有行已填好合约地址，否则 NOT NULL 会报错。

USE nft_auction;

-- t_auction_index: auction_contract 移到 auction_id 后面，NOT NULL
ALTER TABLE t_auction_index
  MODIFY COLUMN auction_contract VARCHAR(42) NOT NULL AFTER auction_id;

-- t_bid_index: auction_contract 移到 auction_id 后面，NOT NULL
ALTER TABLE t_bid_index
  MODIFY COLUMN auction_contract VARCHAR(42) NOT NULL AFTER auction_id;

-- t_indexer_state: contract_address 移到 id 后面，NOT NULL
ALTER TABLE t_indexer_state
  MODIFY COLUMN contract_address VARCHAR(42) NOT NULL AFTER id;
