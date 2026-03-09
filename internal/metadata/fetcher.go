package metadata

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ParsedMetadata 解析后的 NFT 元数据（OpenSea 等常见字段）
type ParsedMetadata struct {
	Name        string
	Description string
	Image       string
	RawJSON     string
}

// Fetcher 从 URI 拉取并解析 NFT 元数据
type Fetcher struct {
	IPFSGateway string
	HTTPClient  *http.Client
}

// DefaultIPFSGateway 默认 IPFS 网关
const DefaultIPFSGateway = "https://gateway.pinata.cloud/ipfs/"

// NewFetcher 创建元数据拉取器，gateway 为空时使用 DefaultIPFSGateway
func NewFetcher(ipfsGateway string) *Fetcher {
	if ipfsGateway == "" {
		ipfsGateway = DefaultIPFSGateway
	}
	if !strings.HasSuffix(ipfsGateway, "/") {
		ipfsGateway += "/"
	}
	return &Fetcher{
		IPFSGateway: ipfsGateway,
		HTTPClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Fetch 根据 tokenURI 拉取并解析元数据
// tokenURI 可为 ipfs://Qm... 或 https://...
func (f *Fetcher) Fetch(tokenURI string) (*ParsedMetadata, error) {
	url := f.resolveURL(tokenURI)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	resp, err := f.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http status %d", resp.StatusCode)
	}

	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode json: %w", err)
	}

	rawBytes, _ := json.Marshal(raw)
	parsed := &ParsedMetadata{
		RawJSON: string(rawBytes),
	}

	if v, ok := raw["name"]; ok {
		if s, ok := v.(string); ok {
			parsed.Name = s
		}
	}
	if v, ok := raw["description"]; ok {
		if s, ok := v.(string); ok {
			parsed.Description = s
		}
	}
	if v, ok := raw["image"]; ok {
		if s, ok := v.(string); ok {
			parsed.Image = f.resolveURL(s)
		}
	}

	return parsed, nil
}

// ResolveURL 将 ipfs:// 转为网关 HTTP URL，供外部（如图片代理）使用
func (f *Fetcher) ResolveURL(uri string) string {
	return f.resolveURL(uri)
}

// resolveURL 将 ipfs:// 转为网关 URL
func (f *Fetcher) resolveURL(uri string) string {
	uri = strings.TrimSpace(uri)
	if strings.HasPrefix(uri, "ipfs://") {
		cid := strings.TrimPrefix(uri, "ipfs://")
		return f.IPFSGateway + cid
	}
	if strings.HasPrefix(uri, "ipfs/") {
		return "https://" + uri
	}
	return uri
}

// FetchImage 拉取图片 URL，返回 body 与 Content-Type（用于代理缓存）
func (f *Fetcher) FetchImage(resolvedURL string) ([]byte, string, error) {
	req, err := http.NewRequest(http.MethodGet, resolvedURL, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := f.HTTPClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("http status %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "image/png"
	}
	body, err := io.ReadAll(resp.Body)
	return body, ct, err
}
