package utils

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	configFileName = "config.json"
	usageFileName  = "usage.jsonl"
)

type Environment map[string]string

func CurrentEnvironment() Environment {
	env := make(Environment)
	for _, pair := range os.Environ() {
		key, value, ok := strings.Cut(pair, "=")
		if ok {
			env[key] = value
		}
	}
	return env
}

func GetConfigRoot(env Environment) string {
	if env == nil {
		env = CurrentEnvironment()
	}
	if root := env["SLEEPYROUTER_HOME"]; root != "" {
		return root
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".sleepyrouter"
	}
	return filepath.Join(home, ".sleepyrouter")
}

func GetConfigPath(root string) string { return filepath.Join(root, configFileName) }

func GetUsagePath(root string) string { return filepath.Join(root, usageFileName) }

func GetEnvPath(root string) string { return filepath.Join(root, ".env") }

func ParseDotEnv(content string) map[string]string {
	values := make(map[string]string)
	for _, rawLine := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found || strings.TrimSpace(key) == "" {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		values[key] = value
	}
	return values
}

func ReadLocalEnv(root string) map[string]string {
	data, err := os.ReadFile(GetEnvPath(root))
	if err != nil {
		return map[string]string{}
	}
	return ParseDotEnv(string(data))
}
