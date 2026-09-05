package config

import (
	"bufio"
	"os"
	"strings"
	"sync"
)

var (
	dotenvOnce   sync.Once
	dotenvValues map[string]string
)

// LoadDotenv loads environment variables from a .env file.
// It does not override existing environment variables.
func LoadDotenv(paths ...string) {
	dotenvOnce.Do(func() {
		dotenvValues = make(map[string]string)

		if len(paths) == 0 {
			paths = []string{".env"}
		}

		for _, path := range paths {
			loadDotenvFile(path)
		}
	})
}

func loadDotenvFile(path string) {
	file, err := os.Open(path)
	if err != nil {
		return // silently ignore missing .env files
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	parseDotenv(scanner, dotenvValues)
}

// parseDotenv parses .env content from a reader into the provided map.
// It does not override keys that already exist in os environment.
func parseDotenv(r *bufio.Scanner, values map[string]string) {
	for r.Scan() {
		line := strings.TrimSpace(r.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		idx := strings.Index(line, "=")
		if idx == -1 {
			continue
		}

		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])

		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		if _, exists := os.LookupEnv(key); !exists {
			values[key] = value
		}
	}
}

type EnvProvider struct {
	Prefix string
}

func (p *EnvProvider) Get(key string) (string, bool) {
	fullKey := p.Prefix + key

	if val, ok := os.LookupEnv(fullKey); ok {
		return val, true
	}

	if val, ok := dotenvValues[fullKey]; ok {
		return val, true
	}

	return "", false
}
