import { timingSafeEqual } from "node:crypto";
import { type Agent, AgentInput, deleteAgent, getAgent, listAgents, saveAgent, TOOL_NAMES } from "./agents";
import { esc, markdown } from "./markdown";
import { mcp } from "./mcp";
import { enqueue } from "./run";
import { SPECS } from "./tools";
import type { Env } from "./types";

const HTML_HEADERS = {
  "content-type": "text/html; charset=utf-8",
  "content-security-policy":
    "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'",
  "x-content-type-options": "nosniff",
  "referrer-policy": "no-referrer",
  "cache-control": "no-store",
};

const usd = (n: number) => `$${n.toFixed(n < 0.1 ? 3 : 2)}`;
const bytes = (s: string) => new TextEncoder().encode(s);
const same = (a: string, b: string) => {
  const x = bytes(a);
  const y = bytes(b);
  return x.byteLength === y.byteLength && timingSafeEqual(x, y);
};

// ponytail: the session is a fixed hash of RUN_TOKEN; rotating the token is the logout-everywhere button.
async function session(token: string) {
  const digest = await crypto.subtle.digest("SHA-256", bytes(`watchd-session:${token}`));
  return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, "0")).join("");
}

async function caller(request: Request, env: Env): Promise<"bearer" | "cookie" | null> {
  const auth = request.headers.get("authorization");
  if (auth) return same(auth, `Bearer ${env.RUN_TOKEN}`) ? "bearer" : null;
  const cookie = /(?:^|;\s*)watchd=([0-9a-f]{64})/.exec(request.headers.get("cookie") ?? "")?.[1];
  return cookie && same(cookie, await session(env.RUN_TOKEN)) ? "cookie" : null;
}

export async function handle(request: Request, env: Env): Promise<Response> {
  if (!env.RUN_TOKEN || env.RUN_TOKEN.length < 24)
    return new Response("RUN_TOKEN must be a random string of at least 24 characters", { status: 500 });
  const url = new URL(request.url);
  const { pathname } = url;
  const method = request.method;
  const match = (re: RegExp) => re.exec(pathname)?.[1];

  const shared = match(/^\/p\/([a-z0-9-]+)$/);
  if (shared && method === "GET") {
    const agent = await getAgent(env, shared);
    return agent?.public ? publicPage(env, agent) : notFound();
  }
  if (pathname === "/login" && method === "POST") {
    const form = await request.formData();
    if (!same(String(form.get("token") ?? ""), env.RUN_TOKEN)) return page("watchd", loginForm("Wrong token."), 401);
    return redirect("/", `watchd=${await session(env.RUN_TOKEN)}; Path=/; HttpOnly; Secure; SameSite=Strict; Max-Age=2592000`);
  }

  const who = await caller(request, env);
  if (pathname === "/" && method === "GET") return who ? dashboard(env, url.searchParams.get("notice")) : page("watchd", loginForm());
  if (!who) return new Response("Unauthorized", { status: 401 });

  if (pathname === "/mcp") return method === "POST" ? mcp(request, env) : new Response(null, { status: 405 });
  if (pathname === "/logout" && method === "POST")
    return redirect("/", "watchd=; Path=/; HttpOnly; Secure; SameSite=Strict; Max-Age=0");
  if (pathname === "/agents/new" && method === "GET") return agentPage(env, null);
  if (pathname === "/agents" && method === "POST") return submitAgent(env, await request.formData());

  const named = match(/^\/(?:agents|run)\/([a-z0-9-]+)(?:\/delete)?$/);
  const agent = named ? await getAgent(env, named) : null;
  if (agent && pathname === `/agents/${agent.name}` && method === "GET") return agentPage(env, agent);
  if (agent && pathname === `/agents/${agent.name}/delete` && method === "POST") {
    await deleteAgent(env, agent.name);
    return redirect(`/?notice=deleted:${agent.name}`);
  }
  if (agent && pathname === `/run/${agent.name}` && method === "POST") {
    const result = await enqueue(env, agent, Date.now());
    if (who === "bearer") return Response.json(result);
    return redirect(`/?notice=${result.status === "queued" ? `queued:${agent.name}` : `skipped:${result.reason}`}`);
  }

  const runId = match(/^\/runs\/(\d+)$/);
  if (runId && method === "GET") {
    const run = await env.DB.prepare("SELECT id, agent, status, cost, reserved, output, error, started_at, finished_at FROM runs WHERE id = ?")
      .bind(Number(runId))
      .first();
    return run ? Response.json(run) : notFound();
  }
  return notFound();
}

const notFound = () => new Response("Not found", { status: 404 });
const redirect = (location: string, cookie?: string) =>
  new Response(null, { status: 303, headers: { location, ...(cookie && { "set-cookie": cookie }) } });

async function submitAgent(env: Env, form: FormData) {
  const text = (k: string) => String(form.get(k) ?? "").trim();
  const draft = {
    name: text("name"),
    prompt: text("prompt"),
    schedule: text("schedule") || null,
    budget: Number(text("budget")),
    model: text("model") || null,
    tools: form.getAll("tools").map(String),
    max_steps: Number(text("max_steps")),
    memory: form.get("memory") === "on",
    public: form.get("public") === "on",
  };
  const parsed = AgentInput.safeParse(draft);
  const creating = form.get("mode") === "create";
  const error = !parsed.success
    ? parsed.error.issues.map((i) => `${i.path.join(".") || "form"}: ${i.message}`).join(". ")
    : creating && (await getAgent(env, parsed.data.name))
      ? `An agent named ${parsed.data.name} already exists.`
      : "";
  if (error || !parsed.success) return agentPage(env, creating ? null : (draft as Agent), error, draft as Agent);
  await saveAgent(env, parsed.data);
  return redirect(`/?notice=saved:${parsed.data.name}`);
}

type Run = {
  id: number;
  agent: string;
  status: string;
  cost: number | null;
  reserved: number;
  output: string | null;
  error: string | null;
  started_at: string;
};
type Step = { run_id: number; tool: string; input: string; output: string; cost: number };

const NOTICES: Record<string, (v: string) => string> = {
  queued: (v) => `Queued <b>${esc(v)}</b>. It starts within a minute; this page refreshes until it finishes.`,
  saved: (v) => `Saved <b>${esc(v)}</b>.`,
  deleted: (v) => `Deleted <b>${esc(v)}</b>. Its past runs are kept.`,
  skipped: (v) => (v === "daily_cap" ? "Not queued: today's spend cap would be exceeded." : "Not queued: that run already exists."),
};

async function dashboard(env: Env, notice: string | null) {
  const day = new Date().toISOString().slice(0, 10);
  const [spent, latest, recent, steps] = await env.DB.batch<Record<string, unknown>>([
    env.DB.prepare("SELECT COALESCE(SUM(COALESCE(cost, reserved)), 0) AS usd FROM runs WHERE day = ?").bind(day),
    env.DB.prepare("SELECT * FROM runs WHERE id IN (SELECT MAX(id) FROM runs GROUP BY agent)"),
    env.DB.prepare("SELECT * FROM runs ORDER BY id DESC LIMIT 30"),
    env.DB.prepare(
      "SELECT run_id, tool, substr(input, 1, 200) AS input, substr(output, 1, 200) AS output, cost FROM steps WHERE run_id IN (SELECT id FROM runs ORDER BY id DESC LIMIT 30) ORDER BY id",
    ),
  ]);
  const agents = await listAgents(env);
  const last = new Map((latest.results as Run[]).map((r) => [r.agent, r]));
  const trace = new Map<number, Step[]>();
  for (const s of steps.results as Step[]) trace.set(s.run_id, [...(trace.get(s.run_id) ?? []), s]);
  const runs = recent.results as Run[];
  const busy = runs.some((r) => r.status === "queued" || r.status === "running");
  const [kind, value = ""] = (notice ?? "").split(/:(.*)/s);
  const banner = NOTICES[kind] ? `<p class="notice">${NOTICES[kind](value)}</p>` : "";

  const rows = agents
    .map((a) => {
      const r = last.get(a.name);
      return `<tr>
        <td><a href="/agents/${a.name}"><b>${a.name}</b></a>${a.public ? ` <a class="tag" href="/p/${a.name}">public</a>` : ""}</td>
        <td>${a.schedule ? `<code>${esc(a.schedule)}</code>` : "<small>manual</small>"}</td>
        <td>${usd(a.budget)}</td>
        <td>${r ? `<span class="s ${r.status}">${r.status}</span> <small>${esc(r.started_at)}</small>` : "<small>never</small>"}</td>
        <td><form method="post" action="/run/${a.name}"><button>Run now</button></form></td>
      </tr>`;
    })
    .join("");
  const items = runs
    .map((r, i) => {
      const s = trace.get(r.id) ?? [];
      const steps = s.length
        ? `<ol class="trace">${s
            .map(
              (t) =>
                `<li><b>${esc(t.tool)}</b> <small>${esc(t.tool === "model" ? t.output : t.input)}${t.cost ? ` · ${usd(t.cost)}` : ""}</small></li>`,
            )
            .join("")}</ol>`
        : "";
      const body = r.error
        ? `<pre>${esc(r.error)}</pre>`
        : r.output
          ? `<div class="md">${markdown(r.output)}</div>`
          : "<p><small>Working…</small></p>";
      return `<details${i === 0 ? " open" : ""}>
        <summary><span class="s ${r.status}">${r.status}</span> <b>${esc(r.agent)}</b>
          <small>${esc(r.started_at)} UTC · ${usd(r.cost ?? r.reserved)}${s.length ? ` · ${s.length} steps` : ""}</small></summary>
        ${body}${steps ? `<details class="sub"><summary><small>Trace</small></summary>${steps}</details>` : ""}
      </details>`;
    })
    .join("");
  const usdSpent = Number((spent.results[0] as { usd: number }).usd);
  return page(
    "watchd",
    `<header><h1>watchd</h1>
      <span>${usd(usdSpent)} of ${usd(Number(env.DAILY_CAP_USD))} today</span>
      <a class="button" href="/agents/new">New agent</a>
      <form method="post" action="/logout"><button class="ghost">Sign out</button></form></header>
    ${banner}
    <table><thead><tr><th>Agent</th><th>Schedule (UTC)</th><th>Budget</th><th>Last run</th><th></th></tr></thead>
      <tbody>${rows || `<tr><td colspan="5"><small>No agents yet.</small></td></tr>`}</tbody></table>
    <h2>Recent runs</h2>${items || "<p><small>No runs yet.</small></p>"}`,
    200,
    busy ? '<meta http-equiv="refresh" content="10">' : "",
  );
}

const BLANK: Agent = {
  name: "",
  prompt: "",
  schedule: null,
  budget: 0.05,
  model: null,
  tools: ["fetch_page", "seen"],
  max_steps: 8,
  memory: true,
  public: false,
};

function agentPage(env: Env, existing: Agent | null, error = "", draft?: Agent) {
  const a = draft ?? existing ?? BLANK;
  const v = (s: string | null) => esc(s ?? "");
  const check = (on: boolean) => (on ? " checked" : "");
  const tools = TOOL_NAMES.map(
    (t) => `<label class="opt"><input type="checkbox" name="tools" value="${t}"${check(a.tools.includes(t))}>
      <span><b>${t}</b> <small>${esc(SPECS[t].description)}</small></span></label>`,
  ).join("");
  return page(
    existing ? existing.name : "New agent",
    `<header><h1><a href="/">watchd</a> / ${existing ? esc(existing.name) : "new agent"}</h1></header>
    ${error ? `<p class="notice err">${esc(error)}</p>` : ""}
    <form class="agent" method="post" action="/agents">
      <input type="hidden" name="mode" value="${existing ? "update" : "create"}">
      <label>Name<input name="name" value="${v(a.name)}" ${existing ? "readonly" : "required autofocus"} pattern="[a-z0-9][a-z0-9\\-]{0,39}" placeholder="job-radar"></label>
      <label>Instructions<textarea name="prompt" rows="12" required placeholder="What to check, where, and what to report.">${v(a.prompt)}</textarea></label>
      <div class="row">
        <label>Schedule <small>cron, UTC; empty = manual</small><input name="schedule" value="${v(a.schedule)}" placeholder="0 7 * * 1"></label>
        <label>Budget per run <small>USD</small><input name="budget" type="number" step="0.01" min="0.01" max="5" value="${a.budget}" required></label>
        <label>Max steps<input name="max_steps" type="number" min="1" max="30" value="${a.max_steps}" required></label>
      </div>
      <label>Model <small>OpenRouter slug; empty = ${esc(env.DEFAULT_MODEL)}</small><input name="model" value="${v(a.model)}"></label>
      <fieldset><legend>Tools</legend>${tools}</fieldset>
      <label class="opt"><input type="checkbox" name="memory"${check(a.memory)}><span><b>Memory</b> <small>keep notes between runs</small></span></label>
      <label class="opt"><input type="checkbox" name="public"${check(a.public)}><span><b>Public</b> <small>publish reports at /p/${v(a.name) || "name"}</small></span></label>
      <button>Save</button>
    </form>
    ${
      existing
        ? `<div class="danger"><form method="post" action="/run/${existing.name}"><button class="ghost">Run now</button></form>
      <form method="post" action="/agents/${existing.name}/delete"><label class="opt"><input type="checkbox" required><span><small>I want to delete this agent</small></span></label><button class="ghost">Delete</button></form></div>`
        : ""
    }`,
  );
}

async function publicPage(env: Env, agent: Agent) {
  const { results } = await env.DB.prepare(
    "SELECT output, started_at FROM runs WHERE agent = ? AND status = 'ok' AND output != 'NOTHING' ORDER BY id DESC LIMIT 10",
  )
    .bind(agent.name)
    .all<{ output: string; started_at: string }>();
  const reports = results
    .map((r) => `<article><small>${esc(r.started_at)} UTC</small><div class="md">${markdown(r.output)}</div></article>`)
    .join("");
  return page(agent.name, `<header><h1>${esc(agent.name)}</h1></header>${reports || "<p><small>No reports yet.</small></p>"}`);
}

const loginForm = (error = "") => `<form class="login" method="post" action="/login">
  <h1>watchd</h1>${error ? `<p class="err">${error}</p>` : ""}
  <input type="password" name="token" placeholder="RUN_TOKEN" autocomplete="current-password" required autofocus>
  <button>Sign in</button></form>`;

const page = (title: string, body: string, status = 200, head = "") =>
  new Response(
    `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex">${head}
<title>${esc(title)}</title><style>
:root{--bg:#fafaf9;--fg:#1c1917;--mute:#78716c;--line:#e7e5e4;--card:#fff;--ok:#15803d;--err:#b91c1c;--run:#a16207}
@media (prefers-color-scheme:dark){:root{--bg:#0c0a09;--fg:#e7e5e4;--mute:#a8a29e;--line:#292524;--card:#1c1917;--ok:#4ade80;--err:#f87171;--run:#facc15}}
*{box-sizing:border-box}body{margin:0 auto;max-width:960px;padding:24px 16px;background:var(--bg);color:var(--fg);font:15px/1.5 system-ui,sans-serif}
header{display:flex;align-items:center;flex-wrap:wrap;gap:12px 16px;margin-bottom:24px}header h1{margin:0;font-size:22px;flex:1}header h1 a{text-decoration:none}header span{color:var(--mute)}
h2{font-size:16px;margin:32px 0 8px}small{color:var(--mute)}a{color:inherit}
table{width:100%;border-collapse:collapse;background:var(--card);border:1px solid var(--line);border-radius:8px;overflow:hidden}
th,td{text-align:left;padding:10px 12px;border-bottom:1px solid var(--line)}td b{white-space:nowrap}th{font-size:12px;color:var(--mute);font-weight:500}
@media (max-width:640px){th:nth-child(2),td:nth-child(2),th:nth-child(3),td:nth-child(3){display:none}}
details,article{background:var(--card);border:1px solid var(--line);border-radius:8px;margin-bottom:8px;padding:10px 12px}
details.sub{border:0;padding:0;margin:8px 0 0;background:none}.trace{margin:6px 0 0;padding-left:22px;font-size:13px}.trace li{margin:2px 0;word-break:break-all}
summary{cursor:pointer}pre{white-space:pre-wrap;word-break:break-word;font:13px/1.55 ui-monospace,monospace;margin:10px 0 0}
.s{font-size:12px;font-weight:600}.ok{color:var(--ok)}.error{color:var(--err)}.running,.queued{color:var(--run)}
.tag{font-size:11px;border:1px solid var(--line);border-radius:4px;padding:1px 5px;text-decoration:none;color:var(--mute)}
button,.button{white-space:nowrap;font:inherit;padding:6px 12px;border-radius:6px;border:1px solid var(--fg);background:var(--fg);color:var(--bg);cursor:pointer;text-decoration:none}
button.ghost{background:none;color:var(--fg);border-color:var(--line)}form{margin:0}
.notice{background:var(--card);border:1px solid var(--line);border-radius:8px;padding:10px 12px;margin:0 0 16px}
input,textarea{font:inherit;padding:8px 10px;border-radius:6px;border:1px solid var(--line);background:var(--card);color:var(--fg);width:100%}
textarea{font:13.5px/1.55 ui-monospace,monospace;resize:vertical}
.agent{display:grid;gap:14px;max-width:720px}.agent label{display:grid;gap:4px;font-weight:500}.row{display:grid;grid-template-columns:2fr 1fr 1fr;gap:12px;align-items:end}
@media (max-width:640px){.row{grid-template-columns:1fr}}
fieldset{border:1px solid var(--line);border-radius:8px;padding:8px 12px;display:grid;gap:6px}legend{font-weight:500;padding:0 4px}
label.opt{display:flex;gap:10px;align-items:flex-start;font-weight:400}label.opt input{width:auto;margin-top:4px}
.danger{display:flex;gap:16px;align-items:center;flex-wrap:wrap;margin-top:32px;padding-top:16px;border-top:1px solid var(--line);max-width:720px}.danger form{display:flex;gap:10px;align-items:center}
.login{max-width:320px;margin:15vh auto;display:grid;gap:12px}
.err{color:var(--err);margin:0}
.md{max-width:72ch;margin-top:10px;line-height:1.6}.md p{margin:0 0 12px}.md h3,.md h4,.md h5{margin:18px 0 8px;font-size:15px}
.md p>strong:first-child{font-size:15.5px}.md ul,.md ol{margin:0 0 12px;padding-left:22px}.md li{margin:2px 0}
.md code{font:12.5px ui-monospace,monospace;background:var(--line);padding:1px 5px;border-radius:4px}
.md a{color:var(--mute);word-break:break-all}
</style></head><body>${body}</body></html>`,
    { status, headers: HTML_HEADERS },
  );
