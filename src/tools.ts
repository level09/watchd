import { z } from "zod";
import type { Agent, ToolName } from "./agents";
import { chat } from "./openrouter";
import type { Env } from "./types";

const RESULT_CHARS = 15_000;
const url = { type: "object", properties: { url: { type: "string" } }, required: ["url"], additionalProperties: false };

export const SPECS: Record<ToolName, { description: string; parameters: object }> = {
  fetch_page: {
    description:
      "Fetch a URL with a plain HTTP GET and return its readable text. Fast and free, so try it first. Works for HTML, feeds, JSON and text; not for pages that need JavaScript. For a JSON API that returns a list, pass fields to keep only those keys on each item.",
    parameters: {
      type: "object",
      properties: { url: { type: "string" }, fields: { type: "array", items: { type: "string" }, maxItems: 50 } },
      required: ["url"],
      additionalProperties: false,
    },
  },
  browse: {
    description: "Load a URL in a real browser and return the rendered page as markdown. Use for JavaScript-heavy pages.",
    parameters: url,
  },
  links: { description: "Load a URL in a real browser and return every link on the page.", parameters: url },
  web_search: {
    description: "Search the web. Returns a short answer with source links.",
    parameters: { type: "object", properties: { query: { type: "string" } }, required: ["query"], additionalProperties: false },
  },
  seen: {
    description:
      "Pass stable IDs (URLs, version tags, listing IDs) of the items you are considering. Returns which ones are new; all are remembered for later runs. Report only new items.",
    parameters: {
      type: "object",
      properties: { keys: { type: "array", items: { type: "string" }, maxItems: 100 } },
      required: ["keys"],
      additionalProperties: false,
    },
  },
};

export const toolDefs = (agent: Agent) =>
  agent.tools.map((name) => ({ type: "function", function: { name, ...SPECS[name] } }));

const Url = z.url({ protocol: /^https?$/ });

/** Tool failures go back to the model as text so it can recover; they never fail the run. */
export async function callTool(env: Env, agent: Agent, name: string, rawArgs: string) {
  if (!(agent.tools as string[]).includes(name))
    return { content: `error: tool ${name} is not enabled for this agent`, cost: 0 };
  // A host that echoes the credential back must not get it into the prompt, the trace or the report.
  const key = secret(env, agent);
  const hide = (s: string) => (key ? s.replaceAll(key, "[secret]") : s);
  try {
    const args = JSON.parse(rawArgs || "{}") as Record<string, unknown>;
    const r = await run(env, agent, name as ToolName, args);
    return { content: hide(r.content).slice(0, RESULT_CHARS), cost: r.cost };
  } catch (e) {
    return { content: hide(`error: ${e instanceof Error ? e.message : String(e)}`), cost: 0 };
  }
}

const secret = (env: Env, agent: Agent) => {
  const value = agent.auth && (env as unknown as Record<string, unknown>)[agent.auth.secret];
  return typeof value === "string" && value ? value : undefined;
};

/** Keeps only `fields` on each object of a root array, or of the arrays one level down, so a long API list fits the result cap. */
export function pick(json: unknown, fields: string[]): unknown {
  const item = (v: unknown) =>
    v && typeof v === "object" && !Array.isArray(v)
      ? Object.fromEntries(fields.filter((f) => f in v).map((f) => [f, (v as Record<string, unknown>)[f]]))
      : v;
  if (Array.isArray(json)) return json.map(item);
  if (!json || typeof json !== "object") return json;
  return Object.fromEntries(Object.entries(json).map(([k, v]) => [k, Array.isArray(v) ? v.map(item) : v]));
}

async function run(env: Env, agent: Agent, name: ToolName, args: Record<string, unknown>) {
  switch (name) {
    case "fetch_page": {
      const target = new URL(Url.parse(args.url));
      const fields = z.array(z.string().min(1).max(100)).max(50).optional().parse(args.fields);
      // The model picks URLs and pages can steer it, so the credential goes to the exact host over https and nowhere else.
      const authed = agent.auth !== null && target.protocol === "https:" && target.host === agent.auth.host;
      const key = secret(env, agent);
      if (authed && !key) throw new Error(`secret ${agent.auth?.secret} is not set`);
      const res = await fetch(target, {
        headers: {
          "user-agent": "Mozilla/5.0 (compatible; watchd)",
          accept: "text/html,application/xml;q=0.9,*/*;q=0.8",
          ...(authed && { authorization: `Bearer ${key}` }),
        },
        // A redirect could carry the credential to another host, so the model gets the target back and fetches it itself.
        redirect: authed ? "manual" : "follow",
        signal: AbortSignal.timeout(15_000),
      });
      if (authed && res.status >= 300 && res.status < 400)
        return { content: `HTTP ${res.status}

Redirect to ${res.headers.get("location")}, not followed.`, cost: 0 };
      const body = await head(res);
      if (fields)
        try {
          return { content: `HTTP ${res.status}

${JSON.stringify(pick(JSON.parse(body), fields))}`, cost: 0 };
        } catch {
          // Not JSON (an error page, say): the model gets the text below.
        }
      const type = res.headers.get("content-type") ?? "";
      // Feeds carry escaped HTML inside XML, so they need a second pass.
      const text = /html/.test(type) ? htmlToText(body) : /xml/.test(type) ? htmlToText(htmlToText(body)) : body;
      return { content: `HTTP ${res.status}\n\n${text}`, cost: 0 };
    }
    case "browse":
      return browser(await env.BROWSER.quickAction("markdown", { url: Url.parse(args.url) }));
    case "links":
      return browser(await env.BROWSER.quickAction("links", { url: Url.parse(args.url) }));
    case "web_search": {
      const query = z.string().min(1).max(500).parse(args.query);
      const reply = await chat(env, {
        model: `${env.DEFAULT_MODEL}:online`,
        max_tokens: 1500,
        messages: [{ role: "user", content: `Answer briefly with source links: ${query}` }],
      });
      return { content: reply.message.content ?? "", cost: reply.cost ?? 0.02 };
    }
    case "seen": {
      const keys = [...new Set(z.array(z.string().min(1).max(300)).max(100).parse(args.keys))];
      if (!keys.length) return { content: '{"new":[],"already_seen":[]}', cost: 0 };
      const inserted = await env.DB.batch<{ key: string }>(
        keys.map((k) =>
          env.DB.prepare("INSERT INTO seen (agent, key) VALUES (?, ?) ON CONFLICT DO NOTHING RETURNING key").bind(agent.name, k),
        ),
      );
      const fresh = inserted.flatMap((r) => r.results.map((row) => row.key));
      return { content: JSON.stringify({ new: fresh, already_seen: keys.filter((k) => !fresh.includes(k)) }), cost: 0 };
    }
  }
}

// A link to a huge file must not exhaust the Worker's 128 MB; real pages fit well under this.
const BODY_BYTES = 5_000_000;

async function head(res: Response) {
  const decoder = new TextDecoder();
  let text = "";
  let size = 0;
  for await (const chunk of res.body ?? []) {
    text += decoder.decode(chunk, { stream: true });
    if ((size += chunk.byteLength) >= BODY_BYTES) break;
  }
  return text;
}

async function browser(res: Response) {
  const body = (await res.json()) as { success: boolean; result?: string | string[]; errors?: { message: string }[] };
  if (!body.success) throw new Error(body.errors?.map((e) => e.message).join("; ") || `browser_http_${res.status}`);
  return { content: Array.isArray(body.result) ? body.result.join("\n") : String(body.result ?? ""), cost: 0 };
}

const ENTITIES: Record<string, string> = { nbsp: " ", amp: "&", lt: "<", gt: ">", quot: '"', apos: "'" };

export function htmlToText(html: string): string {
  return html
    .replace(/<(head|script|style|noscript|svg|template)\b[\s\S]*?<\/\1>/gi, "")
    .replace(/<!--[\s\S]*?-->/g, "")
    .replace(/<a\b[^>]*?href=["']([^"']+)["'][^>]*>([\s\S]*?)<\/a>/gi, "$2 ($1)")
    .replace(/<link\b[^>]*?href=["']([^"']+)["'][^>]*>/gi, "\n$1\n")
    .replace(/<\/?(br|p|div|li|h\d|tr|entry|item|title|updated|section|article)\b[^>]*>/gi, "\n")
    .replace(/<[^>]+>/g, "")
    .replace(/&(#\d+|#x[\da-f]+|\w+);/gi, (m, e: string) =>
      e[0] === "#" ? String.fromCodePoint(Number(e[1] === "x" ? `0${e.slice(1)}` : e.slice(1))) : (ENTITIES[e] ?? m),
    )
    .replace(/[ \t\r]+/g, " ")
    .replace(/\s*\n\s*/g, "\n")
    .trim();
}
