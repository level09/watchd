import { env } from "cloudflare:workers";
import { expect, it } from "vitest";
import { getAgent } from "../src/agents";
import { handle } from "../src/web";
import { agent } from "./helpers";

const req = (path: string, init?: RequestInit) => handle(new Request(`https://w.test${path}`, init), env);
const form = (fields: Record<string, string | string[]>) => {
  const body = new URLSearchParams();
  for (const [k, v] of Object.entries(fields)) for (const x of [v].flat()) body.append(k, x);
  return body;
};

async function signIn() {
  const res = await req("/login", { method: "POST", body: form({ token: env.RUN_TOKEN }) });
  expect(res.status).toBe(303);
  return res.headers.get("set-cookie")!.split(";")[0];
}

it("shows only the login form to strangers and rejects wrong tokens", async () => {
  await agent("private-one");
  const home = await (await req("/")).text();
  expect(home).toContain("Sign in");
  expect(home).not.toContain("private-one");
  expect((await req("/login", { method: "POST", body: form({ token: "nope" }) })).status).toBe(401);
  expect((await req("/run/private-one", { method: "POST" })).status).toBe(401);
  expect((await req("/agents/new")).status).toBe(401);
  expect((await req("/mcp", { method: "POST", headers: { authorization: "Bearer nope" } })).status).toBe(401);
});

it("creates, validates, runs and deletes an agent from the dashboard", async () => {
  const cookie = await signIn();
  const post = (path: string, fields: Record<string, string | string[]>) =>
    req(path, { method: "POST", headers: { cookie }, body: form(fields) });
  const fields = {
    mode: "create",
    name: "price-watch",
    prompt: "Check the price.",
    schedule: "0 8 * * *",
    budget: "0.03",
    max_steps: "5",
    model: "",
    tools: ["fetch_page", "browse"],
    memory: "on",
  };

  const bad = await post("/agents", { ...fields, schedule: "every day" });
  expect(bad.status).toBe(200);
  expect(await bad.text()).toContain("schedule: five-field cron");

  const saved = await post("/agents", fields);
  expect(saved.headers.get("location")).toBe("/?notice=saved:price-watch");
  expect(await getAgent(env, "price-watch")).toEqual({
    name: "price-watch",
    prompt: "Check the price.",
    schedule: "0 8 * * *",
    budget: 0.03,
    model: null,
    tools: ["fetch_page", "browse"],
    max_steps: 5,
    memory: true,
    public: false,
  });
  expect(await (await post("/agents", fields)).text()).toContain("already exists");

  const queued = await post("/run/price-watch", {});
  expect(queued.headers.get("location")).toBe("/?notice=queued:price-watch");
  const home = await (await req("/?notice=queued:price-watch", { headers: { cookie } })).text();
  expect(home).toContain("Queued <b>price-watch</b>");
  expect(home).toContain('http-equiv="refresh"');

  expect((await post("/agents/price-watch/delete", {})).headers.get("location")).toBe("/?notice=deleted:price-watch");
  expect(await getAgent(env, "price-watch")).toBeNull();
});

it("runs by bearer token for scripts and returns run JSON", async () => {
  const auth = { authorization: `Bearer ${env.RUN_TOKEN}` };
  await agent("scripted", { tools: [] });
  const res = await req("/run/scripted", { method: "POST", headers: auth });
  const queued = (await res.json()) as { status: string; id: number };
  expect(queued.status).toBe("queued");
  expect(await (await req(`/runs/${queued.id}`, { headers: auth })).json()).toMatchObject({ agent: "scripted", status: "queued" });
});

it("renders escaped markdown and publishes only ok reports of public agents", async () => {
  await agent("news", { public: true });
  await env.DB.batch([
    env.DB.prepare("INSERT INTO runs (agent, slot, day, status, reserved, cost, output) VALUES ('news', 1, '2026-01-01', 'ok', 0.05, 0.02, '**Big** <script>x</script>')"),
    env.DB.prepare("INSERT INTO runs (agent, slot, day, status, reserved, cost, output) VALUES ('news', 2, '2026-01-01', 'ok', 0.05, 0.02, 'NOTHING')"),
    env.DB.prepare("INSERT INTO runs (agent, slot, day, status, reserved, error) VALUES ('news', 3, '2026-01-01', 'error', 0.05, 'secret failure')"),
  ]);
  const res = await req("/p/news");
  const html = await res.text();
  expect(res.headers.get("content-security-policy")).toContain("default-src 'none'");
  expect(html).toContain("<strong>Big</strong> &#60;script&#62;");
  expect(html).not.toMatch(/NOTHING|secret failure|\$/);
  await agent("not-public");
  expect((await req("/p/not-public")).status).toBe(404);
});

it("refuses to serve anything with a weak RUN_TOKEN", async () => {
  expect((await handle(new Request("https://w.test/p/news"), { ...env, RUN_TOKEN: "short" })).status).toBe(500);
});
