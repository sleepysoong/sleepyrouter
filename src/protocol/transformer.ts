import { anthropicToOpenAI } from "./anthropic-to-openai.js";
import { openAIToAnthropic } from "./openai-to-anthropic.js";

export interface ProtocolTransformer {
  transformRequest(
    body: Record<string, unknown>,
    modelID: string,
    providerName: string,
  ): Record<string, unknown>;
  transformResponse(
    response: Record<string, unknown>,
    fallbackModel: string,
  ): Record<string, unknown>;
}

export class AnthropicToOpenAITransformer implements ProtocolTransformer {
  transformRequest(
    body: Record<string, unknown>,
    modelID: string,
    providerName: string,
  ): Record<string, unknown> {
    return anthropicToOpenAI(body, modelID, providerName);
  }

  transformResponse(
    response: Record<string, unknown>,
    fallbackModel: string,
  ): Record<string, unknown> {
    return openAIToAnthropic(response, fallbackModel);
  }
}

const OPENAI_CHAT_COMPLETIONS_FIELDS = new Set([
  "messages",
  "model",
  "frequency_penalty",
  "max_tokens",
  "n",
  "presence_penalty",
  "response_format",
  "seed",
  "stop",
  "stream",
  "temperature",
  "top_p",
  "tools",
  "tool_choice",
  "parallel_tool_calls",
  "user",
  "logprobs",
  "top_logprobs",
  "reasoning_effort",
  "reasoning",
  "stream_options",
]);

export class OpenAIIdentityTransformer implements ProtocolTransformer {
  transformRequest(
    body: Record<string, unknown>,
    modelID: string,
    _providerName: string,
  ): Record<string, unknown> {
    const requestBody: Record<string, unknown> = {};
    for (const [key, value] of Object.entries(body)) {
      if (OPENAI_CHAT_COMPLETIONS_FIELDS.has(key)) {
        requestBody[key] = value;
      }
    }
    requestBody["model"] = modelID;
    return requestBody;
  }

  transformResponse(
    response: Record<string, unknown>,
    _fallbackModel: string,
  ): Record<string, unknown> {
    return response;
  }
}

export class ProtocolTransformerRegistry {
  private transformers = new Map<string, ProtocolTransformer>();

  constructor() {
    this.transformers.set("anthropic", new AnthropicToOpenAITransformer());
    this.transformers.set("openai", new OpenAIIdentityTransformer());
  }

  register(apiType: string, transformer: ProtocolTransformer): this {
    this.transformers.set(apiType, transformer);
    return this;
  }

  get(apiType: string): ProtocolTransformer {
    return this.transformers.get(apiType) ?? new OpenAIIdentityTransformer();
  }
}

export const defaultProtocolTransformerRegistry = new ProtocolTransformerRegistry();
