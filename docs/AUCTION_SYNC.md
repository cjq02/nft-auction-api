# 拍卖数据同步问题

## 现象

前端 MetaMask 调用合约 `createAuction` 交易成功，但：

```
GET /api/auctions?status=Active
→ { "items": null, "total": 0 }
```

首页「进行中的拍卖」始终为空。

---

## 根本原因

**`GET /api/auctions` 只查数据库表 `t_auction_index`，不读链上合约。**

当前链上与数据库的关系：

```
前端 wagmi → MetaMask → 合约 createAuction ✅ 链上有记录
                                ↓
                   数据库 t_auction_index ← 无任何写入机制 ❌
                                ↓
             GET /api/auctions → total: 0, items: null ❌
```

`POST /api/auctions` 目前只返回参数说明，不写数据库：

```go
// internal/handler/auction_handler.go
func (h *AuctionHandler) Create(c *gin.Context) {
    ...
    response.Success(c, gin.H{
        "message": "请使用钱包调用合约 createAuction 完成创建，以下为调用参数",
        "params":  params,
    })
}
```

---

## 解决方案对比

| 方案                              | 原理                                                                 | 优点             | 缺点                                    |
| --------------------------------- | -------------------------------------------------------------------- | ---------------- | --------------------------------------- |
| **A. 前端链上成功后回调 API**     | 前端交易确认后，解析事件得到 auctionId，调 `POST /api/auctions` 写库 | 改动小，快速可用 | 前端可能绕过/漏报；依赖前端在线         |
| **B. 后端监听链上事件（推荐）**   | 后端轮询/订阅 `AuctionCreated` 事件，自动写库                        | 权威，不依赖前端 | 需实现 event watcher，开发量中等        |
| **C. GET /api/auctions 实时读链** | List 接口遍历链上 auctionId，实时拉取                                | 无需写库         | 性能差，链上 N 个拍卖就要 N 次 RPC 调用 |

**建议：先做方案 A 快速打通，再用方案 B 替代。**

---

## 方案 A：前端交易确认后回调 API

### 1. 后端：修改 `CreateAuctionRequest` 和 `Create` handler

**model/auction_index.go** 补充请求字段：

```go
type CreateAuctionRequest struct {
    AuctionID    uint64  `json:"auctionId" binding:"required"`
    Seller       string  `json:"seller" binding:"required"`
    NFTContract  string  `json:"nftContract" binding:"required"`
    TokenID      uint64  `json:"tokenId" binding:"required"`
    StartTime    int64   `json:"startTime" binding:"required"`
    EndTime      int64   `json:"endTime" binding:"required"`
    MinBidUSD    string  `json:"minBidUSD" binding:"required"`
    PaymentToken *string `json:"paymentToken"`
}
```

**handler/auction_handler.go** 的 `Create` 改为写入数据库：

```go
func (h *AuctionHandler) Create(c *gin.Context) {
    var req model.CreateAuctionRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        response.HandleError(c, h.logger, errors.NewValidationError(err.Error()))
        return
    }
    item := &model.AuctionIndex{
        AuctionID:    req.AuctionID,
        Seller:       req.Seller,
        NFTContract:  req.NFTContract,
        TokenID:      req.TokenID,
        StartTime:    req.StartTime,
        EndTime:      req.EndTime,
        MinBid:       req.MinBidUSD,
        PaymentToken: req.PaymentToken,
        Status:       model.AuctionStatusActive,
    }
    if err := h.auctionService.CreateIndex(item); err != nil {
        response.HandleError(c, h.logger, err)
        return
    }
    response.Success(c, item)
}
```

### 2. 前端：解析事件，交易成功后 POST

合约 `AuctionCreated` 事件 ABI（需与实际合约对齐）：

```typescript
const auctionCreatedEvent = {
  type: "event",
  name: "AuctionCreated",
  inputs: [
    { name: "auctionId", type: "uint256", indexed: true },
    { name: "seller", type: "address", indexed: true },
    { name: "nftContract", type: "address", indexed: false },
    { name: "tokenId", type: "uint256", indexed: false },
    { name: "endTime", type: "uint256", indexed: false },
    { name: "minBid", type: "uint256", indexed: false },
    { name: "paymentToken", type: "address", indexed: false },
  ],
} as const;
```

`useWaitForTransactionReceipt` 返回 receipt 后，用 viem `parseEventLogs` 解析出 `auctionId`，再调 `POST /api/auctions`。

---

## 方案 B：后端监听 AuctionCreated 事件（推荐长期方案）

### 实现思路

1. 在 `internal/blockchain/` 下新增 `event_watcher.go`；
2. 启动一个 goroutine，定期（如每 12s）调用 `eth_getLogs`，过滤 `AuctionCreated` 事件；
3. 对每条新事件，调用 `auctionService.CreateIndex` 写入数据库，记录已处理的最新 block number；
4. 重启时从已记录的 block number 继续，避免漏单或重复。

### AuctionCreated 事件 topic

```
keccak256("AuctionCreated(uint256,address,address,uint256,uint256,uint256,uint256,address)")
```

（具体签名和参数以合约实际定义为准）

### 需要的新文件/改动

| 文件                                   | 改动                             |
| -------------------------------------- | -------------------------------- |
| `internal/blockchain/event_watcher.go` | 新增，实现 FilterLogs + 事件解析 |
| `internal/service/auction_service.go`  | 已有 `CreateIndex`，直接复用     |
| `internal/model/sync_state.go`         | 新增，记录已同步的 block number  |
| `cmd/app/main.go`                      | 启动 watcher goroutine           |

---

## 临时快速验证

在后端方案未完成前，可以直接手动插入一条数据库记录来验证前端展示是否正常：

```sql
INSERT INTO t_auction_index
  (auction_id, seller, nft_contract, token_id, start_time, end_time, min_bid, status, created_at, updated_at)
VALUES
  (1, '0x你的钱包地址', '0xNFT合约地址', 1,
   UNIX_TIMESTAMP(NOW()), UNIX_TIMESTAMP(NOW()) + 604800,
   '10000000000000000', 'Active', NOW(), NOW());
```

若首页能展示，说明前端逻辑正确，只缺后端写入。
