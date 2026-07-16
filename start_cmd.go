package sleepyrouter

import (
	"fmt"
	"os"
	"os/signal"
	"sort"
	"syscall"
)

func RunStartCommand(options struct {
	Port  int
	Store *ConfigStore
}) error {
	store := options.Store
	if store == nil {
		store = NewConfigStore("")
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
		port = DefaultPort
	}
	if config.Port != port {
		config.Port = port
		if err := store.WriteConfig(config); err != nil {
			return err
		}
	}

	env := CurrentEnvironment()
	keys := ResolveProviderAPIKeys(env, store.Paths.Root)
	hasNvidiaKey := keys.NVIDIA != ""
	hasOpenRouterKey := keys.OpenRouter != ""

	fmt.Printf("\nsleepyrouter v%s\n", Version)
	fmt.Printf("  config: %s\n", GetConfigPath(store.Paths.Root))
	fmt.Printf("  env: %s\n", GetEnvPath(store.Paths.Root))
	fmt.Printf("  NVIDIA_API_KEY: %s\n", boolCheck(hasNvidiaKey))
	fmt.Printf("  OPENROUTER_API_KEY: %s\n", boolCheck(hasOpenRouterKey))

	if _, err := RequireAnyProviderAPIKey(env, store.Paths.Root); err != nil {
		return err
	}

	// Validate model IDs have provider prefixes
	invalidModels := []string{}
	groupNames := make([]string, 0, len(config.ModelGroups))
	for name := range config.ModelGroups {
		groupNames = append(groupNames, name)
	}
	sort.Strings(groupNames)
	for _, group := range groupNames {
		for _, id := range config.ModelGroups[group] {
			if !startsWith(id, "nvidia/") && !startsWith(id, "openrouter/") {
				invalidModels = append(invalidModels, fmt.Sprintf("%s: %s", group, id))
			}
		}
	}
	if len(invalidModels) > 0 {
		fmt.Fprintln(os.Stderr, "\n모델 ID가 잘못되었어요. nvidia/ 또는 openrouter/ 접두사가 필요해요:")
		for _, m := range invalidModels {
			fmt.Fprintf(os.Stderr, "  - %s\n", m)
		}
		fmt.Fprintln(os.Stderr, "\nconfig.json을 수정한 후 다시 시도하세요.")
		os.Exit(1)
	}

	if len(groupNames) > 0 {
		totalModels := len(AllGroupModelIDs(config.ModelGroups, config.GroupOrder...))
		fmt.Printf("\n모델 그룹 (%d개 모델, %d개 그룹)\n", totalModels, len(groupNames))
		for _, name := range groupNames {
			marker := ""
			if name == config.DefaultGroup {
				marker = " (기본)"
			}
			fmt.Printf("  %s%s: %s\n", name, marker, joinStrings(config.ModelGroups[name], ", "))
		}
		if config.DefaultGroup != "" {
			fmt.Printf("\n기본 그룹: %s\n", config.DefaultGroup)
		}
		fmt.Println()
	}

	server := CreateSleepyRouterServer(ServerOptions{
		Store:         store,
		RequestLogger: func(event ServerLogEvent) { fmt.Println(FormatServerLogEvent(event, isTerminal(os.Stdout))) },
	})
	actualPort, err := Listen(server, port)
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

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
