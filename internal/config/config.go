package config

import (
	"strconv"
	"sync"
)

type Provider interface {
	Get(key string) (string, bool)
}

type configCache struct {
	provider Provider
	values   map[string]string
	mu       sync.RWMutex
}

var cache *configCache

func init() {
	cache = &configCache{
		provider: new(EnvProvider),
		values:   make(map[string]string),
	}
}

func SetProvider(provider Provider) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.provider = provider
	cache.values = make(map[string]string)
}

func (cache *configCache) get(key string) (string, bool) {
	cache.mu.RLock()
	value, cached := cache.values[key]
	cache.mu.RUnlock()

	if cached {
		return value, true
	}

	value, exists := cache.provider.Get(key)
	if exists {
		cache.mu.Lock()
		cache.values[key] = value
		cache.mu.Unlock()
		return value, true
	}

	return "", false
}

type allowedConfigs interface {
	~string | ~int | ~float64 | bool
}

func Get[T allowedConfigs](key string, fallback ...T) (val T) {
	if len(fallback) > 0 {
		val = fallback[0]
	}

	raw, ok := cache.get(key)
	if !ok {
		return val
	}

	var zero T
	switch any(zero).(type) {
	case string:
		return any(raw).(T)
	case int:
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return val
		}
		return any(parsed).(T)
	case float64:
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return val
		}
		return any(parsed).(T)
	case bool:
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return val
		}
		return any(parsed).(T)
	}

	return val
}
