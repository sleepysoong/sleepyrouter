import type { generateText } from "ai";

export function buildOpenAIResponse(
  result: Awaited<ReturnType<typeof generateText>>,
  model: string,
): Record<string, unknown> {
  const message: Record<string, unknown> = {
    role: "assistant",
    content: result.text || null,
  };
  if (result.reasoning) {
    message["reasoning_content"] = result.reasoning;
  }
  if (result.toolCalls && result.toolCalls.length > 0) {
    message["tool_calls"] = result.toolCalls.map((tc, i) => ({
      index: i,
      id: tc.toolCallId,
      type: "function",
      function: {
        name: tc.toolName,
        arguments: JSON.stringify((tc as any).input ?? (tc as any).args ?? {}),
      },
    }));
  }

  const inputTokens = result.usage.inputTokens ?? 0;
  const outputTokens = result.usage.outputTokens ?? 0;

  return {
    id: `chatcmpl-${Date.now()}`,
    object: "chat.completion",
    created: Math.floor(Date.now() / 1000),
    model,
    choices: [
      {
        index: 0,
        message,
        finish_reason: result.finishReason ?? "stop",
      },
    ],
    usage: {
      prompt_tokens: inputTokens,
      completion_tokens: outputTokens,
      total_tokens: inputTokens + outputTokens,
    },
  };
}

export function buildAnthropicResponse(
  result: Awaited<ReturnType<typeof generateText>>,
  model: string,
): Record<string, unknown> {
  const blocks: Record<string, unknown>[] = [];

  if (result.reasoning) {
    blocks.push({
      type: "thinking",
      thinking: result.reasoning,
    });
  }
  if (result.text) {
    blocks.push({ type: "text", text: result.text });
  }
  if (result.toolCalls) {
    for (const tc of result.toolCalls) {
      blocks.push({
        type: "tool_use",
        id: tc.toolCallId,
        name: tc.toolName,
        input: (tc as any).input ?? (tc as any).args ?? {},
      });
    }
  }

  let stopReason = "end_turn";
  if (result.finishReason === "length") stopReason = "max_tokens";
  else if (result.finishReason === "tool-calls") stopReason = "tool_use";

  return {
    id: `msg_${Date.now()}`,
    type: "message",
    role: "assistant",
    content: blocks,
    model,
    stop_reason: stopReason,
    stop_sequence: null,
    usage: {
      input_tokens: result.usage.inputTokens ?? 0,
      output_tokens: result.usage.outputTokens ?? 0,
    },
  };
}
