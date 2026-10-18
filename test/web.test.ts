import { env } from "cloudflare:workers";
import { expect, it } from "vitest";
import type { Agent } from "../src/types";
import { handle } from "../src/web";

const agents: Agent[] = [
  { name: "news", schedule: "0 7 * * *", budget: 0.05, prompt: "p", public: true },
  { name: "costs", schedule: "0 8 * * *", budget: 0.05, prompt: "p" },
];
const req = (path: string, init?: RequestInit) => handle(new Request(`https://w.test${path}`, init), env, agents);

async function signIn() {
  const res = await req("/login", { method: "POST", body: new URLSearchParams({ token: env.RUN_TOKEN }) });
  expect(res.status).toBe(303);
  return res.headers.get("set-cookie")!.split(";")[0];
}

it("shows only the login form to strangers and rejects a wrong token", async () => {
  const home = await (await req("/")).text();
  expect(home).toContain("Sign in");
  expect(home).not.toContain("costs");
  const wrong = await req("/login", { method: "POST", body: new URLSearchParams({ token: "nope" }) });
  expect(wrong.status).toBe(401);
  expect((await req("/run/costs", { method: "POST" })).status).toBe(401);
  expect((await req("/run/costs", { method: "POST", headers: { authorization: "Bearer nope" } })).status).toBe(401);
});

it("signs in with a session cookie and renders agents, spend and escaped output", async () => {
  await env.DB.prepare(
    "INSERT INTO runs (agent, slot, day, status, reserved, cost, output) VALUES ('costs', 1, '2026-01-01', 'ok', 0.05, 0.01, '<script>x</script> https://example.test/a')",
  ).run();
  const cookie = await signIn();
  expect(cookie).toMatch(/^watchd=[0-9a-f]{64}$/);
  const res = await req("/", { headers: { cookie } });
  const html = await res.text();
  expect(res.headers.get("content-security-policy")).toContain("default-src 'none'");
  expect(html).toContain("of $1.00 today");
  expect(html).toContain('<a class="tag" href="/p/news">public</a>');
  expect(html).toContain("&#60;script&#62;x");
  expect(html).not.toContain("<script>x");
  expect(html).toContain('<a href="https://example.test/a"');
});

it("publishes only successful, non-empty reports of public agents, without costs", async () => {
  await env.DB.batch([
    env.DB.prepare("INSERT INTO runs (agent, slot, day, status, reserved, cost, output) VALUES ('news', 1, '2026-01-01', 'ok', 0.05, 0.02, 'Big release')"),
    env.DB.prepare("INSERT INTO runs (agent, slot, day, status, reserved, cost, output) VALUES ('news', 2, '2026-01-01', 'ok', 0.05, 0.02, 'NOTHING')"),
    env.DB.prepare("INSERT INTO runs (agent, slot, day, status, reserved, error) VALUES ('news', 3, '2026-01-01', 'error', 0.05, 'secret failure')"),
  ]);
  const html = await (await req("/p/news")).text();
  expect(html).toContain("Big release");
  expect(html).not.toContain("NOTHING");
  expect(html).not.toContain("secret failure");
  expect(html).not.toContain("$");
  expect((await req("/p/costs")).status).toBe(404);
});

it("refuses to serve anything with a weak RUN_TOKEN", async () => {
  const res = await handle(new Request("https://w.test/p/news"), { ...env, RUN_TOKEN: "short" }, agents);
  expect(res.status).toBe(500);
});
