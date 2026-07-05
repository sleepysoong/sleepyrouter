import { afterEach, describe, expect, it } from 'vitest';
import {
  COPILOT_CHAT_COMPLETIONS_URL,
  COPILOT_TOKEN_URL,
  listCopilotFreeModels,
  postCopilotChatCompletion,
  resetCopilotTokenCache,
} from '../src/providers/copilot.js';

afterEach(() => resetCopilotTokenCache());

describe('Copilot model provider', () => {
  it('lists known models with copilot/ prefix after successful token exchange', async () => {
    const fetchImpl = (async (input: string | URL | Request) => {
      const url = String(input);
      if (url === COPILOT_TOKEN_URL) {
        return Response.json({ token: 'session-token', expires_at: Math.floor(Date.now() / 1000) + 3600 });
      }
      return Response.json({});
    }) as typeof fetch;

    const models = await listCopilotFreeModels({ apiKey: 'gh-token', fetchImpl });

    expect(models.length).toBeGreaterThan(0);
    expect(models.every((m) => m.id.startsWith('copilot/'))).toBe(true);
    expect(models.every((m) => m.source === 'copilot')).toBe(true);
    expect(models.every((m) => m.provider === 'copilot')).toBe(true);
    expect(models.map((m) => m.id)).toContain('copilot/gpt-4o');
    expect(models.map((m) => m.id)).toContain('copilot/claude-sonnet-4');
  });

  it('throws when token exchange fails', async () => {
    const fetchImpl = (async () => new Response('Unauthorized', { status: 401 })) as typeof fetch;

    await expect(listCopilotFreeModels({ apiKey: 'bad-key', fetchImpl })).rejects.toThrow('Copilot 토큰 교환 실패');
  });

  it('posts chat completion with exchanged session token', async () => {
    const requests: Array<{ url: string; headers: Record<string, string>; body: unknown }> = [];
    const fetchImpl = (async (input: string | URL | Request, init?: RequestInit) => {
      const url = String(input);
      if (url === COPILOT_TOKEN_URL) {
        return Response.json({ token: 'copilot-session-xyz', expires_at: Math.floor(Date.now() / 1000) + 3600 });
      }
      if (url === COPILOT_CHAT_COMPLETIONS_URL) {
        requests.push({
          url,
          headers: Object.fromEntries(new Headers(init?.headers as HeadersInit).entries()),
          body: JSON.parse(init?.body as string),
        });
        return Response.json({ choices: [{ message: { content: 'ok' } }] });
      }
      return new Response('Not found', { status: 404 });
    }) as typeof fetch;

    const result = await postCopilotChatCompletion({
      apiKey: 'gh-token',
      body: { model: 'gpt-4o', messages: [{ role: 'user', content: 'Hi' }] },
      fetchImpl,
    });

    expect(result.ok).toBe(true);
    expect(requests).toHaveLength(1);
    expect(requests[0]!.headers.authorization).toBe('Bearer copilot-session-xyz');
    expect(requests[0]!.headers['copilot-integration-id']).toBe('vscode-chat');
    expect(requests[0]!.body).toMatchObject({ model: 'gpt-4o' });
  });

  it('reuses cached token within expiry window', async () => {
    let tokenCallCount = 0;
    const fetchImpl = (async (input: string | URL | Request) => {
      const url = String(input);
      if (url === COPILOT_TOKEN_URL) {
        tokenCallCount++;
        return Response.json({ token: `token-${tokenCallCount}`, expires_at: Math.floor(Date.now() / 1000) + 3600 });
      }
      return Response.json({ choices: [{ message: { content: 'ok' } }] });
    }) as typeof fetch;

    await postCopilotChatCompletion({ apiKey: 'gh-token', body: { model: 'gpt-4o', messages: [] }, fetchImpl });
    await postCopilotChatCompletion({ apiKey: 'gh-token', body: { model: 'gpt-4o', messages: [] }, fetchImpl });

    // 토큰 교환은 1번만 발생해야 해요 (캐시 사용)
    expect(tokenCallCount).toBe(1);
  });

  it('normalizes model IDs with copilot/ prefix and usageId', async () => {
    const fetchImpl = (async (input: string | URL | Request) => {
      if (String(input) === COPILOT_TOKEN_URL) {
        return Response.json({ token: 'tok', expires_at: Math.floor(Date.now() / 1000) + 3600 });
      }
      return Response.json({});
    }) as typeof fetch;

    const models = await listCopilotFreeModels({ apiKey: 'gh-token', fetchImpl });
    const gpt4o = models.find((m) => m.id === 'copilot/gpt-4o');

    expect(gpt4o).toMatchObject({
      id: 'copilot/gpt-4o',
      upstreamId: 'gpt-4o',
      name: 'GPT-4o',
      provider: 'copilot',
      source: 'copilot',
      usageId: 'copilot/gpt-4o',
      contextLength: 128_000,
    });
  });
});
