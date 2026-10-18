import { env } from "cloudflare:workers";
import { expect, it } from "vitest";
import { enqueue, execute, tick } from "../src/run";
import { agent, call, final, run, setup, steps } from "./helpers";


it("loops through tools, dedups with seen, settles the summed cost and keeps memory", async () => {
  const a = await agent("jobs");
  const t = setup([
    call("fetch_page", { url: "https://jobs.test/list" }),
    call("seen", { keys: ["https://jobs.test/1", "https://jobs.test/2"] }, "c2"),
    final("**Go dev** https://jobs.test/1\n===MEMORY===\n- reported jobs/1\n===MEMORY==="),
  ]);
  await env.DB.prepare("INSERT INTO seen (agent, key) VALUES ('jobs', 'https://jobs.test/2')").run();
  const q = await enqueue(t.env, a, Date.UTC(2026, 9, 1, 7));
  if (q.status !== "queued") throw new Error("not queued");
  await execute(t.env, q.id);

  expect(await run(q.id)).toEqual({ status: "ok", cost: 0.004, output: "**Go dev** https://jobs.test/1", error: null });
  expect((await steps(q.id)).map((s) => s.tool)).toEqual(["model", "fetch_page", "model", "seen", "model"]);
  // The page came back as readable text, and seen reported only the unseen key.
  const toolMessages = (t.bodies[2].messages as { role: string; content: string }[]).filter((m) => m.role === "tool");
  expect(toolMessages[0].content).toContain("Go dev (https://jobs.test/1)");
  expect(toolMessages[1].content).toBe('{"new":["https://jobs.test/1"],"already_seen":["https://jobs.test/2"]}');
  expect(t.bodies[0].tools).toHaveLength(2);
  expect(await env.DB.prepare("SELECT body FROM memory WHERE agent = 'jobs'").first("body")).toBe("- reported jobs/1");
  expect(t.send).toHaveBeenCalledWith(expect.objectContaining({ subject: "[watchd] jobs 2026-10-01" }));
});

it("refuses a duplicate slot and a run that would break the daily cap", async () => {
  const a = await agent("capped", { budget: 0.6 });
  const t = setup([], { DAILY_CAP_USD: "1.00" });
  const slot = Date.UTC(2026, 9, 2, 7);
  expect((await enqueue(t.env, a, slot)).status).toBe("queued");
  expect(await enqueue(t.env, a, slot)).toEqual({ status: "skipped", reason: "duplicate_slot" });
  expect(await enqueue(t.env, a, slot + 1)).toEqual({ status: "skipped", reason: "daily_cap" });
});

it("stops when the run budget is spent", async () => {
  const a = await agent("spender", { budget: 0.01 });
  const t = setup([call("fetch_page", { url: "https://jobs.test/x" }, "c1", 0.02)]);
  const q = await enqueue(t.env, a, Date.UTC(2026, 9, 3, 7));
  if (q.status !== "queued") throw new Error("not queued");
  await execute(t.env, q.id);
  expect(await run(q.id)).toMatchObject({ status: "error", error: "budget_exhausted", cost: 0.02 });
  expect(t.send).toHaveBeenCalledWith(expect.objectContaining({ subject: "[watchd] spender failed" }));
});

it("forces a final answer at the step limit and reports disabled tools back to the model", async () => {
  const a = await agent("limited", { max_steps: 2, tools: ["fetch_page"] });
  const t = setup([call("web_search", { query: "go jobs" }), final("Done.")]);
  const q = await enqueue(t.env, a, Date.UTC(2026, 9, 4, 7));
  if (q.status !== "queued") throw new Error("not queued");
  await execute(t.env, q.id);
  expect(await run(q.id)).toMatchObject({ status: "ok", output: "Done." });
  expect(t.bodies[1].tool_choice).toBe("none");
  const tool = (t.bodies[1].messages as { role: string; content: string }[]).find((m) => m.role === "tool");
  expect(tool?.content).toBe("error: tool web_search is not enabled for this agent");
});

it("charges the reservation on provider failure and keeps old memory", async () => {
  const a = await agent("flaky");
  await env.DB.prepare("INSERT OR REPLACE INTO memory (agent, body) VALUES ('flaky', 'keep me')").run();
  const t = setup([new Response("upstream down", { status: 502 })]);
  const q = await enqueue(t.env, a, Date.UTC(2026, 9, 5, 7));
  if (q.status !== "queued") throw new Error("not queued");
  await execute(t.env, q.id);
  expect(await run(q.id)).toMatchObject({ status: "error", error: "openrouter_http_502 upstream down", cost: 0.05 });
  expect(await env.DB.prepare("SELECT body FROM memory WHERE agent = 'flaky'").first("body")).toBe("keep me");
});

it("claims a run once, stays quiet on NOTHING, and keeps a run ok when email fails", async () => {
  const a = await agent("quiet", { tools: [] });
  const t = setup([final("NOTHING")]);
  t.send.mockRejectedValue(new Error("smtp down"));
  const q = await enqueue(t.env, a, Date.UTC(2026, 9, 6, 7));
  if (q.status !== "queued") throw new Error("not queued");
  await Promise.all([execute(t.env, q.id), execute(t.env, q.id)]);
  expect(t.bodies).toHaveLength(1);
  expect(t.bodies[0].tools).toBeUndefined();
  expect(await run(q.id)).toMatchObject({ status: "ok", output: "NOTHING" });
  expect(t.send).not.toHaveBeenCalled();

  const t2 = setup([final("Report.")]);
  t2.send.mockRejectedValue(new Error("smtp down"));
  const q2 = await enqueue(t2.env, a, Date.UTC(2026, 9, 6, 8));
  if (q2.status !== "queued") throw new Error("not queued");
  await execute(t2.env, q2.id);
  expect(await run(q2.id)).toMatchObject({ status: "ok", output: "Report." });
});

it("tick enqueues due agents, runs the queue and retires stuck runs", async () => {
  await env.DB.prepare("UPDATE agents SET schedule = NULL").run();
  await env.DB.prepare("UPDATE runs SET status = 'error' WHERE status = 'queued'").run();
  await agent("daily", { schedule: "0 7 * * *", tools: [] });
  await agent("other", { schedule: "0 9 * * *", tools: [] });
  await env.DB.prepare(
    "INSERT INTO runs (agent, slot, day, status, reserved, started_at) VALUES ('daily', 1, '2026-10-07', 'running', 0.05, datetime('now', '-30 minutes'))",
  ).run();
  const t = setup([final("Morning report.")]);
  await tick(t.env, new Date(Date.UTC(2026, 9, 7, 7, 0)));
  const rows = await env.DB.prepare("SELECT agent, status, error, cost FROM runs WHERE day = '2026-10-07' ORDER BY id").all();
  expect(rows.results).toEqual([
    { agent: "daily", status: "error", error: "timed_out", cost: 0.05 },
    { agent: "daily", status: "ok", error: null, cost: 0.002 },
  ]);
});
