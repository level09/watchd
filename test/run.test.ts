import { env } from "cloudflare:workers";
import { afterEach, expect, it, vi } from "vitest";
import { runAgent } from "../src/run";
import type { Agent, Env } from "../src/types";

const agent: Agent = {
  name: "scout",
  schedule: "* * * * *",
  budget: 0.06,
  prompt: "Report releases.",
  memory: true,
  sources: ["https://example.test/feed"],
};

function setup(reply: string | Response, cap = "1.00", notifyEmail = "me@example.test") {
  vi.restoreAllMocks();
  const send = vi.fn().mockResolvedValue({ messageId: "m" });
  const fetch = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) =>
    String(input).startsWith("https://example.test")
      ? new Response("<feed>v2 released</feed>")
      : reply instanceof Response
        ? reply
        : Response.json({ choices: [{ message: { content: reply } }], usage: { cost: 0.012 } }),
  );
  return { send, fetch, env: { ...env, DAILY_CAP_USD: cap, EMAIL: { send }, NOTIFY_EMAIL: notifyEmail, EMAIL_FROM: "watchd@example.test" } as Env };
}

const row = (slot: number) =>
  env.DB.prepare("SELECT status, cost, output, error FROM runs WHERE slot = ?").bind(slot).first();
const memory = () =>
  env.DB.prepare("SELECT body FROM memory WHERE agent = 'scout'").first<string>("body");

afterEach(() => vi.restoreAllMocks());

it("runs, settles actual cost, stores memory and emails the output", async () => {
  const t = setup("v2 is out.\n===MEMORY===\n- reported v2");
  const slot = Date.UTC(2026, 9, 1, 7);
  expect(await runAgent(t.env, agent, slot)).toEqual({ status: "ok", cost: 0.012, output: "v2 is out." });
  expect(await row(slot)).toEqual({ status: "ok", cost: 0.012, output: "v2 is out.", error: null });
  expect(await memory()).toBe("- reported v2");
  expect(t.send).toHaveBeenCalledWith(expect.objectContaining({ subject: "[watchd] scout 2026-10-01", text: "v2 is out." }));
  const prompt = JSON.parse(t.fetch.mock.calls[1][1]!.body as string).messages[1].content;
  expect(prompt).toContain('<source url="https://example.test/feed">\n<feed>v2 released</feed>');
});

it("skips a duplicate slot without calling the model", async () => {
  const slot = Date.UTC(2026, 9, 2, 7);
  await runAgent(setup("first").env, agent, slot);
  const t = setup("second");
  expect(await runAgent(t.env, agent, slot)).toEqual({ status: "skipped", reason: "duplicate_slot" });
  expect(t.fetch).not.toHaveBeenCalled();
});

it("refuses a run that would push the day past the cap", async () => {
  const day = Date.UTC(2026, 9, 3, 7);
  expect((await runAgent(setup("ok", "0.10").env, agent, day)).status).toBe("ok"); // settles at 0.012
  expect((await runAgent(setup("ok", "0.10").env, agent, day + 1)).status).toBe("ok"); // 0.024
  const t = setup("ok", "0.07");
  expect(await runAgent(t.env, agent, day + 2)).toEqual({ status: "skipped", reason: "daily_cap" });
  expect(t.fetch).not.toHaveBeenCalled();
});

it("charges the reservation on failure, keeps memory and reports the error", async () => {
  await env.DB.prepare("INSERT OR REPLACE INTO memory (agent, body) VALUES ('scout', 'keep me')").run();
  const t = setup(new Response("upstream down", { status: 502 }));
  const slot = Date.UTC(2026, 9, 4, 7);
  expect(await runAgent(t.env, agent, slot)).toEqual({ status: "error", reason: "openrouter_http_502 upstream down" });
  expect(await row(slot)).toMatchObject({ status: "error", cost: 0.06 });
  expect(await memory()).toBe("keep me");
  expect(t.send).toHaveBeenCalledWith(expect.objectContaining({ subject: "[watchd] scout failed" }));
});

it("stays quiet when the agent has nothing to report", async () => {
  const t = setup("NOTHING\n===MEMORY===\n- nothing new");
  expect((await runAgent(t.env, agent, Date.UTC(2026, 9, 5, 7))).status).toBe("ok");
  expect(t.send).not.toHaveBeenCalled();
});

it("keeps results in D1 without emailing when NOTIFY_EMAIL is unset", async () => {
  const t = setup("v3 is out.", "1.00", "");
  const slot = Date.UTC(2026, 9, 6, 7);
  expect((await runAgent(t.env, agent, slot)).status).toBe("ok");
  expect(await row(slot)).toMatchObject({ status: "ok", output: "v3 is out." });
  expect(t.send).not.toHaveBeenCalled();
});

it("splits on the first marker when the model also closes the memory block", async () => {
  const t = setup("Report.\n===MEMORY===\n- seen v4\n===MEMORY===");
  expect(await runAgent(t.env, agent, Date.UTC(2026, 9, 7, 7))).toMatchObject({ output: "Report." });
  expect(await memory()).toBe("- seen v4");
});
