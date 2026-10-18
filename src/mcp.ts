import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { WebStandardStreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/webStandardStreamableHttp.js";
import { z } from "zod";
import { AgentInput, getAgent, listAgents, saveAgent, TOOL_NAMES } from "./agents";
import { enqueue } from "./run";
import { SPECS } from "./tools";
import type { Env } from "./types";

const json = (value: unknown) => ({ content: [{ type: "text" as const, text: JSON.stringify(value, null, 2) }] });
const fail = (text: string) => ({ content: [{ type: "text" as const, text }], isError: true });

/** Stateless MCP over streamable HTTP; the caller is already authenticated by RUN_TOKEN. */
export async function mcp(request: Request, env: Env): Promise<Response> {
  const server = new McpServer({ name: "watchd", version: "1.0.0" });

  server.registerTool(
    "list_agents",
    { description: "List watchd agents with their schedule, budget and last run." },
    async () => {
      const agents = await listAgents(env);
      const { results } = await env.DB.prepare(
        "SELECT agent, id, status, cost, started_at FROM runs WHERE id IN (SELECT MAX(id) FROM runs GROUP BY agent)",
      ).all<{ agent: string }>();
      return json(
        agents.map((a) => ({ ...a, prompt: undefined, last_run: results.find((r) => r.agent === a.name) ?? null })),
      );
    },
  );

  server.registerTool(
    "get_agent",
    { description: "Get one agent's full definition, including its instructions.", inputSchema: { name: z.string() } },
    async ({ name }) => {
      const agent = await getAgent(env, name);
      return agent ? json(agent) : fail(`No agent named ${name}.`);
    },
  );

  const tools = TOOL_NAMES.map((t) => `${t}: ${SPECS[t].description}`).join("\n");
  server.registerTool(
    "save_agent",
    {
      description: `Create or replace an agent. The agent runs an LLM tool loop in a Cloudflare Worker: it may call the enabled tools up to max_steps times, then writes a markdown report. schedule is a five-field UTC cron, or null to run only on demand. budget is the USD ceiling per run. Available tools:\n${tools}`,
      inputSchema: {
        ...AgentInput.shape,
        schedule: AgentInput.shape.schedule.default(null),
        budget: AgentInput.shape.budget.default(0.05),
        model: AgentInput.shape.model.default(null),
        tools: AgentInput.shape.tools.default(["fetch_page", "seen"]),
        max_steps: AgentInput.shape.max_steps.default(8),
        memory: AgentInput.shape.memory.default(true),
        public: AgentInput.shape.public.default(false),
      },
    },
    async (input) => {
      const agent = AgentInput.parse(input);
      await saveAgent(env, agent);
      return json({ saved: agent.name });
    },
  );

  server.registerTool(
    "run_agent",
    {
      description: "Queue an agent to run now. It starts within a minute; poll get_run with the returned id.",
      inputSchema: { name: z.string() },
    },
    async ({ name }) => {
      const agent = await getAgent(env, name);
      return agent ? json(await enqueue(env, agent, Date.now())) : fail(`No agent named ${name}.`);
    },
  );

  server.registerTool(
    "get_run",
    {
      description: "Get a run's status, cost, report and step trace.",
      inputSchema: { id: z.number().int() },
    },
    async ({ id }) => {
      const [run, steps] = await env.DB.batch([
        env.DB.prepare("SELECT id, agent, status, cost, reserved, output, error, started_at, finished_at FROM runs WHERE id = ?").bind(id),
        env.DB.prepare("SELECT tool, substr(input, 1, 300) AS input, cost FROM steps WHERE run_id = ? ORDER BY id").bind(id),
      ]);
      return run.results[0] ? json({ ...run.results[0], steps: steps.results }) : fail(`No run ${id}.`);
    },
  );

  server.registerTool(
    "list_runs",
    {
      description: "List recent runs, newest first, optionally for one agent.",
      inputSchema: { agent: z.string().optional(), limit: z.number().int().min(1).max(50).default(10) },
    },
    async ({ agent, limit }) => {
      const { results } = await env.DB.prepare(
        "SELECT id, agent, status, cost, started_at, substr(output, 1, 300) AS preview, error FROM runs WHERE ?1 IS NULL OR agent = ?1 ORDER BY id DESC LIMIT ?2",
      )
        .bind(agent ?? null, limit)
        .all();
      return json(results);
    },
  );

  const transport = new WebStandardStreamableHTTPServerTransport({ sessionIdGenerator: undefined, enableJsonResponse: true });
  await server.connect(transport);
  return transport.handleRequest(request);
}
