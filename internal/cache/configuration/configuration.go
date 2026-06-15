package configuration

// ExpandCacheConfiguration returns the cache definitions unchanged. Cache keys
// are resolved at send time (see ResolveCacheKey), so there is nothing to
// expand at load time.
func ExpandCacheConfiguration(caches []Cache) ([]Cache, error) {
	return caches, nil
}
