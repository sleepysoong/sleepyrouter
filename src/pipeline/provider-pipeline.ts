import { generateText, streamText } from "ai";
import type { SleepyRouterModel, ProviderAPIKeys } from "../types.js";
import { sourceOf } from "../types.js";
import { defaultProviderRegistry, type ProviderAdapter } from "../providers/index.js";
import { apiKeyFor } from "../config/index.js";
import type { NormalizedAISDKParams } from "./request-pipeline.js";

export interface ProviderExecutionResult {
  adapter: ProviderAdapter;
  model: SleepyRouterModel;
  upstreamModelID: string;
  isStream: boolean;
  generateResult?: Awaited<ReturnType<typeof generateText>>;
  streamResult?: Awaited<ReturnType<typeof streamText>>;
}

export class BaseProviderPipeline {
  async execute(
    model: SleepyRouterModel,
    apiKeys: ProviderAPIKeys,
    params: NormalizedAISDKParams,
    isStream: boolean,
  ): Promise<ProviderExecutionResult> {
    const source = sourceOf(model);
    const adapter = defaultProviderRegistry.get(source);
    if (!adapter) {
      throw new Error(`unsupported provider: ${source}`);
    }

    let apiKey = apiKeyFor(apiKeys, source);
    if (!apiKey) {
      const keyName = adapter.apiKeyEnvVar ?? "OPENROUTER_API_KEY";
      throw new Error(
        `${keyName}가 없어서 ${model.id}을(를) 사용할 수 없어요. 환경변수 또는 .env 파일에 키를 추가하세요.`,
      );
    }

    if (adapter.prepareApiKey) {
      apiKey = await adapter.prepareApiKey(apiKey);
    }

    const upstreamModelID = model.upstreamId || model.id;
    const langModel = adapter.chatModel(upstreamModelID, apiKey);

    if (isStream) {
      const streamResult = await streamText({
        model: langModel,
        maxRetries: 0,
        messages: params.messages,
        system: params.system,
        temperature: params.temperature,
        topP: params.topP,
        maxOutputTokens: params.maxOutputTokens,
        providerOptions: {
          [adapter.name.toLowerCase()]: params.rawBody as any,
        } as any,
      });

      return {
        adapter,
        model,
        upstreamModelID,
        isStream: true,
        streamResult,
      };
    }

    const generateResult = await generateText({
      model: langModel,
      maxRetries: 0,
      messages: params.messages,
      system: params.system,
      temperature: params.temperature,
      topP: params.topP,
      maxOutputTokens: params.maxOutputTokens,
      providerOptions: {
        [adapter.name.toLowerCase()]: params.rawBody as any,
      } as any,
    });

    return {
      adapter,
      model,
      upstreamModelID,
      isStream: false,
      generateResult,
    };
  }
}

export const defaultBaseProviderPipeline = new BaseProviderPipeline();
