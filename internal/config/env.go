package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// ResolveHome returns the sleepyrouter home directory.
func ResolveHome() string {
	if h := os.Getenv("SLEEPYROUTER_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".sleepyrouter"
	}
	return filepath.Join(home, ".sleepyrouter")
}

func ConfigPath(home string) string  { return filepath.Join(home, "config.toml") }
func EnvPath(home string) string     { return filepath.Join(home, ".env") }
func UsageDBPath(home string) string { return filepath.Join(home, "usage.db") }

// LoadDotenv parses a simple .env file (KEY=VALUE, # comments, quotes).
func LoadDotenv(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		k := strings.TrimSpace(line[:eq])
		v := strings.TrimSpace(line[eq+1:])
		if len(v) >= 2 {
			if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
				v = v[1 : len(v)-1]
			}
		}
		if k != "" {
			out[k] = v
		}
	}
	return out
}

// LookupEnv implements precedence: process env wins over dotenv file content.
func LookupEnv(key string, dotenv map[string]string) (string, bool) {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v), true
	}
	if v, ok := dotenv[key]; ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v), true
	}
	return "", false
}

// DefaultAPIKeyEnv returns the conventional env name for a provider.
func DefaultAPIKeyEnv(providerID string) string {
	upper := strings.ToUpper(strings.ReplaceAll(providerID, "-", "_"))
	return upper + "_API_KEY"
}

// Built-in provider defaults.
func builtinProviderDefault(id string) (baseURL, env string) {
	switch id {
	case "zen":
		return "https://opencode.ai/zen/v1", "OPENCODE_API_KEY"
	case "nvidia":
		return "https://integrate.api.nvidia.com/v1", "NVIDIA_API_KEY"
	case "gemini":
		return "https://generativelanguage.googleapis.com/v1beta/openai/", "GEMINI_API_KEY"
	case "openrouter":
		return "https://openrouter.ai/api/v1", "OPENROUTER_API_KEY"
	default:
		return "", DefaultAPIKeyEnv(id)
	}
}

// BuiltinProviderDefault is exported for provider package use.
func BuiltinProviderDefault(id string) (string, string) { return builtinProviderDefault(id) }
