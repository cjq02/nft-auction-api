# NFT Auction API

基于 [nft-auction](https://github.com/example/nft-auction) 智能合约的 REST API 服务，提供链下索引、元数据查询等能力。

## 技术栈

- Go 1.21+
- Gin
- GORM + MySQL
- go-ethereum
- JWT

## 快速开始

### 1. 创建数据库

```bash
mysql -u root -p < scripts/sql/001_create_database.sql
mysql -u root -p < scripts/sql/002_create_user.sql
```

### 2. 配置环境变量

```bash
cp .env.example .env
# 编辑 .env 填入数据库等配置
```

### 3. 运行

```bash
go run cmd/app/main.go
```

## API 端点

### 认证

- `POST /api/auth/register` - 注册
- `POST /api/auth/login` - 登录
- `POST /api/auth/logout` - 退出（需认证）

### 用户

- `GET /api/users/me` - 当前用户（需认证）
- `GET /api/users/:address/auctions` - 某地址的拍卖列表

### 拍卖

- `GET /api/auctions` - 拍卖列表（?page=1&limit=10&status=Active）
- `GET /api/auctions/:id` - 拍卖详情
- `GET /api/auctions/:id/bids` - 出价列表
- `POST /api/auctions` - 创建拍卖（返回合约调用参数，需认证）

### NFT

- `GET /api/nfts/:contract/:tokenId` - NFT 元数据

## 架构

详见 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)
