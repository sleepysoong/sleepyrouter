import { defaultProtocolTransformerRegistry } from "../protocol/index.js";
import type { ApiType } from "../handlers/types.js";

export interface NormalizedAISDKParams {
  messages: any;
  system?: string;
  temperature?: number;
  topP?: number;
  maxOutputTokens?: number;
  rawBody: Record<string, unknown>;
}

export class RequestPipeline {
  normalize(
    body: Record<string, unknown>,
    modelID: string,
    providerName: string,
    apiType: ApiType,
  ): NormalizedAISDKParams {
    const transformer = defaultProtocolTransformerRegistry.get(apiType);
    const requestBody = transformer.transformRequest(body, modelID, providerName);

    return {
      messages: requestBody["messages"] as any,
      system: requestBody["system"] as string | undefined,
      temperature: requestBody["temperature"] as number | undefined,
      topP: requestBody["top_p"] as number | undefined,
      maxOutputTokens: (requestBody["max_tokens"] as number) || undefined,
      rawBody: requestBody,
    };
  }
}

export const defaultRequestPipeline = new RequestPipeline();
