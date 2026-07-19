package cfg

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/sleepysoong/sleepyrouter/internal/routing"
	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"

	_ "modernc.org/sqlite"
)

const (
	DefaultPort = 4567
)

type StorePaths struct {
	Root       string
	ConfigPath string
	UsagePath  string
}

func CreateStorePaths(root string) StorePaths {
	return StorePaths{
		Root:       root,
		ConfigPath: utils.GetConfigPath(root),
		UsagePath:  utils.GetUsagePath(root),
	}
}

type ConfigStore struct {
	Paths StorePaths
	db    *sql.DB
	once  sync.Once
	dbErr error
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

func (store *ConfigStore) usageDBPath() string {
	return filepath.Join(store.Paths.Root, "usage.db")
}

func (store *ConfigStore) initDB() error {
	store.once.Do(func() {
		store.db, store.dbErr = sql.Open("sqlite", store.usageDBPath())
		if store.dbErr != nil {
			return
		}
		_, store.dbErr = store.db.Exec(`CREATE TABLE IF NOT EXISTS usage_log (
			ts TEXT NOT NULL,
			model TEXT NOT NULL,
			input_tokens INTEGER NOT NULL,
			output_tokens INTEGER NOT NULL,
			success INTEGER NOT NULL
		)`)
	})
	return store.dbErr
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
	if defaultRaw, ok := raw["defaultModelGroup"]; ok {
		_ = json.Unmarshal(defaultRaw, &config.DefaultModelGroup)
	} else if defaultRaw, ok := raw["defaultGroup"]; ok {
		_ = json.Unmarshal(defaultRaw, &config.DefaultModelGroup)
	}
	if modelsRaw, ok := raw["models"]; ok {
		_ = json.Unmarshal(modelsRaw, &config.Models)
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
	if err := store.initDB(); err != nil {
		return err
	}
	success := 0
	if entry.Success {
		success = 1
	}
	_, err := store.db.Exec("INSERT INTO usage_log(ts,model,input_tokens,output_tokens,success) VALUES(?,?,?,?,?)",
		entry.TS, entry.Model, entry.InputTokens, entry.OutputTokens, success)
	return err
}

func (store *ConfigStore) ReadUsageLogs() ([]types.UsageLogEntry, error) {
	if err := store.initDB(); err != nil {
		return nil, err
	}
	rows, err := store.db.Query("SELECT ts,model,input_tokens,output_tokens,success FROM usage_log ORDER BY rowid")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []types.UsageLogEntry
	for rows.Next() {
		var entry types.UsageLogEntry
		var success int
		if err := rows.Scan(&entry.TS, &entry.Model, &entry.InputTokens, &entry.OutputTokens, &success); err != nil {
			continue
		}
		entry.Success = success != 0
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}
