package cfg

import (
	"fmt"
	"strings"

	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

func resolveAPIKey(name string, env utils.Environment, localEnv map[string]string) string {
	if env == nil {
		env = utils.CurrentEnvironment()
	}
	if value := strings.TrimSpace(env[name]); value != "" {
		return value
	}
	return strings.TrimSpace(localEnv[name])
}

func resolveOpenRouterAPIKey(env utils.Environment, localEnv map[string]string) string {
	return resolveAPIKey("OPENROUTER_API_KEY", env, localEnv)
}

func resolveNVIDIAAPIKey(env utils.Environment, localEnv map[string]string) string {
	return resolveAPIKey("NVIDIA_API_KEY", env, localEnv)
}

func resolveCopilotAPIKey(env utils.Environment, localEnv map[string]string) string {
	return resolveAPIKey("GITHUB_COPILOT_TOKEN", env, localEnv)
}

func resolveZenAPIKey(env utils.Environment, localEnv map[string]string) string {
	return resolveAPIKey("OPENCODE_API_KEY", env, localEnv)
}

func resolveGoogleAPIKey(env utils.Environment, localEnv map[string]string) string {
	if key := resolveAPIKey("GOOGLE_API_KEY", env, localEnv); key != "" {
		return key
	}
	return resolveAPIKey("GEMINI_API_KEY", env, localEnv)
}

func ResolveProviderAPIKeys(env utils.Environment, root string) types.ProviderAPIKeys {
	localEnv := utils.ReadLocalEnv(root)
	return types.ProviderAPIKeys{
		OpenRouter: resolveOpenRouterAPIKey(env, localEnv),
		NVIDIA:     resolveNVIDIAAPIKey(env, localEnv),
		Copilot:    resolveCopilotAPIKey(env, localEnv),
		Zen:        resolveZenAPIKey(env, localEnv),
		Google:     resolveGoogleAPIKey(env, localEnv),
	}
}

func RequireAnyProviderAPIKey(env utils.Environment, root string) (types.ProviderAPIKeys, error) {
	keys := ResolveProviderAPIKeys(env, root)
	if keys.OpenRouter == "" && keys.NVIDIA == "" && keys.Copilot == "" && keys.Zen == "" && keys.Google == "" {
		return types.ProviderAPIKeys{}, fmt.Errorf("API 키가 설정되지 않았어요.\n  NVIDIA_API_KEY, OPENROUTER_API_KEY, GITHUB_COPILOT_TOKEN, OPENCODE_API_KEY, 또는 GOOGLE_API_KEY 중 하나 이상이 필요해요.\n  설정 방법:\n    1. 환경변수: export GOOGLE_API_KEY=AIza...\n    2. .env 파일: echo \"GOOGLE_API_KEY=AIza...\" > %s", utils.GetEnvPath(root))
	}
	return keys, nil
}
