import type { streamText } from "ai";

export function streamOpenAIAsAnthropic(
  result: Awaited<ReturnType<typeof streamText>>,
  model: string,
): Response {
  const encoder = new TextEncoder();

  const stream = new ReadableStream({
    async start(controller) {
      function writeSSE(event: string, data: unknown) {
        const json = JSON.stringify(data);
        controller.enqueue(
          encoder.encode(`event: ${event}\ndata: ${json}\n\n`),
        );
      }

      writeSSE("message_start", {
        type: "message_start",
        message: {
          id: `msg_${Date.now()}`,
          type: "message",
          role: "assistant",
          content: [],
          model,
          stop_reason: null,
          stop_sequence: null,
          usage: { input_tokens: 0, output_tokens: 0 },
        },
      });

      let blockIndex = 0;
      let textBlockStarted = false;

      try {
        for await (const chunk of result.textStream) {
          if (!textBlockStarted) {
            writeSSE("content_block_start", {
              type: "content_block_start",
              index: blockIndex,
              content_block: { type: "text", text: "" },
            });
            textBlockStarted = true;
          }
          writeSSE("content_block_delta", {
            type: "content_block_delta",
            index: blockIndex,
            delta: { type: "text_delta", text: chunk },
          });
        }

        if (textBlockStarted) {
          writeSSE("content_block_stop", {
            type: "content_block_stop",
            index: blockIndex,
          });
        }
      } catch {
        // Stream error - still send message_stop
      }

      const usage = await Promise.resolve(result.usage);
      writeSSE("message_delta", {
        type: "message_delta",
        delta: { stop_reason: "end_turn", stop_sequence: null },
        usage: { output_tokens: usage.outputTokens ?? 0 },
      });

      writeSSE("message_stop", { type: "message_stop" });
      controller.close();
    },
  });

  return new Response(stream, {
    headers: {
      "Content-Type": "text/event-stream; charset=utf-8",
      "Cache-Control": "no-cache, no-transform",
      Connection: "keep-alive",
    },
  });
}
