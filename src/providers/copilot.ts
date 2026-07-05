import { FetchLike, OmfmModel } from '../types.js';
import { VERSION } from '../version.js';

export const COPILOT_CHAT_COMPLETIONS_URL = 'https://api.githubcopilot.com/chat/completions';
export const COPILOT_TOKEN_URL = 'https://api.github.com/copilot_internal/v2/token';

interface CopilotToken {
  token: string;
  expiresAt: number;
}

let cachedToken: CopilotToken | undefined;
const TOKEN_REFRESH_MARGIN_MS = 5 * 60 * 1000; // 만료 5분 전에 갱신

interface CopilotModelDef {
  id: string;
  name: string;
  contextLength: number;
}

const KNOWN_MODELS: CopilotModelDef[] = [
  { id: 'gpt-4o', name: 'GPT-4o', contextLength: 128_000 },
  { id: 'gpt-4o-mini', name: 'GPT-4o Mini', contextLength: 128_000 },
  { id: 'o3-mini', name: 'o3-mini', contextLength: 200_000 },
  { id: 'claude-sonnet-4', name: 'Claude Sonnet 4', contextLength: 200_000 },
  { id: 'gemini-2.5-pro', name: 'Gemini 2.5 Pro', contextLength: 1_000_000 },
];

function copilotUsageId(upstreamId: string): string {
  return `copilot/${upstreamId}`;
}

function normalizeCopilotModel(def: CopilotModelDef): OmfmModel {
  return {
    id: `copilot/${def.id}`,
    upstreamId: def.id,
    name: def.name,
    provider: 'copilot',
    source: 'copilot',
    usageId: copilotUsageId(def.id),
    contextLength: def.contextLength,
  };
}

async function exchangeToken(apiKey: string, fetchImpl: FetchLike): Promise<CopilotToken> {
  const response = await fetchImpl(COPILOT_TOKEN_URL, {
    headers: {
      Authorization: `token ${apiKey}`,
      'User-Agent': `sleepy-llm-router/${VERSION}`,
    },
  });
  if (!response.ok) {
    throw new Error(`Copilot 토큰 교환 실패: ${response.status} ${response.statusText} (GET copilot_internal/v2/token)`);
  }
  const body = (await response.json()) as { token?: string; expires_at?: number };
  if (!body.token || !body.expires_at) {
    throw new Error('Copilot 토큰 응답에 token 또는 expires_at 필드가 없어요.');
  }
  return { token: body.token, expiresAt: body.expires_at * 1000 };
}

async function getSessionToken(apiKey: string, fetchImpl: FetchLike): Promise<string> {
  if (cachedToken && Date.now() < cachedToken.expiresAt - TOKEN_REFRESH_MARGIN_MS) {
    return cachedToken.token;
  }
  cachedToken = await exchangeToken(apiKey, fetchImpl);
  return cachedToken.token;
}

export async function listCopilotFreeModels(options: { apiKey: string; fetchImpl?: FetchLike }): Promise<OmfmModel[]> {
  const fetchImpl = options.fetchImpl ?? fetch;
  // API 키 유효성 검증을 위해 토큰 교환 시도
  await exchangeToken(options.apiKey, fetchImpl);
  return KNOWN_MODELS.map(normalizeCopilotModel);
}

export async function postCopilotChatCompletion(options: {
  apiKey: string;
  body: unknown;
  fetchImpl?: FetchLike;
}): Promise<Response> {
  const fetchImpl = options.fetchImpl ?? fetch;
  const sessionToken = await getSessionToken(options.apiKey, fetchImpl);
  return fetchImpl(COPILOT_CHAT_COMPLETIONS_URL, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${sessionToken}`,
      'Content-Type': 'application/json',
      'Copilot-Integration-Id': 'vscode-chat',
      'Editor-Version': 'vscode/1.99.0',
      'Editor-Plugin-Version': 'copilot-chat/0.26.7',
      'x-github-api-version': '2025-04-01',
    },
    body: JSON.stringify(options.body),
  });
}

/** 테스트용: 캐시된 토큰을 초기화해요. */
export function resetCopilotTokenCache(): void {
  cachedToken = undefined;
}
