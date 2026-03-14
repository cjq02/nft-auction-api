package service

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"strconv"
	"strings"

	"nft-auction-api/internal/blockchain"
	"nft-auction-api/internal/errors"
	"nft-auction-api/internal/model"

	"github.com/ethereum/go-ethereum/common"
	"gorm.io/gorm"
)

type AuctionService struct {
	db                  *gorm.DB
	auctionContract     *blockchain.AuctionContract
	defaultContractAddr string // 默认拍卖合约地址，用于查询时 contract 为空或历史数据
}

func NewAuctionService(db *gorm.DB, auctionContract *blockchain.AuctionContract, defaultContractAddr string) *AuctionService {
	return &AuctionService{
		db:                  db,
		auctionContract:     auctionContract,
		defaultContractAddr: defaultContractAddr,
	}
}

func (s *AuctionService) resolveContract(contract string) string {
	if contract != "" {
		return contract
	}
	return s.defaultContractAddr
}

// Counts 返回拍卖总数、进行中、已结束数量（用于数据概览）
func (s *AuctionService) Counts() (total, active, ended int64, err error) {
	if err = s.db.Model(&model.AuctionIndex{}).Count(&total).Error; err != nil {
		return 0, 0, 0, errors.NewDatabaseError(err)
	}
	if err = s.db.Model(&model.AuctionIndex{}).Where("status = ?", model.AuctionStatusActive).Count(&active).Error; err != nil {
		return 0, 0, 0, errors.NewDatabaseError(err)
	}
	if err = s.db.Model(&model.AuctionIndex{}).Where("status = ?", model.AuctionStatusEnded).Count(&ended).Error; err != nil {
		return 0, 0, 0, errors.NewDatabaseError(err)
	}
	return total, active, ended, nil
}

// TokenFeeTotal 按代币地址聚合的手续费总额
type TokenFeeTotal struct {
	Token string `json:"token"`
	Total string `json:"total"`
}

// TokenFeeTotalWithUsd 代币手续费合计及折合美元（用于手续费明细展示）
type TokenFeeTotalWithUsd struct {
	Token string `json:"token"`
	Total string `json:"total"`
	Usd   string `json:"usd"`
}

// FeeTotals 返回平台已收取的手续费总额（单位：原始最小单位字符串），
// 分别统计 ETH 总额与按 payment_token 维度聚合的非 ETH 手续费。
// 仅统计 status=Ended 且 fee_amount > 0 的拍卖。
func (s *AuctionService) FeeTotals(contractFilter string) (ethTotal string, tokenTotals []TokenFeeTotal, err error) {
	contract := s.resolveContract(contractFilter)

	type Row struct {
		Total string
	}

	var eth Row
	qEth := s.db.Table("t_auction_index").
		Select("COALESCE(SUM(CAST(fee_amount AS DECIMAL(65,0))), 0) AS total").
		Where("status = ? AND fee_amount IS NOT NULL AND fee_amount <> '' AND fee_is_eth = 1", model.AuctionStatusEnded)
	if contract != "" {
		qEth = qEth.Where("auction_contract = ?", contract)
	}
	if err = qEth.Scan(&eth).Error; err != nil {
		return "", nil, errors.NewDatabaseError(err)
	}

	type TokenRow struct {
		Token string
		Total string
	}
	var tokenRows []TokenRow
	qToken := s.db.Table("t_auction_index").
		Select("payment_token AS token, COALESCE(SUM(CAST(fee_amount AS DECIMAL(65,0))), 0) AS total").
		Where("status = ? AND fee_amount IS NOT NULL AND fee_amount <> '' AND (fee_is_eth = 0 OR fee_is_eth IS NULL)", model.AuctionStatusEnded)
	if contract != "" {
		qToken = qToken.Where("auction_contract = ?", contract)
	}
	if err = qToken.Group("payment_token").Scan(&tokenRows).Error; err != nil {
		return "", nil, errors.NewDatabaseError(err)
	}

	tokenTotals = make([]TokenFeeTotal, 0, len(tokenRows))
	for _, r := range tokenRows {
		if r.Token == "" || r.Total == "" || r.Total == "0" {
			continue
		}
		tokenTotals = append(tokenTotals, TokenFeeTotal{
			Token: r.Token,
			Total: r.Total,
		})
	}

	// 规范化为整数字符串，避免 MySQL 返回 "0.000000" 等导致前端解析异常；
	// 若驱动返回科学计数法（如 8e+14），big.Int.SetString(10) 会失败，需用 float 解析后再转整数
	intPart := eth.Total
	if i := strings.Index(eth.Total, "."); i >= 0 {
		intPart = eth.Total[:i]
	}
	ethTotalOut := "0"
	if n, ok := new(big.Int).SetString(intPart, 10); ok && n.Sign() > 0 {
		ethTotalOut = n.String()
	} else if strings.ContainsAny(intPart, "eE") {
		if f, err := strconv.ParseFloat(intPart, 64); err == nil && f > 0 {
			// 仅当可精确还原为整数时使用（避免浮点误差）
			if f <= 9e18 && f == float64(int64(f)) {
				ethTotalOut = big.NewInt(int64(f)).String()
			}
		}
	}
	return ethTotalOut, tokenTotals, nil
}

// formatUsd8 将 8 位小数的美元整数格式化为 "12345.67"
func formatUsd8(usd8 *big.Int) string {
	if usd8 == nil || usd8.Sign() < 0 {
		return "0.00"
	}
	oneE8 := big.NewInt(1e8)
	intPart := new(big.Int).Div(usd8, oneE8)
	rem := new(big.Int).Rem(usd8, oneE8)
	centPart := new(big.Int).Mul(rem, big.NewInt(100))
	centPart.Div(centPart, oneE8)
	if centPart.Cmp(big.NewInt(100)) >= 0 {
		centPart = big.NewInt(99)
	}
	return fmt.Sprintf("%s.%02d", intPart.String(), centPart.Int64())
}

// FeeTotalsWithUsd 返回手续费总额及每项折合美元（ETH 部分 + 各代币部分），用于手续费明细展示。
func (s *AuctionService) FeeTotalsWithUsd(ctx context.Context, contractFilter string) (
	ethTotal string,
	ethUsd string,
	tokenTotalsWithUsd []TokenFeeTotalWithUsd,
	err error,
) {
	ethTotal, tokenTotals, err := s.FeeTotals(contractFilter)
	if err != nil {
		return "", "", nil, err
	}
	ethUsd = "0.00"
	tokenTotalsWithUsd = make([]TokenFeeTotalWithUsd, 0, len(tokenTotals))
	if s.auctionContract == nil {
		for _, t := range tokenTotals {
			tokenTotalsWithUsd = append(tokenTotalsWithUsd, TokenFeeTotalWithUsd{Token: t.Token, Total: t.Total, Usd: "0.00"})
		}
		return ethTotal, ethUsd, tokenTotalsWithUsd, nil
	}
	ethPrice8, err := s.auctionContract.GetEthPrice8(ctx)
	if err != nil {
		ethPrice8 = big.NewInt(0)
	}
	oneE18 := big.NewInt(1e18)
	if ethTotal != "" && ethTotal != "0" {
		ethWei, ok := new(big.Int).SetString(ethTotal, 10)
		if ok && ethWei.Sign() > 0 && ethPrice8.Sign() > 0 {
			ethUsd8 := new(big.Int).Mul(ethWei, ethPrice8)
			ethUsd8.Div(ethUsd8, oneE18)
			ethUsd = formatUsd8(ethUsd8)
		}
	}
	for _, t := range tokenTotals {
		usd := "0.00"
		if t.Total != "" && t.Total != "0" {
			price8, _ := s.auctionContract.GetTokenPrice8(ctx, t.Token)
			if price8 != nil && price8.Sign() > 0 {
				amt, ok := new(big.Int).SetString(t.Total, 10)
				if ok && amt.Sign() > 0 {
					tokenUsd8 := new(big.Int).Mul(amt, price8)
					tokenUsd8.Div(tokenUsd8, oneE18)
					usd = formatUsd8(tokenUsd8)
				}
			}
		}
		tokenTotalsWithUsd = append(tokenTotalsWithUsd, TokenFeeTotalWithUsd{Token: t.Token, Total: t.Total, Usd: usd})
	}
	return ethTotal, ethUsd, tokenTotalsWithUsd, nil
}

// ComputeFeeTotalUsd 将 ETH + 各代币手续费折成美元合计（使用链上当前价格）。无法读价格时返回 "0.00"。
func (s *AuctionService) ComputeFeeTotalUsd(ctx context.Context, contractFilter string) (string, error) {
	ethTotal, tokenTotals, err := s.FeeTotals(contractFilter)
	if err != nil {
		return "", err
	}
	if s.auctionContract == nil {
		return "0.00", nil
	}

	ethPrice8, err := s.auctionContract.GetEthPrice8(ctx)
	if err != nil {
		ethPrice8 = big.NewInt(0)
	}

	oneE18 := big.NewInt(1e18)
	oneE8 := big.NewInt(1e8)
	totalUsd8 := new(big.Int)

	if ethTotal != "" && ethTotal != "0" {
		ethWei, ok := new(big.Int).SetString(ethTotal, 10)
		if ok && ethWei.Sign() > 0 {
			// ethUsd8 = ethWei * ethPrice8 / 1e18（price8 为 8 位小数，结果即 USD 的 8 位小数）
			ethUsd8 := new(big.Int).Mul(ethWei, ethPrice8)
			ethUsd8.Div(ethUsd8, oneE18)
			totalUsd8.Add(totalUsd8, ethUsd8)
		}
	}

	for _, t := range tokenTotals {
		if t.Total == "" || t.Total == "0" {
			continue
		}
		price8, err := s.auctionContract.GetTokenPrice8(ctx, t.Token)
		if err != nil || price8 == nil || price8.Sign() <= 0 {
			continue
		}
		amt, ok := new(big.Int).SetString(t.Total, 10)
		if !ok || amt.Sign() <= 0 {
			continue
		}
		// tokenUsd8 = amt * price8 / 1e18
		tokenUsd8 := new(big.Int).Mul(amt, price8)
		tokenUsd8.Div(tokenUsd8, oneE18)
		totalUsd8.Add(totalUsd8, tokenUsd8)
	}

	// 格式化为 "12345.67"（totalUsd8 为 8 位小数）
	intPart := new(big.Int).Div(totalUsd8, oneE8)
	rem := new(big.Int).Rem(totalUsd8, oneE8)
	centPart := new(big.Int).Mul(rem, big.NewInt(100))
	centPart.Div(centPart, oneE8)
	if centPart.Cmp(big.NewInt(100)) >= 0 {
		centPart = big.NewInt(99)
	}
	return fmt.Sprintf("%s.%02d", intPart.String(), centPart.Int64()), nil
}

// ComputeFeeTotalEthEquivalent 将平台手续费美元总额按当前链上 ETH 价格换算成 ETH 数量（用于展示「折合 x.xx ETH」）。
func (s *AuctionService) ComputeFeeTotalEthEquivalent(ctx context.Context, feeTotalUsd string) (string, error) {
	if feeTotalUsd == "" || feeTotalUsd == "0" || feeTotalUsd == "0.00" {
		return "0", nil
	}
	if s.auctionContract == nil {
		return "0", nil
	}
	ethPrice8, err := s.auctionContract.GetEthPrice8(ctx)
	if err != nil || ethPrice8 == nil || ethPrice8.Sign() <= 0 {
		return "0", nil
	}
	usd, err := strconv.ParseFloat(feeTotalUsd, 64)
	if err != nil || usd <= 0 {
		return "0", nil
	}
	// 用 big.Float 避免 ethPrice8 超过 int64 时溢出；ethEquivalent = usd / (ethPrice8/1e8)
	usdF := big.NewFloat(usd)
	price8F := new(big.Float).SetInt(ethPrice8)
	oneE8F := big.NewFloat(1e8)
	priceUsdF := new(big.Float).Quo(price8F, oneE8F)
	if priceUsdF.Sign() <= 0 {
		return "0", nil
	}
	ethEqF := new(big.Float).Quo(usdF, priceUsdF)
	ethEq, _ := ethEqF.Float64()
	return fmt.Sprintf("%.6f", ethEq), nil
}

// StatsForActive 返回平台统计数据：进行中拍卖数量、有出价的拍卖数量、最高价合计（wei）
func (s *AuctionService) StatsForActive(contractFilter string) (totalAuctions, bidCount int64, totalHighestBidWei *big.Int, err error) {
	contract := s.resolveContract(contractFilter)

	query := s.db.Model(&model.AuctionIndex{}).Where("status = ?", model.AuctionStatusActive)
	if contract != "" {
		query = query.Where("auction_contract = ?", contract)
	}

	if err = query.Count(&totalAuctions).Error; err != nil {
		return 0, 0, nil, errors.NewDatabaseError(err)
	}

	// 每个进行中拍卖的最高出价（仅统计 ETH 拍卖，确保单位为 wei→ETH；MySQL 5.7 兼容：用 GROUP BY + MAX，不用窗口函数）
	type HighestBidRow struct {
		AuctionID uint64
		Amount    string
	}

	var rows []HighestBidRow
	q := s.db.Table("t_bid_index b").
		Select("b.auction_id, MAX(b.amount) AS amount").
		Joins("INNER JOIN t_auction_index a ON a.auction_id = b.auction_id AND a.auction_contract = b.auction_contract AND a.status = ?", model.AuctionStatusActive).
		Where("b.is_eth = ?", true). // 只统计 ETH 出价，避免将 ERC20 金额当作 ETH
		Group("b.auction_id")
	if contract != "" {
		q = q.Where("b.auction_contract = ?", contract)
	}
	if err = q.Scan(&rows).Error; err != nil {
		return totalAuctions, 0, nil, errors.NewDatabaseError(err)
	}

	totalHighestBidWei = big.NewInt(0)
	for _, r := range rows {
		if r.Amount == "" {
			continue
		}
		amt, ok := new(big.Int).SetString(r.Amount, 10)
		if !ok {
			continue
		}
		if amt.Sign() > 0 {
			bidCount++
			totalHighestBidWei.Add(totalHighestBidWei, amt)
		}
	}

	return totalAuctions, bidCount, totalHighestBidWei, nil
}

func (s *AuctionService) List(page, limit int, status, contractFilter string) ([]model.AuctionIndex, int64, error) {
	var items []model.AuctionIndex
	var total int64

	query := s.db.Model(&model.AuctionIndex{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	contract := s.resolveContract(contractFilter)
	if contract != "" {
		query = query.Where("auction_contract = ?", contract)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, errors.NewDatabaseError(err)
	}

	offset := (page - 1) * limit
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	if err := query.Order("created_at DESC").Limit(limit).Offset(offset).Find(&items).Error; err != nil {
		return nil, 0, errors.NewDatabaseError(err)
	}

	return items, total, nil
}

func (s *AuctionService) GetByAuctionID(auctionID uint64, contract string) (*model.AuctionIndex, error) {
	contract = s.resolveContract(contract)
	var item model.AuctionIndex
	query := s.db.Where("auction_id = ?", auctionID)
	if contract != "" {
		query = query.Where("auction_contract = ?", contract)
	}
	if err := query.First(&item).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			if s.auctionContract != nil && contract != "" {
				chainInfo, err := s.auctionContract.GetAuction(context.Background(), auctionID)
				if err != nil {
					return nil, errors.NewBlockchainError("获取链上拍卖失败", err)
				}
				if chainInfo != nil && chainInfo.Seller != (common.Address{}) {
					return s.chainInfoToIndex(auctionID, chainInfo, contract), nil
				}
			}
			return nil, errors.NewNotFoundError("拍卖不存在")
		}
		return nil, errors.NewDatabaseError(err)
	}
	return &item, nil
}

func (s *AuctionService) chainInfoToIndex(auctionID uint64, info *blockchain.AuctionInfo, contractAddress string) *model.AuctionIndex {
	var paymentToken *string
	if info.PaymentToken != (common.Address{}) && info.PaymentToken.Hex() != "0x0000000000000000000000000000000000000000" {
		addr := info.PaymentToken.Hex()
		paymentToken = &addr
	}

	status := model.AuctionStatusActive
	switch info.Status {
	case 1:
		status = model.AuctionStatusEnded
	case 2:
		status = model.AuctionStatusCancelled
	}

	return &model.AuctionIndex{
		AuctionContract: contractAddress,
		AuctionID:       auctionID,
		Seller:          info.Seller.Hex(),
		NFTContract:     info.NFTContract.Hex(),
		TokenID:         info.TokenID.Uint64(),
		StartTime:       info.StartTime.Int64(),
		EndTime:         info.EndTime.Int64(),
		MinBid:          info.MinBid.String(),
		PaymentToken:    paymentToken,
		Status:          status,
	}
}

// CountActiveBySeller 返回某卖家当前进行中的拍卖数量（status=Active）
func (s *AuctionService) CountActiveBySeller(seller string, contractFilter string) (int64, error) {
	contract := s.resolveContract(contractFilter)
	query := s.db.Model(&model.AuctionIndex{}).Where("seller = ? AND status = ?", seller, model.AuctionStatusActive)
	if contract != "" {
		query = query.Where("auction_contract = ?", contract)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return 0, errors.NewDatabaseError(err)
	}
	return count, nil
}

func (s *AuctionService) ListBySeller(seller string, page, limit int, contractFilter string) ([]model.AuctionIndex, int64, error) {
	var items []model.AuctionIndex
	var total int64

	query := s.db.Model(&model.AuctionIndex{}).Where("seller = ?", seller)
	contract := s.resolveContract(contractFilter)
	if contract != "" {
		query = query.Where("auction_contract = ?", contract)
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, errors.NewDatabaseError(err)
	}

	offset := (page - 1) * limit
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 10
	}

	if err := query.Order("created_at DESC").Limit(limit).Offset(offset).Find(&items).Error; err != nil {
		return nil, 0, errors.NewDatabaseError(err)
	}

	return items, total, nil
}

func (s *AuctionService) CreateIndex(item *model.AuctionIndex) error {
	return s.db.Create(item).Error
}

func (s *AuctionService) UpdateStatus(contractAddress string, auctionID uint64, status model.AuctionStatus) error {
	contractAddress = s.resolveContract(contractAddress)
	return s.db.Model(&model.AuctionIndex{}).Where("auction_id = ? AND auction_contract = ?", auctionID, contractAddress).Update("status", status).Error
}

// UpdateFeeCollected 更新某场拍卖的手续费（由 FeeCollected 事件触发）
func (s *AuctionService) UpdateFeeCollected(contractAddress string, auctionID uint64, feeAmount string, feeIsETH bool, feeRateBps uint64) error {
	contractAddress = s.resolveContract(contractAddress)
	updates := map[string]interface{}{"fee_amount": feeAmount, "fee_is_eth": feeIsETH}
	if feeRateBps > 0 {
		updates["fee_rate_bps"] = feeRateBps
	}
	return s.db.Model(&model.AuctionIndex{}).Where("auction_id = ? AND auction_contract = ?", auctionID, contractAddress).Updates(updates).Error
}

// BackfillResult 补录结果
type BackfillResult struct {
	FoundOnChain int // 链上扫描到的 AuctionCreated 数量
	Added        int // 本次新写入 DB 的数量
}

// BackfillFromChain 扫描链上所有历史 AuctionCreated 事件，将 DB 中缺失的拍卖补录进来。
// contractAddress 为当前监听的合约地址；startBlock 为 0 时从创世块扫。
func (s *AuctionService) BackfillFromChain(ctx context.Context, contractAddress string, startBlock uint64) (*BackfillResult, error) {
	contractAddress = s.resolveContract(contractAddress)
	log.Printf("[auction_sync] BackfillFromChain start contract=%s startBlock=%d", contractAddress, startBlock)
	if s.auctionContract == nil {
		log.Printf("[auction_sync] BackfillFromChain error: auction contract not configured")
		return nil, errors.NewBlockchainError("拍卖合约未配置", nil)
	}

	ids, err := s.auctionContract.ScanAuctionIDs(ctx, startBlock)
	if err != nil {
		log.Printf("[auction_sync] BackfillFromChain scan_failed startBlock=%d err=%v", startBlock, err)
		return nil, errors.NewBlockchainError("扫描链上事件失败: "+err.Error(), err)
	}
	log.Printf("[auction_sync] BackfillFromChain scan_done startBlock=%d foundOnChain=%d", startBlock, len(ids))

	result := &BackfillResult{FoundOnChain: len(ids)}
	for _, auctionID := range ids {
		var existing model.AuctionIndex
		if dbErr := s.db.Where("auction_contract = ? AND auction_id = ?", contractAddress, auctionID).First(&existing).Error; dbErr == nil {
			continue // 已存在，跳过
		}

		chainInfo, chainErr := s.auctionContract.GetAuction(ctx, auctionID)
		if chainErr != nil || chainInfo == nil {
			log.Printf("[auction_sync] BackfillFromChain skip_auction auctionId=%d get_auction_err=%v", auctionID, chainErr)
			continue
		}

		item := s.chainInfoToIndex(auctionID, chainInfo, contractAddress)
		if createErr := s.db.Create(item).Error; createErr == nil {
			result.Added++
			log.Printf("[auction_sync] BackfillFromChain added auctionId=%d", auctionID)
		} else {
			log.Printf("[auction_sync] BackfillFromChain db_create_failed auctionId=%d err=%v", auctionID, createErr)
		}
	}
	log.Printf("[auction_sync] BackfillFromChain done startBlock=%d foundOnChain=%d added=%d", startBlock, result.FoundOnChain, result.Added)
	return result, nil
}

// IndexFromAuctionID 从链上读取 auctionId 对应的拍卖信息并写入数据库（幂等）。
// contractAddress 为当前监听的拍卖合约地址。
func (s *AuctionService) IndexFromAuctionID(ctx context.Context, contractAddress string, auctionID uint64) (*model.AuctionIndex, error) {
	contractAddress = s.resolveContract(contractAddress)
	// 幂等：若已存在则直接返回
	var existing model.AuctionIndex
	if dbErr := s.db.Where("auction_contract = ? AND auction_id = ?", contractAddress, auctionID).First(&existing).Error; dbErr == nil {
		log.Printf("[auction_sync] IndexFromAuctionID already_exists auctionId=%d", auctionID)
		return &existing, nil
	}

	if s.auctionContract == nil {
		return nil, errors.NewBlockchainError("拍卖合约未配置", nil)
	}

	chainInfo, err := s.auctionContract.GetAuction(ctx, auctionID)
	if err != nil || chainInfo == nil {
		log.Printf("[auction_sync] IndexFromAuctionID get_auction_failed auctionId=%d err=%v", auctionID, err)
		return nil, errors.NewBlockchainError("从链上获取拍卖信息失败", err)
	}
	log.Printf("[auction_sync] IndexFromAuctionID chain_info_ok auctionId=%d seller=%s", auctionID, chainInfo.Seller.Hex())

	item := s.chainInfoToIndex(auctionID, chainInfo, contractAddress)
	if createErr := s.db.Create(item).Error; createErr != nil {
		log.Printf("[auction_sync] IndexFromAuctionID db_create_failed auctionId=%d err=%v", auctionID, createErr)
		return nil, errors.NewDatabaseError(createErr)
	}
	log.Printf("[auction_sync] IndexFromAuctionID db_created auctionId=%d", auctionID)
	return item, nil
}

// IndexFromTxHash 从 txHash 解析 AuctionCreated 事件，向链上查询完整数据后写入数据库（幂等）
// 使用默认合约地址作为 auction_contract。
func (s *AuctionService) IndexFromTxHash(ctx context.Context, txHash string) (*model.AuctionIndex, error) {
	log.Printf("[auction_sync] IndexFromTxHash start txHash=%s", txHash)
	if s.auctionContract == nil {
		return nil, errors.NewBlockchainError("拍卖合约未配置", nil)
	}

	auctionID, err := s.auctionContract.ParseAuctionCreatedFromReceipt(ctx, txHash)
	if err != nil {
		log.Printf("[auction_sync] IndexFromTxHash parse_failed txHash=%s err=%v", txHash, err)
		return nil, errors.NewBlockchainError("解析交易事件失败", err)
	}
	log.Printf("[auction_sync] IndexFromTxHash parsed auctionId=%d", auctionID)
	return s.IndexFromAuctionID(ctx, s.defaultContractAddr, auctionID)
}

// GetMinBidEth 根据链上价格将 minBid（USD，18 位小数字符串）换算为 ETH 展示字符串。
// 若链未配置或调用失败则返回空字符串，调用方可不展示 ETH 或做降级。
func (s *AuctionService) GetMinBidEth(ctx context.Context, auctionContractAddr, minBidUSD string) (string, error) {
	if s.auctionContract == nil || auctionContractAddr == "" || minBidUSD == "" {
		return "", nil
	}
	minBid := new(big.Int)
	if _, ok := minBid.SetString(minBidUSD, 10); !ok {
		return "", nil
	}
	if minBid.Sign() <= 0 {
		return "", nil
	}
	return s.auctionContract.GetMinBidEth(ctx, auctionContractAddr, minBid)
}

