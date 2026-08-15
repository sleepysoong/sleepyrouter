export function estimateInputTokens(body: Record<string, unknown>): number {
  const json = JSON.stringify(body["messages"] ?? body);
  return Math.max(1, Math.ceil(json.length / 4));
}
