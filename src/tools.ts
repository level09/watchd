import { z } from "zod";
import type { Agent, ToolName } from "./agents";
import { chat } from "./openrouter";
import type { Env } from "./types";

const RESULT_CHARS = 15_000;
const url = { type: "object", properties: { url: { type: "string" } }, required: ["url"], additionalProperties: false };

export const SPECS: Record<ToolName, { description: string; parameters: object }> = {
  fetch_page: {
    description:
      "Fetch a URL with a plain HTTP GET and return its readable text. Fast and free, so try it first. Works for HTML, feeds, JSON and text; not for pages that need JavaScript.",
    parameters: url,
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
  try {
    const args = JSON.parse(rawArgs || "{}") as Record<string, unknown>;
    const r = await run(env, agent, name as ToolName, args);
    return { content: r.content.slice(0, RESULT_CHARS), cost: r.cost };
  } catch (e) {
    return { content: `error: ${e instanceof Error ? e.message : String(e)}`, cost: 0 };
  }
}

async function run(env: Env, agent: Agent, name: ToolName, args: Record<string, unknown>) {
  switch (name) {
    case "fetch_page": {
      const res = await fetch(Url.parse(args.url), {
        headers: { "user-agent": "Mozilla/5.0 (compatible; watchd)", accept: "text/html,application/xml;q=0.9,*/*;q=0.8" },
        signal: AbortSignal.timeout(15_000),
      });
      const body = await res.text();
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
