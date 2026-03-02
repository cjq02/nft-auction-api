# 数据库脚本说明

## 执行顺序

1. `001_create_database.sql` - 创建数据库和表
2. `002_create_user.sql` - 创建应用用户（可选，按需修改密码）
3. `003_update_t_users_required_fields.sql` - **已有库时**：更新 t_users（email 选填、wallet_address 必填）

## 快速开始

```bash
# 1. 创建数据库和表（使用 root 或已有权限账户）
mysql -u root -p < scripts/sql/001_create_database.sql

# 2. 创建应用用户（可选）
mysql -u root -p < scripts/sql/002_create_user.sql
# 编辑 002_create_user.sql，将 your_password 改为实际密码

# 3. 若数据库是旧版建的（t_users 曾为 email 必填、wallet_address 选填），执行更新脚本
mysql -u root -p nft_auction < scripts/sql/003_update_t_users_required_fields.sql
```

## 表结构

| 表名 | 说明 |
|------|------|
| t_users | 用户表，关联钱包地址与 JWT |
| t_auction_index | 拍卖索引，与链上 IAuction.AuctionInfo 对齐 |
| t_bid_index | 出价索引 |
| t_nft_metadata | NFT 元数据缓存 |

## 环境变量

创建完成后，在 `.env` 中配置：

```env
DB_HOST=localhost
DB_PORT=3306
DB_USER=nft_auction_user
DB_PASSWORD=your_password
DB_NAME=nft_auction
```
