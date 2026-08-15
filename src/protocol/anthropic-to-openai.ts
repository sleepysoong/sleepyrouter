import { stringFromUnknown } from "../utils.js";

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
  const args = typeof input === "string" ? input : JSON.stringify(input);

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
