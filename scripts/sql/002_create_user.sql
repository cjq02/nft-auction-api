-- 创建 nft_auction 数据库用户
-- 需要 root 或具有 CREATE USER、GRANT 权限的账户执行
-- 用法: mysql -u root -p < scripts/sql/002_create_user.sql
-- 注意: 生产环境请修改为强密码

CREATE USER IF NOT EXISTS 'nft_auction_user'@'localhost' IDENTIFIED BY '123456';
CREATE USER IF NOT EXISTS 'nft_auction_user'@'%' IDENTIFIED BY '123456';

GRANT ALL PRIVILEGES ON nft_auction.* TO 'nft_auction_user'@'localhost';
GRANT ALL PRIVILEGES ON nft_auction.* TO 'nft_auction_user'@'%';

FLUSH PRIVILEGES;