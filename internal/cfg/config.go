package cfg

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/routing"
	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

const (
	DefaultPort   = 4567
	ModelCacheTTL = 5 * time.Minute
)

type StorePaths struct {
	Root           string
	ConfigPath     string
	UsagePath      string
	ModelCachePath string
}

func CreateStorePaths(root string) StorePaths {
	return StorePaths{
		Root:           root,
		ConfigPath:     utils.GetConfigPath(root),
		UsagePath:      utils.GetUsagePath(root),
		ModelCachePath: utils.GetModelCachePath(root),
	}
}

type ConfigStore struct {
	Paths StorePaths
}

func NewConfigStore(root string) *ConfigStore {
	if root == "" {
		root = utils.GetConfigRoot(nil)
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

func (store *ConfigStore) ReadConfig() (types.SleepyRouterConfig, error) {
	var raw map[string]json.RawMessage
	exists, err := readFileJSON(store.Paths.ConfigPath, &raw)
	if err != nil {
		return types.SleepyRouterConfig{}, err
	}
	if !exists {
		return types.SleepyRouterConfig{Port: DefaultPort, ModelGroups: types.ModelGroups{}}, nil
	}
	config := types.SleepyRouterConfig{Port: DefaultPort, ModelGroups: types.ModelGroups{}}
	if portRaw, ok := raw["port"]; ok {
		var port float64
		if json.Unmarshal(portRaw, &port) == nil && port == float64(int(port)) {
			config.Port = int(port)
		}
	}
	if groupsRaw, ok := raw["modelGroups"]; ok {
		var value any
		if json.Unmarshal(groupsRaw, &value) == nil {
			config.ModelGroups, config.GroupOrder = routing.NormalizeModelGroupsOrdered(value)
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

func (store *ConfigStore) WriteConfig(config types.SleepyRouterConfig) error {
	if config.Port == 0 {
		config.Port = DefaultPort
	}
	if config.ModelGroups == nil {
		config.ModelGroups = types.ModelGroups{}
	}
	config.GroupOrder = types.CompleteGroupOrder(config.ModelGroups, config.GroupOrder)
	return writeFileJSON(store.Paths.ConfigPath, config)
}

func (store *ConfigStore) UpdateModelGroup(group string, modelIDs []string) (types.SleepyRouterConfig, error) {
	config, err := store.ReadConfig()
	if err != nil {
		return types.SleepyRouterConfig{}, err
	}
	if config.ModelGroups == nil {
		config.ModelGroups = types.ModelGroups{}
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
		return types.SleepyRouterConfig{}, err
	}
	return config, nil
}

func (store *ConfigStore) AppendUsage(entry types.UsageLogEntry) error {
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

func (store *ConfigStore) ReadUsageLogs() ([]types.UsageLogEntry, error) {
	data, err := os.ReadFile(store.Paths.UsagePath)
	if errors.Is(err, os.ErrNotExist) {
		return []types.UsageLogEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return []types.UsageLogEntry{}, nil
	}
	entries := make([]types.UsageLogEntry, 0)
	skipped := 0
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		var entry types.UsageLogEntry
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

func (store *ConfigStore) ReadModelCache() (*types.ModelCache, error) {
	var cache types.ModelCache
	exists, err := readFileJSON(store.Paths.ModelCachePath, &cache)
	if err != nil || !exists {
		return nil, err
	}
	return &cache, nil
}

func (store *ConfigStore) WriteModelCache(cache types.ModelCache) error {
	return writeFileJSON(store.Paths.ModelCachePath, cache)
}

func IsModelCacheFresh(cache types.ModelCache, now time.Time) bool {
	fetchedAt, err := time.Parse(time.RFC3339, cache.FetchedAt)
	if err != nil {
		return false
	}
	return now.Sub(fetchedAt) < ModelCacheTTL
}


