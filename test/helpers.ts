import { env } from "cloudflare:workers";
import { vi } from "vitest";
import { type Agent, saveAgent } from "../src/agents";
import type { Env } from "../src/types";

export const call = (name: string, args: unknown, id = "c1", cost = 0.001) => ({
  choices: [{ message: { content: null, tool_calls: [{ id, type: "function", function: { name, arguments: JSON.stringify(args) } }] } }],
  usage: { cost },
});
export const final = (content: string, cost = 0.002) => ({ choices: [{ message: { content } }], usage: { cost } });

/** Queue OpenRouter replies; every other URL serves a small HTML page. */
export function setup(replies: (object | Response)[], over: Partial<Env> = {}) {
  vi.restoreAllMocks();
  vi.spyOn(scheduler, "wait").mockResolvedValue(undefined);
  const send = vi.fn().mockResolvedValue({ messageId: "m" });
  const bodies: Record<string, unknown>[] = [];
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    if (String(input).startsWith("https://openrouter.ai")) {
      bodies.push(JSON.parse(init!.body as string));
      const reply = replies.shift();
      if (!reply) throw new Error("no reply queued");
      return reply instanceof Response ? reply : Response.json(reply);
    }
    return new Response(`<html><head><title>x</title></head><body><h1>Jobs</h1><a href="https://jobs.test/1">Go dev</a></body></html>`, {
      headers: { "content-type": "text/html" },
    });
  });
  const testEnv = { ...env, EMAIL: { send }, NOTIFY_EMAIL: "me@example.test", EMAIL_FROM: "w@example.test", ...over } as Env;
  return { send, bodies, env: testEnv };
}

export async function agent(name: string, over: Partial<Agent> = {}): Promise<Agent> {
  const a: Agent = {
    name,
    prompt: "Find new Go jobs.",
    schedule: null,
    budget: 0.05,
    model: null,
    tools: ["fetch_page", "seen"],
    max_steps: 8,
    memory: true,
    public: false,
    ...over,
  };
  await saveAgent(env, a);
  return a;
}

export const run = (id: number) =>
  env.DB.prepare("SELECT status, cost, output, error FROM runs WHERE id = ?").bind(id).first();
export const steps = (id: number) =>
  env.DB.prepare("SELECT tool, input, cost FROM steps WHERE run_id = ? ORDER BY id").bind(id).all().then((r) => r.results);
