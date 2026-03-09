package metadata

import (
	"sync"
	"time"
)

const (
	imageCacheMaxEntries = 300
	imageCacheTTL        = 24 * time.Hour
)

type cachedImage struct {
	data        []byte
	contentType string
	expiresAt   time.Time
}

// ImageCache 内存缓存图片，按 resolved URL 存，减轻 IPFS 网关压力
type ImageCache struct {
	mu    sync.RWMutex
	store map[string]*cachedImage
	keys  []string
}

func NewImageCache() *ImageCache {
	return &ImageCache{store: make(map[string]*cachedImage), keys: make([]string, 0, imageCacheMaxEntries)}
}

func (c *ImageCache) Get(url string) ([]byte, string, bool) {
	c.mu.RLock()
	entry, ok := c.store[url]
	c.mu.RUnlock()
	if !ok || entry == nil || time.Now().After(entry.expiresAt) {
		return nil, "", false
	}
	return entry.data, entry.contentType, true
}

func (c *ImageCache) Set(url string, data []byte, contentType string) {
	if len(data) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.store[url]; exists {
		c.store[url] = &cachedImage{data: data, contentType: contentType, expiresAt: time.Now().Add(imageCacheTTL)}
		return
	}
	if len(c.keys) >= imageCacheMaxEntries {
		old := c.keys[0]
		c.keys = c.keys[1:]
		delete(c.store, old)
	}
	c.keys = append(c.keys, url)
	c.store[url] = &cachedImage{data: data, contentType: contentType, expiresAt: time.Now().Add(imageCacheTTL)}
}
