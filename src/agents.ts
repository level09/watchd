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
});
export type Agent = z.infer<typeof AgentInput>;

type Row = Omit<Agent, "tools" | "memory" | "public"> & { tools: string; memory: number; public: number };
const fromRow = (r: Row): Agent => ({
  ...r,
  tools: JSON.parse(r.tools),
  memory: r.memory === 1,
  public: r.public === 1,
});
const COLUMNS = "name, prompt, schedule, budget, model, tools, max_steps, memory, public";

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
    `INSERT INTO agents (${COLUMNS}) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
     ON CONFLICT (name) DO UPDATE SET prompt = excluded.prompt, schedule = excluded.schedule,
       budget = excluded.budget, model = excluded.model, tools = excluded.tools, max_steps = excluded.max_steps,
       memory = excluded.memory, public = excluded.public, updated_at = CURRENT_TIMESTAMP`,
  )
    .bind(a.name, a.prompt, a.schedule, a.budget, a.model, JSON.stringify(a.tools), a.max_steps, +a.memory, +a.public)
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
