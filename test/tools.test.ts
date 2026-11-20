import { expect, it, vi } from "vitest";
import { AgentInput, getAgent } from "../src/agents";
import { callTool, htmlToText, pick } from "../src/tools";
import { agent, setup } from "./helpers";

it("stops reading a page body that never ends", async () => {
  const t = setup([]);
  const chunk = new TextEncoder().encode("<p>x</p>".repeat(8_000));
  const endless = new ReadableStream({ pull: (c) => c.enqueue(chunk) });
  vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(endless, { headers: { "content-type": "text/html" } }));
  const r = await callTool(t.env, await agent("big"), "fetch_page", JSON.stringify({ url: "https://big.test/" }));
  expect(r.content).toMatch(/^HTTP 200\n\nx\nx/);
});

it("turns HTML into readable text with link targets", () => {
  expect(
    htmlToText(
      `<html><head><title>T</title><style>p{}</style></head><body><script>x()</script><h1>Jobs &amp; more</h1><p>Go dev at <a href="https://e.test/1">Acme</a></p><!-- c --><li>&#39;Remote&#x27;</li></body></html>`,
    ),
  ).toBe("Jobs & more\nGo dev at Acme (https://e.test/1)\n'Remote'");
});

it("reads Atom feeds, keeping entry links and unescaping their HTML content", () => {
  const feed = `<?xml version="1.0"?><feed><entry><title>v2.0</title><link rel="alternate" href="https://e.test/v2"/><content type="html">&lt;p&gt;Adds &lt;b&gt;tools&lt;/b&gt;&lt;/p&gt;</content></entry></feed>`;
  expect(htmlToText(htmlToText(feed))).toBe("v2.0\nhttps://e.test/v2\nAdds tools");
});

it("sends the agent's secret to its exact https host and to no other request", async () => {
  const t = setup([], { API_KEY: "s3cret-value" } as object);
  await agent("keyed", { auth: { host: "api.test", secret: "API_KEY" } });
  const a = (await getAgent(t.env, "keyed"))!;
  expect(a.auth).toEqual({ host: "api.test", secret: "API_KEY" });
  const sent = (url: string) => {
    const init = vi.mocked(fetch).mock.calls.find((c) => String(c[0]) === url)![1]!;
    return { auth: new Headers(init.headers).get("authorization"), redirect: init.redirect };
  };
  const get = (url: string) => callTool(t.env, a, "fetch_page", JSON.stringify({ url }));

  // The host redirects elsewhere and echoes the credential: neither leaks.
  const spy = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) =>
    String(input) === "https://api.test/moved"
      ? new Response(null, { status: 302, headers: { location: "https://evil.test/steal" } })
      : new Response("your key is s3cret-value"),
  );
  expect((await get("https://api.test/v1/items")).content).toBe("HTTP 200\n\nyour key is [secret]");
  expect(sent("https://api.test/v1/items")).toEqual({ auth: "Bearer s3cret-value", redirect: "manual" });
  expect((await get("https://api.test/moved")).content).toBe("HTTP 302\n\nRedirect to https://evil.test/steal, not followed.");
  expect(spy).toHaveBeenCalledTimes(2);

  for (const url of [
    "https://evil.test/",
    "http://api.test/",
    "https://api.test.evil.test/",
    "https://sub.api.test/",
    "https://api.test:8443/",
    "https://api.test@evil.test/",
    "https://api.test./",
  ]) {
    await get(url);
    const call = vi.mocked(fetch).mock.calls.at(-1)!;
    expect(new Headers(call[1]!.headers).get("authorization"), url).toBeNull();
  }

  // An unset secret fails the call instead of going out unauthenticated.
  const calls = spy.mock.calls.length;
  expect((await callTool({ ...t.env, API_KEY: undefined } as never, a, "fetch_page", '{"url":"https://api.test/"}')).content).toBe(
    "error: secret API_KEY is not set",
  );
  expect(spy).toHaveBeenCalledTimes(calls);
});

it("refuses to use watchd's own secrets as an agent credential", () => {
  const base = { name: "x", prompt: "p", schedule: null, budget: 0.05, model: null, tools: [], max_steps: 8, memory: false, public: false };
  for (const secret of ["RUN_TOKEN", "OPENROUTER_API_KEY", "lower"])
    expect(AgentInput.safeParse({ ...base, auth: { host: "api.test", secret } }).success, secret).toBe(false);
  expect(AgentInput.safeParse({ ...base, auth: { host: "https://api.test", secret: "API_KEY" } }).success).toBe(false);
  expect(AgentInput.parse(base).auth).toBeNull();
});

it("keeps only the requested fields on JSON list items", async () => {
  const t = setup([]);
  const list = { data: [{ slug: "a", price: 100, noise: "x".repeat(50) }, { slug: "b", extra: 1 }], meta: { page: 1 } };
  vi.spyOn(globalThis, "fetch").mockImplementation(async () => Response.json(list));
  const r = await callTool(t.env, await agent("api"), "fetch_page", JSON.stringify({ url: "https://api.test/", fields: ["slug", "price"] }));
  expect(r.content).toBe('HTTP 200\n\n{"data":[{"slug":"a","price":100},{"slug":"b"}],"meta":{"page":1}}');
  expect(pick([{ a: 1, b: 2 }, 3], ["a"])).toEqual([{ a: 1 }, 3]);
  // A non-JSON reply still comes back as text.
  vi.spyOn(globalThis, "fetch").mockImplementation(async () => new Response("<p>down</p>", { status: 503, headers: { "content-type": "text/html" } }));
  expect((await callTool(t.env, await agent("api"), "fetch_page", '{"url":"https://api.test/","fields":["slug"]}')).content).toBe("HTTP 503\n\ndown");
});
