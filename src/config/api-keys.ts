import type { ProviderAPIKeys, ModelSource } from "../types.js";
import { getEnvPath, readLocalEnv } from "../utils.js";
import { defaultProviderRegistry } from "../providers/index.js";

function resolveEnvValue(
  envVar: string,
  env: Record<string, string | undefined>,
  localEnv: Record<string, string>,
): string {
  const envVal = (env[envVar] ?? "").trim();
  if (envVal) return envVal;
  return (localEnv[envVar] ?? "").trim();
}

export function resolveProviderAPIKeys(
  env: Record<string, string | undefined>,
  root: string,
): ProviderAPIKeys {
  const localEnv = readLocalEnv(root);
  return {
    openRouter: resolveEnvValue("OPENROUTER_API_KEY", env, localEnv),
    nvidia: resolveEnvValue("NVIDIA_API_KEY", env, localEnv),
    copilot: resolveEnvValue("GITHUB_COPILOT_TOKEN", env, localEnv),
    zen: resolveEnvValue("OPENCODE_API_KEY", env, localEnv),
    google:
      resolveEnvValue("GOOGLE_API_KEY", env, localEnv) ||
      resolveEnvValue("GEMINI_API_KEY", env, localEnv),
  };
}

export function apiKeyFor(keys: ProviderAPIKeys, source: ModelSource): string {
  const adapter = defaultProviderRegistry.get(source);
  if (adapter) {
    const envVar = adapter.apiKeyEnvVar;
    const processVal = (process.env[envVar] ?? "").trim();
    if (processVal) return processVal;

    switch (source) {
      case "openrouter":
        return keys.openRouter;
      case "nvidia":
        return keys.nvidia;
      case "copilot":
        return keys.copilot;
      case "zen":
        return keys.zen;
      case "google":
        return keys.google;
    }
  }

  const customKeyName = `${source.toUpperCase().replace(/[^A-Z0-9_]/g, "_")}_API_KEY`;
  return (process.env[customKeyName] ?? "").trim();
}

export function requireAnyProviderAPIKey(
  env: Record<string, string | undefined>,
  root: string,
): ProviderAPIKeys {
  const keys = resolveProviderAPIKeys(env, root);
  if (
    !keys.openRouter &&
    !keys.nvidia &&
    !keys.copilot &&
    !keys.zen &&
    !keys.google
  ) {
    throw new Error(
      `API 키가 설정되지 않았어요.\n` +
        `  NVIDIA_API_KEY, OPENROUTER_API_KEY, GITHUB_COPILOT_TOKEN, OPENCODE_API_KEY, 또는 GOOGLE_API_KEY 중 하나 이상이 필요해요.\n` +
        `  설정 방법:\n` +
        `    1. 환경변수: export GOOGLE_API_KEY=AIza...\n` +
        `    2. .env 파일: echo "GOOGLE_API_KEY=AIza..." > ${getEnvPath(root)}`,
    );
  }
  return keys;
}
