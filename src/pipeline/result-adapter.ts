import type { ProviderExecutionResult } from "./provider-pipeline.js";
import { buildOpenAIResponse, buildAnthropicResponse } from "../handlers/response-builder.js";
import { streamOpenAIAsAnthropic } from "../handlers/stream-converter.js";

export interface ResultAdapter {
  adaptGenerateResult(execResult: ProviderExecutionResult): Response;
  adaptStreamResult(execResult: ProviderExecutionResult): Response;
}

export class OpenAIResultAdapter implements ResultAdapter {
  adaptGenerateResult(execResult: ProviderExecutionResult): Response {
    if (!execResult.generateResult) {
      throw new Error("Missing generateResult in ProviderExecutionResult");
    }
    const json = buildOpenAIResponse(
      execResult.generateResult,
      execResult.upstreamModelID,
    );
    return Response.json(json);
  }

  adaptStreamResult(execResult: ProviderExecutionResult): Response {
    if (!execResult.streamResult) {
      throw new Error("Missing streamResult in ProviderExecutionResult");
    }
    return execResult.streamResult.toTextStreamResponse();
  }
}

export class AnthropicResultAdapter implements ResultAdapter {
  adaptGenerateResult(execResult: ProviderExecutionResult): Response {
    if (!execResult.generateResult) {
      throw new Error("Missing generateResult in ProviderExecutionResult");
    }
    const json = buildAnthropicResponse(
      execResult.generateResult,
      execResult.upstreamModelID,
    );
    return Response.json(json);
  }

  adaptStreamResult(execResult: ProviderExecutionResult): Response {
    if (!execResult.streamResult) {
      throw new Error("Missing streamResult in ProviderExecutionResult");
    }
    return streamOpenAIAsAnthropic(
      execResult.streamResult,
      execResult.upstreamModelID,
    );
  }
}

export class ResultAdapterRegistry {
  private adapters = new Map<string, ResultAdapter>();

  constructor() {
    this.adapters.set("openai", new OpenAIResultAdapter());
    this.adapters.set("anthropic", new AnthropicResultAdapter());
  }

  register(apiType: string, adapter: ResultAdapter): this {
    this.adapters.set(apiType, adapter);
    return this;
  }

  get(apiType: string): ResultAdapter {
    return this.adapters.get(apiType) ?? new OpenAIResultAdapter();
  }
}

export const defaultResultAdapterRegistry = new ResultAdapterRegistry();
