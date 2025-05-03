package path

import "sync"

// Cache provides a thread-safe cache for parsed paths
type Cache struct {
	cache map[string]FactPath
	mu    sync.RWMutex
}

// NewCache creates a new path cache
func NewCache() *Cache {
	return &Cache{
		cache: make(map[string]FactPath),
	}
}

// Get retrieves a path from the cache or parses it if not cached
func (c *Cache) Get(path string) (FactPath, error) {
	// Try to get from cache first
	c.mu.RLock()
	cachedPath, exists := c.cache[path]
	c.mu.RUnlock()

	if exists {
		return cachedPath, nil
	}

	// Parse the path
	parsedPath, err := ParsePath(path)
	if err != nil {
		return FactPath{}, err
	}

	// Store in cache
	c.mu.Lock()
	c.cache[path] = parsedPath
	c.mu.Unlock()

	return parsedPath, nil
}

// Put stores a pre-parsed path in the cache
func (c *Cache) Put(pathString string, path FactPath) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[pathString] = path
}

// Clear empties the cache
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = make(map[string]FactPath)
}

// Size returns the number of entries in the cache
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.cache)
}
