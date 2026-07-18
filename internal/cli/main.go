package cli

import (
	"fmt"
	"os"
	"strconv"

	"github.com/sleepysoong/sleepyrouter/internal/types"
)

func parseArgs(argv []string) (command string, flags map[string]any) {
	if len(argv) == 0 {
		return "help", map[string]any{}
	}
	command = argv[0]
	flags = map[string]any{}
	for i := 1; i < len(argv); i++ {
		arg := argv[i]
		if len(arg) > 2 && arg[:2] == "--" {
			rest := arg[2:]
			if idx := indexByte(rest, '='); idx >= 0 {
				flags[rest[:idx]] = rest[idx+1:]
			} else if i+1 < len(argv) && argv[i+1][0] != '-' {
				i++
				flags[rest] = argv[i]
			} else {
				flags[rest] = true
			}
		}
	}
	return
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func parsePort(value any) (int, error) {
	if value == nil || value == true {
		return 0, nil
	}
	s := fmt.Sprint(value)
	port, err := strconv.Atoi(s)
	if err != nil || port < 0 || port > 65535 {
		return 0, fmt.Errorf("잘못된 --port 값: %s (0~65535 사이의 숫자를 입력하세요)", s)
	}
	return port, nil
}

func helpText() string {
	return fmt.Sprintf("sleepyrouter %s\n\n사용법:\n  sleepyrouter start [--port 4567]\n  sleepyrouter usage [--date YYYYMMDD|--week NN]\n  sleepyrouter --version\n", types.Version)
}

func Main() {
	command, flags := parseArgs(os.Args[1:])

	switch command {
	case "--version", "-v", "version":
		fmt.Println(types.Version)
		return
	case "help", "--help", "-h":
		fmt.Print(helpText())
		return
	case "start":
		port, err := parsePort(flags["port"])
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
		if err := RunStartCommand(StartCommandOptions{Port: port}); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
	case "usage":
		date := ""
		week := 0
		if d, ok := flags["date"].(string); ok {
			date = d
		}
		if w, ok := flags["week"].(string); ok {
			if n, err := strconv.Atoi(w); err == nil {
				week = n
			}
		}
		RunUsageCommand(UsageCommandOptions{Date: date, Week: week})
	default:
		fmt.Print(helpText())
		os.Exit(1)
	}
}
