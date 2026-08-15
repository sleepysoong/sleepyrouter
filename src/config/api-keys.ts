import type { ProviderAPIKeys, ModelSource } from "../types.js";
import { getEnvPath, readLocalEnv } from "../utils.js";

function resolveAPIKey(
  name: string,
  env: Record<string, string | undefined>,
  localEnv: Record<string, string>,
): string {
  const envVal = (env[name] ?? "").trim();
  if (envVal) return envVal;
  return (localEnv[name] ?? "").trim();
}

export function resolveProviderAPIKeys(
  env: Record<string, string | undefined>,
  root: string,
): ProviderAPIKeys {
  const localEnv = readLocalEnv(root);
  return {
    openRouter: resolveAPIKey("OPENROUTER_API_KEY", env, localEnv),
    nvidia: resolveAPIKey("NVIDIA_API_KEY", env, localEnv),
    copilot: resolveAPIKey("GITHUB_COPILOT_TOKEN", env, localEnv),
    zen: resolveAPIKey("OPENCODE_API_KEY", env, localEnv),
    google:
      resolveAPIKey("GOOGLE_API_KEY", env, localEnv) ||
      resolveAPIKey("GEMINI_API_KEY", env, localEnv),
  };
}

export function apiKeyFor(keys: ProviderAPIKeys, source: ModelSource): string {
  switch (source) {
    case "nvidia":
      return keys.nvidia;
    case "copilot":
      return keys.copilot;
    case "zen":
      return keys.zen;
    case "google":
      return keys.google;
    default:
      return keys.openRouter;
  }
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
