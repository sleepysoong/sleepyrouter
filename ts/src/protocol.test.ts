import { describe, expect, test } from "bun:test";
import {
  anthropicToOpenAI,
  openAIToAnthropic,
  mapStopReason,
  estimateInputTokens,
} from "./protocol.js";

describe("Protocol Transformations", () => {
  test("mapStopReason maps OpenAI finish_reasons to Anthropic stop_reasons", () => {
    expect(mapStopReason("length")).toBe("max_tokens");
    expect(mapStopReason("tool_calls")).toBe("tool_use");
    expect(mapStopReason("function_call")).toBe("tool_use");
    expect(mapStopReason("content_filter")).toBe("refusal");
    expect(mapStopReason("stop")).toBe("end_turn");
    expect(mapStopReason("unknown")).toBe("end_turn");
  });

  test("estimateInputTokens estimates token count based on input size", () => {
    const body = { messages: [{ role: "user", content: "Hello world" }] };
    const tokens = estimateInputTokens(body);
    expect(tokens).toBeGreaterThan(0);
  });

  test("anthropicToOpenAI converts simple text prompt", () => {
    const input = {
      model: "claude-3-5-sonnet",
      system: "You are a helpful AI assistant.",
      messages: [{ role: "user", content: "Hi!" }],
      temperature: 0.7,
      max_tokens: 100,
    };

    const output = anthropicToOpenAI(input, "upstream-model", "openrouter");
    expect(output["model"]).toBe("upstream-model");
    const msgs = output["messages"] as Array<Record<string, unknown>>;
    expect(msgs.length).toBe(2);
    expect(msgs[0]).toEqual({ role: "system", content: "You are a helpful AI assistant." });
    expect(msgs[1]).toEqual({ role: "user", content: "Hi!" });
    expect(output["temperature"]).toBe(0.7);
    expect(output["max_tokens"]).toBe(100);
  });

  test("anthropicToOpenAI converts tool definitions and tool calls", () => {
    const input = {
      model: "claude-3-5-sonnet",
      messages: [
        { role: "user", content: "What is the weather?" },
        {
          role: "assistant",
          content: [
            {
              type: "tool_use",
              id: "tool_123",
              name: "get_weather",
              input: { location: "Seoul" },
            },
          ],
        },
        {
          role: "user",
          content: [
            {
              type: "tool_result",
              tool_use_id: "tool_123",
              content: "Sunny, 25C",
            },
          ],
        },
      ],
      tools: [
        {
          name: "get_weather",
          description: "Get current weather",
          input_schema: {
            type: "object",
            properties: { location: { type: "string" } },
          },
        },
      ],
    };

    const output = anthropicToOpenAI(input, "upstream-model", "openrouter");
    const tools = output["tools"] as Array<Record<string, unknown>>;
    expect(tools.length).toBe(1);
    expect(tools[0]!["function"]).toEqual({
      name: "get_weather",
      description: "Get current weather",
      parameters: {
        type: "object",
        properties: { location: { type: "string" } },
      },
    });

    const msgs = output["messages"] as Array<Record<string, unknown>>;
    expect(msgs[1]!["role"]).toBe("assistant");
    expect(msgs[1]!["tool_calls"]).toEqual([
      {
        id: "tool_123",
        type: "function",
        function: {
          name: "get_weather",
          arguments: '{"location":"Seoul"}',
        },
      },
    ]);
    expect(msgs[2]!["role"]).toBe("tool");
    expect(msgs[2]!["tool_call_id"]).toBe("tool_123");
    expect(msgs[2]!["content"]).toBe("Sunny, 25C");
  });

  test("openAIToAnthropic converts OpenAI response to Anthropic message format", () => {
    const openAIResp = {
      id: "chatcmpl-999",
      model: "gpt-4o",
      choices: [
        {
          index: 0,
          message: {
            role: "assistant",
            content: "Hello there!",
            reasoning_content: "Thought process here",
          },
          finish_reason: "stop",
        },
      ],
      usage: { prompt_tokens: 10, completion_tokens: 5 },
    };

    const anthropicMsg = openAIToAnthropic(openAIResp, "fallback-model");
    expect(anthropicMsg["id"]).toBe("chatcmpl-999");
    expect(anthropicMsg["type"]).toBe("message");
    expect(anthropicMsg["role"]).toBe("assistant");
    expect(anthropicMsg["model"]).toBe("gpt-4o");
    expect(anthropicMsg["stop_reason"]).toBe("end_turn");

    const content = anthropicMsg["content"] as Array<Record<string, unknown>>;
    expect(content.length).toBe(2);
    expect(content[0]).toEqual({ type: "thinking", thinking: "Thought process here" });
    expect(content[1]).toEqual({ type: "text", text: "Hello there!" });
    expect(anthropicMsg["usage"]).toEqual({ input_tokens: 10, output_tokens: 5 });
  });
});
