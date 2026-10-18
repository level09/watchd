import type { Env } from "./types";

export type ToolCall = { id: string; type: "function"; function: { name: string; arguments: string } };
export type Message =
  | { role: "system" | "user"; content: string }
  | { role: "assistant"; content: string | null; tool_calls?: ToolCall[] }
  | { role: "tool"; tool_call_id: string; content: string };

/** One chat completion. `cost` is undefined when OpenRouter does not report it. */
export async function chat(env: Env, body: Record<string, unknown>) {
  const res = await fetch("https://openrouter.ai/api/v1/chat/completions", {
    method: "POST",
    headers: { authorization: `Bearer ${env.OPENROUTER_API_KEY}`, "content-type": "application/json" },
    signal: AbortSignal.timeout(180_000),
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new Error(`openrouter_http_${res.status} ${(await res.text()).slice(0, 300)}`);
  const data = (await res.json()) as {
    choices?: { message?: { content?: string | null; tool_calls?: ToolCall[] } }[];
    usage?: { cost?: number };
    error?: { message?: string };
  };
  if (data.error) throw new Error(`openrouter_error ${data.error.message}`);
  const m = data.choices?.[0]?.message;
  if (!m) throw new Error("openrouter_empty_reply");
  const message: Message = {
    role: "assistant",
    content: m.content ?? null,
    ...(m.tool_calls?.length ? { tool_calls: m.tool_calls } : {}),
  };
  return { message, cost: data.usage?.cost };
}
