package path

import (
	"sync"

	"github.com/effectus/effectus-go"
)

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

// CachedPathResolver is a PathResolver implementation that caches results
type CachedPathResolver struct {
	delegate  PathResolver
	cache     sync.Map // Maps path strings to resolved values
	typeCache sync.Map // Maps path strings to type information
}

// NewCachedPathResolver creates a new cached path resolver
func NewCachedPathResolver(delegate PathResolver) *CachedPathResolver {
	return &CachedPathResolver{
		delegate: delegate,
	}
}

// Resolve implements PathResolver.Resolve with caching
func (r *CachedPathResolver) Resolve(facts effectus.Facts, path FactPath) (interface{}, bool) {
	// Generate a cache key - simplified to avoid dependency on CacheKey
	// In a real implementation, you'd want to use a more robust key that uniquely identifies facts
	cacheKey := path.String()

	if value, ok := r.cache.Load(cacheKey); ok {
		// Cache hit
		if value == nil {
			return nil, false
		}
		return value, true
	}

	// Cache miss, delegate to underlying resolver
	value, found := r.delegate.Resolve(facts, path)

	// Cache the result (even if not found)
	if found {
		r.cache.Store(cacheKey, value)
	} else {
		r.cache.Store(cacheKey, nil)
	}

	return value, found
}

// ResolveWithContext implements PathResolver.ResolveWithContext with caching
func (r *CachedPathResolver) ResolveWithContext(facts effectus.Facts, path FactPath) (interface{}, *ResolutionResult) {
	value, found := r.Resolve(facts, path)

	// Create a result with cached information
	result := &ResolutionResult{
		Found: found,
		Value: value,
		Path:  path,
		Type:  r.Type(path),
	}

	return value, result
}

// Type implements PathResolver.Type with caching
func (r *CachedPathResolver) Type(path FactPath) interface{} {
	// Check type cache
	cacheKey := path.String()
	if typeInfo, ok := r.typeCache.Load(cacheKey); ok {
		return typeInfo
	}

	// Cache miss, delegate
	typeInfo := r.delegate.Type(path)

	// Cache the result
	r.typeCache.Store(cacheKey, typeInfo)

	return typeInfo
}

// ClearCache clears all cached values
func (r *CachedPathResolver) ClearCache() {
	r.cache = sync.Map{}
	r.typeCache = sync.Map{}
}
