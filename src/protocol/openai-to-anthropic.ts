import { stringFromUnknown } from "../utils.js";

export function mapStopReason(reason: unknown): string {
  const s = typeof reason === "string" ? reason : String(reason ?? "");
  switch (s) {
    case "length":
      return "max_tokens";
    case "tool_calls":
    case "function_call":
      return "tool_use";
    case "content_filter":
      return "refusal";
    case "pause_turn":
      return "pause_turn";
    case "model_context_window_exceeded":
      return "model_context_window_exceeded";
    default:
      return "end_turn";
  }
}

function filterEmpty(items: string[]): string[] {
  return items.filter((s) => s !== "");
}

const ANTHROPIC_ID_SANITIZER = /[^a-zA-Z0-9_-]/g;

function sanitizeAnthropicID(value: unknown): string {
  const fallback = `toolu_${Date.now()}`;
  let raw = fallback;
  if (typeof value === "string" && value) raw = value;
  const sanitized = raw.replace(ANTHROPIC_ID_SANITIZER, "_");
  return sanitized || fallback;
}

function contentFromOpenAI(content: unknown): string {
  if (typeof content === "string") return content;
  if (!Array.isArray(content)) return "";
  return filterEmpty(
    content.map((part) => {
      if (typeof part === "string") return part;
      if (part && typeof part === "object") {
        if (part["type"] === "text") return stringFromUnknown(part["text"]);
        if (part["text"]) return stringFromUnknown(part["text"]);
      }
      return "";
    }),
  ).join("\n");
}

function parseToolArguments(value: unknown): Record<string, unknown> {
  if (value && typeof value === "object" && !Array.isArray(value))
    return value as Record<string, unknown>;
  if (typeof value !== "string" || !value.trim()) return {};
  try {
    const parsed = JSON.parse(value);
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed))
      return parsed;
  } catch {}
  return {};
}

function extractSignatureFromToolCall(
  tc: Record<string, unknown>,
): string {
  for (const key of ["thought_signature", "signature"]) {
    const sig = stringFromUnknown(tc[key]);
    if (sig) return sig;
  }
  const ef = tc["extra_fields"] as Record<string, unknown> | undefined;
  if (ef) {
    for (const key of ["thought_signature", "signature"]) {
      const sig = stringFromUnknown(ef[key]);
      if (sig) return sig;
    }
  }
  const fn = tc["function"] as Record<string, unknown> | undefined;
  if (fn) {
    for (const key of ["thought_signature", "signature"]) {
      const sig = stringFromUnknown(fn[key]);
      if (sig) return sig;
    }
  }
  const ec = tc["extra_content"] as Record<string, unknown> | undefined;
  if (ec) {
    const g = ec["google"] as Record<string, unknown> | undefined;
    if (g) {
      const sig = stringFromUnknown(g["thought_signature"]);
      if (sig) return sig;
    }
  }
  return "";
}

export function openAIToAnthropic(
  response: Record<string, unknown>,
  fallbackModel: string,
): Record<string, unknown> {
  const choices = (response["choices"] as unknown[]) ?? [];
  const choice = ((choices[0] as Record<string, unknown>) ?? {}) as Record<
    string,
    unknown
  >;
  const message = ((choice["message"] as Record<string, unknown>) ?? {}) as Record<
    string,
    unknown
  >;

  let contentVal = message["content"] ?? choice["text"] ?? message["refusal"] ?? "";
  const content = contentFromOpenAI(contentVal);

  let reasoningText =
    stringFromUnknown(message["reasoning_content"]) ||
    stringFromUnknown(message["reasoning"]) ||
    stringFromUnknown(message["thinking"]) ||
    stringFromUnknown(message["thought"]);

  let msgSig =
    stringFromUnknown(message["thought_signature"]) ||
    stringFromUnknown(message["signature"]);
  if (!msgSig) {
    const ef = message["extra_fields"] as Record<string, unknown> | undefined;
    if (ef) msgSig = stringFromUnknown(ef["thought_signature"]);
  }
  if (!msgSig) {
    const ec = message["extra_content"] as Record<string, unknown> | undefined;
    if (ec) {
      const g = ec["google"] as Record<string, unknown> | undefined;
      if (g) msgSig = stringFromUnknown(g["thought_signature"]);
    }
  }

  const toolCalls = ((message["tool_calls"] as unknown[]) ?? []).slice();
  const fc = message["function_call"] as Record<string, unknown> | undefined;
  if (fc) {
    toolCalls.push({
      id: `toolu_${Date.now()}`,
      type: "function",
      function: fc,
    });
  }

  if (!msgSig) {
    for (const raw of toolCalls) {
      if (raw && typeof raw === "object") {
        const sig = extractSignatureFromToolCall(raw as Record<string, unknown>);
        if (sig) {
          msgSig = sig;
          break;
        }
      }
    }
  }

  const blocks: Record<string, unknown>[] = [];
  if (reasoningText || msgSig) {
    const tb: Record<string, unknown> = {
      type: "thinking",
      thinking: reasoningText || "",
    };
    if (msgSig) tb["signature"] = msgSig;
    blocks.push(tb);
  }
  if (content) {
    blocks.push({ type: "text", text: content });
  }

  for (const raw of toolCalls) {
    if (!raw || typeof raw !== "object") continue;
    const tc = raw as Record<string, unknown>;
    const tcType = stringFromUnknown(tc["type"]);
    if (tcType && tcType !== "function") continue;
    const fn = (tc["function"] as Record<string, unknown>) ?? {};
    const sig = extractSignatureFromToolCall(tc) || msgSig;

    const tu: Record<string, unknown> = {
      type: "tool_use",
      id: sanitizeAnthropicID(tc["id"]),
      name: stringFromUnknown(fn["name"]),
      input: parseToolArguments(fn["arguments"]),
    };
    if (sig) {
      tu["thought_signature"] = sig;
      tu["signature"] = sig;
      tu["extra_fields"] = { thought_signature: sig };
    }
    blocks.push(tu);
  }

  const usage = (response["usage"] as Record<string, unknown>) ?? {};
  const inputTokens =
    (usage["prompt_tokens"] as number) ??
    (usage["input_tokens"] as number) ??
    0;
  const outputTokens =
    (usage["completion_tokens"] as number) ??
    (usage["output_tokens"] as number) ??
    0;

  const stopReason = mapStopReason(choice["finish_reason"]);
  const model = stringFromUnknown(response["model"]) || fallbackModel;

  return {
    id: stringFromUnknown(response["id"]) || `msg_${Date.now()}`,
    type: "message",
    role: "assistant",
    content: blocks,
    model,
    stop_reason: stopReason,
    stop_sequence: null,
    usage: { input_tokens: inputTokens, output_tokens: outputTokens },
  };
}
