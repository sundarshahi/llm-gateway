package llmgateway

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
)

// Cache stores raw CLI stdout keyed by a content hash of the request. A hit is
// always a previously-validated reply (the gateway only writes after a clean
// parse), so an identical job returns a byte-identical result and skips the
// subprocess. Implementations must be safe for concurrent use; Put is
// best-effort and must never block the request meaningfully.
type Cache interface {
	Get(key string) (string, bool)
	Put(key, value string)
}

// cacheKey hashes the exact inputs that determine a provider's reply. The tuple
// is JSON-encoded (not concatenated) so a delimiter inside one field can't be
// crafted to collide with a different field split.
func cacheKey(model string, r SpawnRequest) string {
	payload, _ := json.Marshal([]string{
		model, r.ModelName, r.System, r.Prompt, toolsStr(r.Tools), r.Schema, r.Thinking,
	})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// FileCache is a content-addressed on-disk cache (one file per key). Atomic
// writes via temp+rename. Safe for concurrent use.
type FileCache struct {
	Dir string
	seq atomic.Uint64
	log func(format string, a ...any)
}

// NewFileCache creates the cache directory and returns a FileCache.
func NewFileCache(dir string) (*FileCache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &FileCache{Dir: dir}, nil
}

func (c *FileCache) path(key string) string { return filepath.Join(c.Dir, key+".txt") }

func (c *FileCache) Get(key string) (string, bool) {
	b, err := os.ReadFile(c.path(key))
	if err != nil {
		return "", false
	}
	return string(b), true
}

func (c *FileCache) Put(key, value string) {
	dst := c.path(key)
	tmp := dst + "." + strconv.FormatUint(c.seq.Add(1), 10) + ".tmp"
	if err := os.WriteFile(tmp, []byte(value), 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
	}
}

// MemoryCache is an in-process cache. Useful for tests and single-instance
// deployments that don't need on-disk persistence.
type MemoryCache struct {
	mu sync.RWMutex
	m  map[string]string
}

func NewMemoryCache() *MemoryCache { return &MemoryCache{m: map[string]string{}} }

func (c *MemoryCache) Get(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.m[key]
	return v, ok
}

func (c *MemoryCache) Put(key, value string) {
	c.mu.Lock()
	c.m[key] = value
	c.mu.Unlock()
}
