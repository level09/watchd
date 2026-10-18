import { env } from "cloudflare:workers";
import { expect, it } from "vitest";
import { getAgent } from "../src/agents";
import { handle } from "../src/web";

let id = 0;
async function rpc(method: string, params: object = {}) {
  const res = await handle(
    new Request("https://w.test/mcp", {
      method: "POST",
      headers: {
        authorization: `Bearer ${env.RUN_TOKEN}`,
        "content-type": "application/json",
        accept: "application/json, text/event-stream",
      },
      body: JSON.stringify({ jsonrpc: "2.0", id: ++id, method, params }),
    }),
    env,
  );
  expect(res.status).toBe(200);
  return (await res.json()) as { result: { tools?: { name: string }[]; content?: { text: string }[]; isError?: boolean } };
}

it("lets an MCP client create, queue and inspect agents", async () => {
  const init = await rpc("initialize", { protocolVersion: "2025-06-18", capabilities: {}, clientInfo: { name: "t", version: "1" } });
  expect(init.result).toMatchObject({ serverInfo: { name: "watchd" } });

  const { result } = await rpc("tools/list");
  expect(result.tools!.map((t) => t.name).sort()).toEqual(["get_agent", "get_run", "list_agents", "list_runs", "run_agent", "save_agent"]);

  await rpc("tools/call", { name: "save_agent", arguments: { name: "via-mcp", prompt: "Say hi." } });
  expect(await getAgent(env, "via-mcp")).toMatchObject({ budget: 0.05, tools: ["fetch_page", "seen"], schedule: null });

  const queued = JSON.parse((await rpc("tools/call", { name: "run_agent", arguments: { name: "via-mcp" } })).result.content![0].text);
  expect(queued.status).toBe("queued");
  const got = JSON.parse((await rpc("tools/call", { name: "get_run", arguments: { id: queued.id } })).result.content![0].text);
  expect(got).toMatchObject({ agent: "via-mcp", status: "queued", steps: [] });

  const bad = await rpc("tools/call", { name: "save_agent", arguments: { name: "Bad Name", prompt: "x" } });
  expect(bad.result.isError).toBe(true);
});
