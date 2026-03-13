-- 为 t_auction_index 增加手续费字段，用于展示每笔已结束拍卖收取的手续费
-- 用法: mysql -u root -p nft_auction < scripts/sql/005_add_auction_fee_columns.sql

USE nft_auction;

ALTER TABLE t_auction_index
  ADD COLUMN fee_amount VARCHAR(78) NULL DEFAULT NULL COMMENT '该场拍卖收取的手续费(wei/最小单位)，仅 Ended 且有成交时有值' AFTER status,
  ADD COLUMN fee_is_eth TINYINT(1) NULL DEFAULT NULL COMMENT '手续费是否为 ETH' AFTER fee_amount;
