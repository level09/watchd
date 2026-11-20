import { z } from "zod";
import { cron } from "./cron";
import type { Env } from "./types";

export const TOOL_NAMES = ["fetch_page", "browse", "links", "web_search", "seen"] as const;
export type ToolName = (typeof TOOL_NAMES)[number];

const validCron = (s: string) => {
  try {
    cron(s);
    return true;
  } catch {
    return false;
  }
};

// The secrets watchd itself runs on can never be pointed at a host an agent names. One exception: the OpenRouter
// key may go back to OpenRouter itself, for its public datasets. fetch_page is GET only, so there it can read
// (including account figures) but cannot spend credit. Do not widen this to other hosts or to RUN_TOKEN.
const reserved = (a: { host: string; secret: string }) =>
  a.secret === "RUN_TOKEN" || (a.secret === "OPENROUTER_API_KEY" && a.host !== "openrouter.ai");

export const AgentInput = z.strictObject({
  name: z.string().regex(/^[a-z0-9][a-z0-9-]{0,39}$/, "lowercase letters, digits and dashes, up to 40"),
  prompt: z.string().trim().min(1).max(20_000),
  schedule: z.string().trim().max(100).refine(validCron, "five-field cron, e.g. 0 7 * * 1").nullable(),
  budget: z.number().positive().max(5),
  model: z.string().trim().min(1).max(100).nullable(),
  tools: z.array(z.enum(TOOL_NAMES)).max(TOOL_NAMES.length),
  max_steps: z.number().int().min(1).max(30),
  memory: z.boolean(),
  public: z.boolean(),
  auth: z
    .strictObject({
      host: z.string().regex(/^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?$/, "a bare hostname, e.g. api.example.com"),
      secret: z.string().regex(/^[A-Z][A-Z0-9_]{0,63}$/, "the name of a Worker secret, e.g. EXAMPLE_API_KEY"),
    })
    .refine((a) => !reserved(a), { path: ["secret"], message: "this secret is reserved for watchd" })
    .nullable()
    .default(null),
  reason_model: z.string().trim().min(1).max(100).nullable().default(null),
});
export type Agent = z.infer<typeof AgentInput>;

type Row = Omit<Agent, "tools" | "memory" | "public" | "auth"> & { tools: string; memory: number; public: number; auth: string | null };
const fromRow = (r: Row): Agent => ({
  ...r,
  tools: JSON.parse(r.tools),
  memory: r.memory === 1,
  public: r.public === 1,
  auth: r.auth ? JSON.parse(r.auth) : null,
});
const COLUMNS = "name, prompt, schedule, budget, model, tools, max_steps, memory, public, auth, reason_model";

export async function listAgents(env: Env): Promise<Agent[]> {
  const { results } = await env.DB.prepare(`SELECT ${COLUMNS} FROM agents ORDER BY name`).all<Row>();
  return results.map(fromRow);
}

export async function getAgent(env: Env, name: string): Promise<Agent | null> {
  const row = await env.DB.prepare(`SELECT ${COLUMNS} FROM agents WHERE name = ?`).bind(name).first<Row>();
  return row && fromRow(row);
}

export async function saveAgent(env: Env, a: Agent) {
  await env.DB.prepare(
    `INSERT INTO agents (${COLUMNS}) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
     ON CONFLICT (name) DO UPDATE SET prompt = excluded.prompt, schedule = excluded.schedule,
       budget = excluded.budget, model = excluded.model, tools = excluded.tools, max_steps = excluded.max_steps,
       memory = excluded.memory, public = excluded.public, auth = excluded.auth, reason_model = excluded.reason_model, updated_at = CURRENT_TIMESTAMP`,
  )
    .bind(
      a.name,
      a.prompt,
      a.schedule,
      a.budget,
      a.model,
      JSON.stringify(a.tools),
      a.max_steps,
      +a.memory,
      +a.public,
      a.auth && JSON.stringify(a.auth),
      a.reason_model,
    )
    .run();
}

// Runs stay as history; the agent's notes and seen keys go with it.
export async function deleteAgent(env: Env, name: string) {
  await env.DB.batch(
    ["agents", "memory", "seen"].map((t) =>
      env.DB.prepare(`DELETE FROM ${t} WHERE ${t === "agents" ? "name" : "agent"} = ?`).bind(name),
    ),
  );
}
