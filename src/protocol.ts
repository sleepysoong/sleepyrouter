// Protocol translation - mirrors internal/protocol/
import { stringFromUnknown } from "./utils.js";

// ---- Anthropic to OpenAI request conversion ----

function toolsToOpenAI(tools: unknown): Record<string, unknown>[] | null {
  if (!Array.isArray(tools) || tools.length === 0) return null;
  const result: Record<string, unknown>[] = [];
  for (const raw of tools) {
    if (!raw || typeof raw !== "object") continue;
    const tool = raw as Record<string, unknown>;
    const name = stringFromUnknown(tool["name"]);
    if (!name) continue;
    const params = tool["input_schema"] ?? { type: "object" };
    result.push({
      type: "function",
      function: { name, description: tool["description"], parameters: params },
    });
  }
  return result.length === 0 ? null : result;
}

function toolChoiceToOpenAI(toolChoice: unknown): unknown {
  if (!toolChoice || typeof toolChoice !== "object") return null;
  const tc = toolChoice as Record<string, unknown>;
  const tcType = stringFromUnknown(tc["type"]);
  if (!tcType) return null;
  switch (tcType) {
    case "none":
      return "none";
    case "auto":
      return "auto";
    case "any":
      return "required";
    case "tool": {
      const name = stringFromUnknown(tc["name"]);
      if (name) return { type: "function", function: { name } };
      break;
    }
  }
  return null;
}

function systemToText(system: unknown): unknown {
  if (system == null) return null;
  if (typeof system === "string") return system || null;
  if (!Array.isArray(system)) return null;
  const parts = system
    .filter(
      (b): b is Record<string, unknown> => b != null && typeof b === "object",
    )
    .map((b) => stringFromUnknown(b["text"]))
    .filter(Boolean);
  return parts.length > 0 ? parts.join("\n") : null;
}

function filterEmpty(items: string[]): string[] {
  return items.filter((s) => s !== "");
}

function sanitizeAnthropicID(value: unknown): string {
  const fallback = `toolu_${Date.now()}`;
  let raw = fallback;
  if (typeof value === "string" && value) raw = value;
  const sanitized = raw.replace(/[^a-zA-Z0-9_-]/g, "_");
  return sanitized || fallback;
}

function openAIContentFromBlocks(
  blocks: Record<string, unknown>[],
): unknown {
  const parts: Record<string, unknown>[] = [];
  for (const block of blocks) {
    if (block["type"] === "text") {
      const text = stringFromUnknown(block["text"]);
      if (text) {
        const part: Record<string, unknown> = { type: "text", text };
        if (block["cache_control"]) part["cache_control"] = block["cache_control"];
        parts.push(part);
      }
    } else if (block["type"] === "image") {
      const url = imageUrlFromAnthropic(block);
      if (url) {
        const part: Record<string, unknown> = {
          type: "image_url",
          image_url: { url },
        };
        if (block["cache_control"]) part["cache_control"] = block["cache_control"];
        parts.push(part);
      }
    }
    // Skip thinking/redacted_thinking blocks
  }
  if (parts.length === 0) return null;
  if (parts.every((p) => p["type"] === "text")) {
    return parts.map((p) => stringFromUnknown(p["text"])).join("\n");
  }
  return parts;
}

function imageUrlFromAnthropic(block: Record<string, unknown>): string {
  const source = block["source"] as Record<string, unknown> | undefined;
  if (!source) return "";
  if (source["type"] === "url") return stringFromUnknown(source["url"]);
  if (source["type"] === "base64") {
    const mediaType = stringFromUnknown(source["media_type"]);
    const data = stringFromUnknown(source["data"]);
    if (mediaType && data) return `data:${mediaType};base64,${data}`;
  }
  return "";
}

function stringifyToolResult(content: unknown): string {
  if (typeof content === "string") return content;
  if (!Array.isArray(content)) return String(content ?? "");
  const parts = content.map((block) => {
    if (typeof block === "string") return block;
    if (block && typeof block === "object" && block["type"] === "text")
      return stringFromUnknown(block["text"]);
    return JSON.stringify(block);
  });
  return parts.join("\n");
}

function toolUseToOpenAICall(
  block: Record<string, unknown>,
  inheritedSig: string,
): Record<string, unknown> {
  let input = block["input"];
  if (input == null) input = {};
  const args =
    typeof input === "string" ? input : JSON.stringify(input);

  let sig = stringFromUnknown(block["thought_signature"]);
  if (!sig) sig = stringFromUnknown(block["signature"]);
  if (!sig) {
    const ef = block["extra_fields"] as Record<string, unknown> | undefined;
    if (ef) sig = stringFromUnknown(ef["thought_signature"]);
  }
  if (!sig) sig = inheritedSig;

  const tc: Record<string, unknown> = {
    id: sanitizeAnthropicID(block["id"]),
    type: "function",
    function: {
      name: stringFromUnknown(block["name"]),
      arguments: args,
    },
  };
  if (sig) {
    tc["thought_signature"] = sig;
    tc["signature"] = sig;
    tc["extra_fields"] = { thought_signature: sig };
  }
  return tc;
}

function anthropicMessagesToOpenAI(
  messages: unknown,
): Record<string, unknown>[] {
  if (!Array.isArray(messages)) return [];
  const out: Record<string, unknown>[] = [];

  for (const raw of messages) {
    if (!raw || typeof raw !== "object") continue;
    const msg = raw as Record<string, unknown>;
    const role = stringFromUnknown(msg["role"]);

    if (typeof msg["content"] === "string") {
      out.push({ role, content: msg["content"] });
      continue;
    }

    const rawBlocks = (msg["content"] as unknown[]) ?? [];
    const blocks = rawBlocks.filter(
      (b): b is Record<string, unknown> => b != null && typeof b === "object",
    );

    const toolUses = blocks.filter((b) => b["type"] === "tool_use");
    const thinkingBlocks = blocks.filter((b) => b["type"] === "thinking");

    if (role === "assistant" && toolUses.length > 0) {
      const nonToolBlocks = blocks.filter((b) => b["type"] !== "tool_use");
      let inheritedSig = "";
      const thinkingTexts: string[] = [];
      for (const tb of thinkingBlocks) {
        const sig =
          stringFromUnknown(tb["signature"]) ||
          stringFromUnknown(tb["thought_signature"]);
        if (sig) inheritedSig = sig;
        const t =
          stringFromUnknown(tb["thinking"]) || stringFromUnknown(tb["text"]);
        if (t) thinkingTexts.push(t);
      }

      const content = openAIContentFromBlocks(nonToolBlocks);
      const contentStr = typeof content === "string" ? content : null;
      const toolCalls = toolUses.map((tu) =>
        toolUseToOpenAICall(tu, inheritedSig),
      );
      const msgMap: Record<string, unknown> = {
        role: "assistant",
        content: contentStr || null,
        tool_calls: toolCalls,
      };
      if (thinkingTexts.length > 0) {
        msgMap["reasoning_content"] = thinkingTexts.join("\n");
      }
      out.push(msgMap);
      continue;
    }

    const pendingContentBlocks: Record<string, unknown>[] = [];
    const flushContent = () => {
      const content = openAIContentFromBlocks(pendingContentBlocks);
      pendingContentBlocks.length = 0;
      if (content != null) out.push({ role, content });
    };

    for (const block of blocks) {
      if (block["type"] === "tool_result") {
        flushContent();
        out.push({
          role: "tool",
          tool_call_id: sanitizeAnthropicID(block["tool_use_id"]),
          content: stringifyToolResult(block["content"]),
        });
      } else if (
        block["type"] === "text" ||
        block["type"] === "image"
      ) {
        pendingContentBlocks.push(block);
      }
    }
    flushContent();
  }
  return out;
}

export function anthropicToOpenAI(
  body: Record<string, unknown>,
  modelID: string,
  _provider: string,
): Record<string, unknown> {
  const messages: Record<string, unknown>[] = [];
  const system = systemToText(body["system"]);
  if (system != null) {
    messages.push({ role: "system", content: system });
  }
  messages.push(...anthropicMessagesToOpenAI(body["messages"]));

  const result: Record<string, unknown> = { model: modelID, messages };
  const tools = toolsToOpenAI(body["tools"]);
  if (tools) result["tools"] = tools;
  const tc = toolChoiceToOpenAI(body["tool_choice"]);
  if (tc != null) result["tool_choice"] = tc;

  // Forward common params
  for (const key of [
    "max_tokens",
    "temperature",
    "top_p",
    "top_k",
    "stream",
  ]) {
    if (body[key] != null) result[key] = body[key];
  }
  if (body["stop_sequences"]) result["stop"] = body["stop_sequences"];

  return result;
}

// ---- OpenAI to Anthropic response conversion ----

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

  // Extract message-level signature
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

  // Usage
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
  const model =
    stringFromUnknown(response["model"]) || fallbackModel;

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

export function estimateInputTokens(body: Record<string, unknown>): number {
  // Rough estimate: 1 token ~= 4 chars
  const json = JSON.stringify(body["messages"] ?? body);
  return Math.ceil(json.length / 4);
}
