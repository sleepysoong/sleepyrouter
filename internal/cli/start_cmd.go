package cli

import (
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"

	"github.com/sleepysoong/sleepyrouter/internal/cfg"
	"github.com/sleepysoong/sleepyrouter/internal/providers"
	"github.com/sleepysoong/sleepyrouter/internal/routing"
	"github.com/sleepysoong/sleepyrouter/internal/srv"
	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

func RunStartCommand(options struct {
	Port  int
	Store *cfg.ConfigStore
}) error {
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

	fmt.Printf("\nsleepyrouter v%s\n", types.Version)
	fmt.Printf("  config: %s\n", utils.GetConfigPath(store.Paths.Root))
	fmt.Printf("  env: %s\n", utils.GetEnvPath(store.Paths.Root))
	fmt.Printf("  NVIDIA_API_KEY: %s\n", boolCheck(hasNvidiaKey))
	fmt.Printf("  OPENROUTER_API_KEY: %s\n", boolCheck(hasOpenRouterKey))

	if _, err := cfg.RequireAnyProviderAPIKey(env, store.Paths.Root); err != nil {
		return err
	}

	// Validate model IDs have provider prefixes
	invalidModels := invalidModelIDs(config.ModelGroups)
	groupNames := make([]string, 0, len(config.ModelGroups))
	for name := range config.ModelGroups {
		groupNames = append(groupNames, name)
	}
	sort.Strings(groupNames)
	if len(invalidModels) > 0 {
		fmt.Fprintln(os.Stderr, "\n모델 ID가 잘못되었어요. nvidia/, openrouter/ 또는 copilot/ 접두사가 필요해요:")
		for _, m := range invalidModels {
			fmt.Fprintf(os.Stderr, "  - %s\n", m)
		}
		fmt.Fprintln(os.Stderr, "\nconfig.json을 수정한 후 다시 시도하세요.")
		os.Exit(1)
	}

	if len(groupNames) > 0 {
		totalModels := len(routing.AllGroupModelIDs(config.ModelGroups, config.GroupOrder...))
		fmt.Printf("\n모델 그룹 (%d개 모델, %d개 그룹)\n", totalModels, len(groupNames))
		for _, name := range groupNames {
			marker := ""
			if name == config.DefaultGroup {
				marker = " (기본)"
			}
			fmt.Printf("  %s%s: %s\n", name, marker, providers.JoinStrings(config.ModelGroups[name], ", "))
		}
		if config.DefaultGroup != "" {
			fmt.Printf("\n기본 그룹: %s\n", config.DefaultGroup)
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

func invalidModelIDs(groups types.ModelGroups) []string {
	prefixes := []string{"nvidia/", "openrouter/", "copilot/"}
	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	sort.Strings(names)
	var result []string
	for _, name := range names {
		for _, id := range groups[name] {
			valid := false
			for _, prefix := range prefixes {
				if strings.HasPrefix(id, prefix) {
					valid = true
					break
				}
			}
			if !valid {
				result = append(result, name+": "+id)
			}
		}
	}
	return result
}
