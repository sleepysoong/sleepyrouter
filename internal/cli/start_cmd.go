package cli

import (
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"

	"github.com/sleepysoong/sleepyrouter/internal/cfg"
	"github.com/sleepysoong/sleepyrouter/internal/routing"
	"github.com/sleepysoong/sleepyrouter/internal/srv"
	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

// StartCommandOptions is the parameter struct for RunStartCommand.
type StartCommandOptions struct {
	Port  int
	Store *cfg.ConfigStore
}

func RunStartCommand(options StartCommandOptions) error {
	store := options.Store
	if store == nil {
		store = cfg.NewConfigStore("")
	}
	if err := store.EnsureRoot(); err != nil {
		return err
	}
	config, err := store.ReadConfig()
	if err != nil {
		return err
	}
	port := options.Port
	if port == 0 {
		port = config.Port
	}
	if port == 0 {
		port = cfg.DefaultPort
	}
	if config.Port != port {
		config.Port = port
		if err := store.WriteConfig(config); err != nil {
			return err
		}
	}

	env := utils.CurrentEnvironment()
	keys := cfg.ResolveProviderAPIKeys(env, store.Paths.Root)
	hasNvidiaKey := keys.NVIDIA != ""
	hasOpenRouterKey := keys.OpenRouter != ""
	hasZenKey := keys.Zen != ""

	fmt.Printf("\nsleepyrouter v%s\n", types.Version)
	fmt.Printf("  config: %s\n", utils.GetConfigPath(store.Paths.Root))
	fmt.Printf("  env: %s\n", utils.GetEnvPath(store.Paths.Root))
	fmt.Printf("  NVIDIA_API_KEY: %s\n", boolCheck(hasNvidiaKey))
	fmt.Printf("  OPENROUTER_API_KEY: %s\n", boolCheck(hasOpenRouterKey))
	fmt.Printf("  OPENCODE_API_KEY: %s\n", boolCheck(hasZenKey))

	if _, err := cfg.RequireAnyProviderAPIKey(env, store.Paths.Root); err != nil {
		return err
	}

	// Validate all model aliases are defined in config.Models
	undefinedAliases := undefinedModelAliases(config.ModelGroups, config.Models)
	groupNames := make([]string, 0, len(config.ModelGroups))
	for name := range config.ModelGroups {
		groupNames = append(groupNames, name)
	}
	sort.Strings(groupNames)
	if len(undefinedAliases) > 0 {
		msg := "\n모델 그룹에 정의되지 않은 alias가 있어요. config.json의 models에 추가하세요:\n"
		for _, m := range undefinedAliases {
			msg += fmt.Sprintf("  - %s\n", m)
		}
		return fmt.Errorf("%s\nconfig.json을 수정한 후 다시 시도하세요.", msg)
	}

	if len(groupNames) > 0 {
		totalModels := len(routing.AllGroupModelIDs(config.ModelGroups, config.GroupOrder...))
		fmt.Printf("\n모델 그룹 (%d개 모델, %d개 그룹)\n", totalModels, len(groupNames))
	for _, name := range groupNames {
		marker := ""
		if name == config.DefaultModelGroup {
			marker = " (기본)"
		}
		fmt.Printf("  %s%s: %s\n", name, marker, strings.Join(config.ModelGroups[name], ", "))
	}
	if config.DefaultModelGroup != "" {
		fmt.Printf("\n기본 그룹: %s\n", config.DefaultModelGroup)
	}
		fmt.Println()
	}

	server := srv.CreateSleepyRouterServer(srv.ServerOptions{
		Store: store,
		RequestLogger: func(event srv.ServerLogEvent) {
			fmt.Println(srv.FormatServerLogEvent(event, utils.IsTerminal(os.Stdout)))
		},
	})
	actualPort, err := srv.Listen(server, port)
	if err != nil {
		return err
	}
	fmt.Printf("sleepyrouter가 http://localhost:%d에서 실행 중이에요.\n", actualPort)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	server.Close()
	return nil
}

func boolCheck(value bool) string {
	if value {
		return "✓"
	}
	return "✗"
}

func undefinedModelAliases(groups types.ModelGroups, models map[string]types.ModelDefinition) []string {
	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	sort.Strings(names)
	var result []string
	for _, name := range names {
		for _, id := range groups[name] {
			if _, ok := models[id]; !ok {
				result = append(result, name+": "+id)
			}
		}
	}
	return result
}
