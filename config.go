package sleepyrouter

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultPort        = 4567
	ModelCacheTTL      = 5 * time.Minute
	configFileName     = "config.json"
	usageFileName      = "usage.jsonl"
	modelCacheFileName = "models-cache.json"
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

func GetConfigPath(root string) string     { return filepath.Join(root, configFileName) }
func GetUsagePath(root string) string      { return filepath.Join(root, usageFileName) }
func GetModelCachePath(root string) string { return filepath.Join(root, modelCacheFileName) }
func GetEnvPath(root string) string        { return filepath.Join(root, ".env") }
func GetLogPath(root string) string        { return filepath.Join(root, "sleepyrouter.log") }

type StorePaths struct {
	Root           string
	ConfigPath     string
	UsagePath      string
	ModelCachePath string
}

func CreateStorePaths(root string) StorePaths {
	return StorePaths{
		Root:           root,
		ConfigPath:     GetConfigPath(root),
		UsagePath:      GetUsagePath(root),
		ModelCachePath: GetModelCachePath(root),
	}
}

type ConfigStore struct {
	Paths StorePaths
}

func NewConfigStore(root string) *ConfigStore {
	if root == "" {
		root = GetConfigRoot(nil)
	}
	return &ConfigStore{Paths: CreateStorePaths(root)}
}

func (store *ConfigStore) EnsureRoot() error {
	return os.MkdirAll(store.Paths.Root, 0o755)
}

func readFileJSON(path string, target any) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return true, fmt.Errorf("%s 파싱에 실패했어요 (%dB): %w", path, len(data), err)
	}
	return true, nil
}

func writeFileJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".sleepyrouter-*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}

func (config SleepyRouterConfig) MarshalJSON() ([]byte, error) {
	groups := config.ModelGroups
	if groups == nil {
		groups = ModelGroups{}
	}
	order := completeGroupOrder(groups, config.GroupOrder)
	var out bytes.Buffer
	out.WriteString(`{"port":`)
	out.WriteString(strconv.Itoa(config.Port))
	out.WriteString(`,"modelGroups":{`)
	for index, name := range order {
		if index > 0 {
			out.WriteByte(',')
		}
		key, err := json.Marshal(name)
		if err != nil {
			return nil, err
		}
		ids, err := json.Marshal(groups[name])
		if err != nil {
			return nil, err
		}
		out.Write(key)
		out.WriteByte(':')
		out.Write(ids)
	}
	out.WriteByte('}')
	if config.DefaultGroup != "" {
		value, err := json.Marshal(config.DefaultGroup)
		if err != nil {
			return nil, err
		}
		out.WriteString(`,"defaultGroup":`)
		out.Write(value)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

func (store *ConfigStore) ReadConfig() (SleepyRouterConfig, error) {
	var raw map[string]json.RawMessage
	exists, err := readFileJSON(store.Paths.ConfigPath, &raw)
	if err != nil {
		return SleepyRouterConfig{}, err
	}
	if !exists {
		return SleepyRouterConfig{Port: DefaultPort, ModelGroups: ModelGroups{}}, nil
	}
	config := SleepyRouterConfig{Port: DefaultPort, ModelGroups: ModelGroups{}}
	if portRaw, ok := raw["port"]; ok {
		var port float64
		if json.Unmarshal(portRaw, &port) == nil && port == float64(int(port)) {
			config.Port = int(port)
		}
	}
	if groupsRaw, ok := raw["modelGroups"]; ok {
		var value any
		if json.Unmarshal(groupsRaw, &value) == nil {
			config.ModelGroups, config.GroupOrder = NormalizeModelGroupsOrdered(value)
			if keys := objectKeysInJSON(groupsRaw); len(keys) > 0 {
				config.GroupOrder = keys
			}
		}
	}
	if defaultRaw, ok := raw["defaultGroup"]; ok {
		_ = json.Unmarshal(defaultRaw, &config.DefaultGroup)
	}
	return config, nil
}

func objectKeysInJSON(data []byte) []string {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil
	}
	keys := []string{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil
		}
		key, ok := token.(string)
		if !ok {
			return nil
		}
		var ignored json.RawMessage
		if decoder.Decode(&ignored) != nil {
			return nil
		}
		keys = append(keys, key)
	}
	return keys
}

func (store *ConfigStore) WriteConfig(config SleepyRouterConfig) error {
	if config.Port == 0 {
		config.Port = DefaultPort
	}
	if config.ModelGroups == nil {
		config.ModelGroups = ModelGroups{}
	}
	config.GroupOrder = completeGroupOrder(config.ModelGroups, config.GroupOrder)
	return writeFileJSON(store.Paths.ConfigPath, config)
}

func (store *ConfigStore) UpdateModelGroup(group string, modelIDs []string) (SleepyRouterConfig, error) {
	config, err := store.ReadConfig()
	if err != nil {
		return SleepyRouterConfig{}, err
	}
	if config.ModelGroups == nil {
		config.ModelGroups = ModelGroups{}
	}
	_, exists := config.ModelGroups[group]
	seen := make(map[string]bool)
	ids := make([]string, 0, len(modelIDs))
	for _, id := range modelIDs {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	config.ModelGroups[group] = ids
	if !exists {
		config.GroupOrder = append(config.GroupOrder, group)
	}
	if err := store.WriteConfig(config); err != nil {
		return SleepyRouterConfig{}, err
	}
	return config, nil
}

func (store *ConfigStore) AppendUsage(entry UsageLogEntry) error {
	if err := os.MkdirAll(filepath.Dir(store.Paths.UsagePath), 0o755); err != nil {
		return err
	}
	oldJSON := strings.TrimSuffix(store.Paths.UsagePath, ".jsonl") + ".json"
	if _, err := os.Stat(oldJSON); err == nil {
		if err := os.Rename(oldJSON, oldJSON+".bak"); err != nil {
			return err
		}
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(store.Paths.UsagePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(append(data, '\n'))
	return err
}

func (store *ConfigStore) ReadUsageLogs() ([]UsageLogEntry, error) {
	data, err := os.ReadFile(store.Paths.UsagePath)
	if errors.Is(err, os.ErrNotExist) {
		return []UsageLogEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return []UsageLogEntry{}, nil
	}
	entries := make([]UsageLogEntry, 0)
	skipped := 0
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		var entry UsageLogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			skipped++
			continue
		}
		entries = append(entries, entry)
	}
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "[sleepyrouter] 경고: 사용 기록 파일에서 %d줄이 손상되어 건너뛰었어요.\n", skipped)
	}
	return entries, nil
}

func (store *ConfigStore) ReadModelCache() (*ModelCache, error) {
	var cache ModelCache
	exists, err := readFileJSON(store.Paths.ModelCachePath, &cache)
	if err != nil || !exists {
		return nil, err
	}
	return &cache, nil
}

func (store *ConfigStore) WriteModelCache(cache ModelCache) error {
	return writeFileJSON(store.Paths.ModelCachePath, cache)
}

func IsModelCacheFresh(cache ModelCache, now time.Time) bool {
	fetchedAt, err := time.Parse(time.RFC3339, cache.FetchedAt)
	if err != nil {
		return false
	}
	return now.Sub(fetchedAt) < ModelCacheTTL
}

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

func resolveAPIKey(name string, env Environment, root string) string {
	if env == nil {
		env = CurrentEnvironment()
	}
	if value := strings.TrimSpace(env[name]); value != "" {
		return value
	}
	return strings.TrimSpace(ReadLocalEnv(root)[name])
}

func ResolveOpenRouterAPIKey(env Environment, root string) string {
	return resolveAPIKey("OPENROUTER_API_KEY", env, root)
}
func ResolveNVIDIAAPIKey(env Environment, root string) string {
	return resolveAPIKey("NVIDIA_API_KEY", env, root)
}
func ResolveCopilotAPIKey(env Environment, root string) string {
	return resolveAPIKey("GITHUB_COPILOT_TOKEN", env, root)
}

func ResolveProviderAPIKeys(env Environment, root string) ProviderAPIKeys {
	return ProviderAPIKeys{
		OpenRouter: ResolveOpenRouterAPIKey(env, root),
		NVIDIA:     ResolveNVIDIAAPIKey(env, root),
		Copilot:    ResolveCopilotAPIKey(env, root),
	}
}

func RequireAnyProviderAPIKey(env Environment, root string) (ProviderAPIKeys, error) {
	keys := ResolveProviderAPIKeys(env, root)
	if keys.OpenRouter == "" && keys.NVIDIA == "" && keys.Copilot == "" {
		return ProviderAPIKeys{}, fmt.Errorf("API 키가 설정되지 않았어요.\n  NVIDIA_API_KEY, OPENROUTER_API_KEY, 또는 GITHUB_COPILOT_TOKEN 중 하나 이상이 필요해요.\n  설정 방법:\n    1. 환경변수: export NVIDIA_API_KEY=nvapi-...\n    2. .env 파일: echo \"NVIDIA_API_KEY=nvapi-...\" > %s", GetEnvPath(root))
	}
	return keys, nil
}
