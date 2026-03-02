package blockchain

import (
	"context"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// ERC721 / NFTMarketplace 的 ABI 片段（tokenURI + totalSupply + nextTokenId + ownerOf + NFTMinted 事件）
const erc721TokenURIABI = `[
  {"inputs":[{"internalType":"uint256","name":"tokenId","type":"uint256"}],"name":"tokenURI","outputs":[{"internalType":"string","name":"","type":"string"}],"stateMutability":"view","type":"function"},
  {"inputs":[],"name":"totalSupply","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},
  {"inputs":[],"name":"nextTokenId","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"internalType":"uint256","name":"tokenId","type":"uint256"}],"name":"ownerOf","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},
  {"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"to","type":"address"},{"indexed":true,"internalType":"uint256","name":"tokenId","type":"uint256"},{"indexed":false,"internalType":"string","name":"tokenURI","type":"string"}],"name":"NFTMinted","type":"event"}
]`

// nftMintedEventSig NFTMinted(address,uint256,string) 的 keccak256 签名 topic
var nftMintedEventSig = crypto.Keccak256Hash([]byte("NFTMinted(address,uint256,string)"))

// NFTContract 用于调用任意 ERC721 的 tokenURI
type NFTContract struct {
	client  *Client
	abi     abi.ABI
}

// NewNFTContract 创建 NFT 合约调用封装，client 可为 nil
func NewNFTContract(client *Client) (*NFTContract, error) {
	if client == nil || !client.IsAvailable() {
		return nil, nil
	}
	parsed, err := abi.JSON(strings.NewReader(erc721TokenURIABI))
	if err != nil {
		return nil, err
	}
	return &NFTContract{client: client, abi: parsed}, nil
}

// TokenURI 调用指定 ERC721 合约的 tokenURI(tokenId)，返回元数据 URI
func (c *NFTContract) TokenURI(ctx context.Context, contractAddress string, tokenID uint64) (string, error) {
	if c == nil || c.client == nil {
		return "", nil
	}
	addr := common.HexToAddress(contractAddress)
	data, err := c.abi.Pack("tokenURI", new(big.Int).SetUint64(tokenID))
	if err != nil {
		return "", err
	}
	result, err := c.client.CallContract(ctx, ethereum.CallMsg{
		To:   &addr,
		Data: data,
	}, nil)
	if err != nil {
		return "", err
	}
	var uri string
	if err := c.abi.UnpackIntoInterface(&uri, "tokenURI", result); err != nil {
		return "", err
	}
	return uri, nil
}

// TotalSupply 调用 NFT 合约的 totalSupply()，返回已铸造数量
func (c *NFTContract) TotalSupply(ctx context.Context, contractAddress string) (uint64, error) {
	if c == nil || c.client == nil || contractAddress == "" {
		return 0, nil
	}
	addr := common.HexToAddress(contractAddress)
	data, err := c.abi.Pack("totalSupply")
	if err != nil {
		return 0, err
	}
	result, err := c.client.CallContract(ctx, ethereum.CallMsg{
		To:   &addr,
		Data: data,
	}, nil)
	if err != nil {
		return 0, err
	}
	var n *big.Int
	if err := c.abi.UnpackIntoInterface(&n, "totalSupply", result); err != nil {
		return 0, err
	}
	if n == nil || !n.IsUint64() {
		return 0, nil
	}
	return n.Uint64(), nil
}

// NextTokenId 调用 NFTMarketplace 的 nextTokenId()，返回下一个将铸造的 token ID
func (c *NFTContract) NextTokenId(ctx context.Context, contractAddress string) (uint64, error) {
	if c == nil || c.client == nil || contractAddress == "" {
		return 0, nil
	}
	addr := common.HexToAddress(contractAddress)
	data, err := c.abi.Pack("nextTokenId")
	if err != nil {
		return 0, err
	}
	result, err := c.client.CallContract(ctx, ethereum.CallMsg{
		To:   &addr,
		Data: data,
	}, nil)
	if err != nil {
		return 0, err
	}
	var n *big.Int
	if err := c.abi.UnpackIntoInterface(&n, "nextTokenId", result); err != nil {
		return 0, err
	}
	if n == nil || !n.IsUint64() {
		return 0, nil
	}
	return n.Uint64(), nil
}

// OwnerOf 调用 ERC721 的 ownerOf(tokenId)，返回持有人地址
func (c *NFTContract) OwnerOf(ctx context.Context, contractAddress string, tokenID uint64) (common.Address, error) {
	if c == nil || c.client == nil || contractAddress == "" {
		return common.Address{}, nil
	}
	addr := common.HexToAddress(contractAddress)
	data, err := c.abi.Pack("ownerOf", new(big.Int).SetUint64(tokenID))
	if err != nil {
		return common.Address{}, err
	}
	result, err := c.client.CallContract(ctx, ethereum.CallMsg{
		To:   &addr,
		Data: data,
	}, nil)
	if err != nil {
		return common.Address{}, err
	}
	var owner common.Address
	if err := c.abi.UnpackIntoInterface(&owner, "ownerOf", result); err != nil {
		return common.Address{}, err
	}
	return owner, nil
}

// MintRecord 单条铸造记录
type MintRecord struct {
	TokenID uint64
	To      common.Address
}

// GetMintedToAddress 查询指定合约中铸造给 toAddress 的所有 NFT（通过 NFTMinted 事件日志）
func (c *NFTContract) GetMintedToAddress(ctx context.Context, contractAddress string, toAddress common.Address) ([]MintRecord, error) {
	if c == nil || c.client == nil || contractAddress == "" {
		return nil, nil
	}
	addr := common.HexToAddress(contractAddress)
	// topic[1]：to 地址，左填充到 32 字节
	toTopic := common.BytesToHash(toAddress.Bytes())
	logs, err := c.client.FilterLogs(ctx, ethereum.FilterQuery{
		Addresses: []common.Address{addr},
		Topics:    [][]common.Hash{{nftMintedEventSig}, {toTopic}},
	})
	if err != nil {
		return nil, err
	}
	records := make([]MintRecord, 0, len(logs))
	for _, log := range logs {
		if len(log.Topics) < 3 {
			continue
		}
		tokenID := new(big.Int).SetBytes(log.Topics[2].Bytes())
		if !tokenID.IsUint64() {
			continue
		}
		records = append(records, MintRecord{
			TokenID: tokenID.Uint64(),
			To:      toAddress,
		})
	}
	return records, nil
}
