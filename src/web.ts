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
    "default-src 'none'; style-src 'unsafe-inline' https://fonts.googleapis.com; font-src https://fonts.gstatic.com; form-action 'self'; frame-ancestors 'none'; base-uri 'none'",
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
    auth: text("auth_host") || text("auth_secret") ? { host: text("auth_host"), secret: text("auth_secret") } : null,
    reason_model: text("reason_model") || null,
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
        <th><a href="/agents/${a.name}">${a.name}</a>${a.public ? ` <a class="tag" href="/p/${a.name}">public</a>` : ""}</th>
        <td>${a.schedule ? `<code>${esc(a.schedule)}</code>` : "<small>manual</small>"}</td>
        <td class="n">${usd(a.budget)}</td>
        <td>${r ? `<span class="s ${r.status}">${r.status}</span> <small>${esc(r.started_at)}</small>` : "<small>never</small>"}</td>
        <td class="act"><form method="post" action="/run/${a.name}"><button class="ghost">Run now</button></form></td>
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
      return `<details class="card"${i === 0 ? " open" : ""}>
        <summary><span class="s ${r.status}">${r.status}</span> <b>${esc(r.agent)}</b>
          <small>${esc(r.started_at)} UTC · ${usd(r.cost ?? r.reserved)}${s.length ? ` · ${s.length} steps` : ""}</small></summary>
        ${body}${steps ? `<details class="sub"><summary>Trace</summary>${steps}</details>` : ""}
      </details>`;
    })
    .join("");
  const usdSpent = Number((spent.results[0] as { usd: number }).usd);
  const cap = Number(env.DAILY_CAP_USD);
  const active = runs.filter((r) => r.status === "queued" || r.status === "running").length;
  return page(
    "watchd",
    `${bar("", `<a class="button" href="/agents/new">New agent</a>
      <form method="post" action="/logout"><button class="ghost">Sign out</button></form>`)}
    <main class="wrap">${banner}
    <div class="tiles">
      <div class="tile"><span class="k">Spent today</span><b>${usd(usdSpent)}</b>
        <div class="meter"><i style="width:${Math.min(100, (usdSpent / cap) * 100).toFixed(1)}%"></i></div><p>of ${usd(cap)} daily cap</p></div>
      <div class="tile"><span class="k">Agents</span><b>${agents.length}</b><p>${agents.filter((a) => a.schedule).length} scheduled</p></div>
      <div class="tile"><span class="k">Active runs</span><b>${active}</b><p>${active ? "refreshing every 10s" : "nothing in flight"}</p></div>
    </div>
    <section><h2>Agents</h2>
    <div class="panel tbl"><table><thead><tr><th>Agent</th><th>Schedule (UTC)</th><th>Budget</th><th>Last run</th><th></th></tr></thead>
      <tbody>${rows || `<tr><td colspan="5"><small>No agents yet.</small></td></tr>`}</tbody></table></div></section>
    <section><h2>Recent runs</h2>${items || "<p><small>No runs yet.</small></p>"}</section></main>`,
    200,
    busy ? '<meta http-equiv="refresh" content="10">' : "",
  );
}

const bar = (crumb: string, actions = "") =>
  `<header class="bar"><div class="wrap"><b><a href="/">watchd</a>${crumb ? ` <span>/ ${crumb}</span>` : ""}</b><nav>${actions}</nav></div></header>`;

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
  auth: null,
  reason_model: null,
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
    `${bar(existing ? esc(existing.name) : "new agent")}<main class="wrap narrow">
    <h1>${existing ? esc(existing.name) : "New agent"}</h1>
    ${error ? `<p class="notice err">${esc(error)}</p>` : ""}
    <form class="agent panel" method="post" action="/agents">
      <input type="hidden" name="mode" value="${existing ? "update" : "create"}">
      <label>Name<input name="name" value="${v(a.name)}" ${existing ? "readonly" : "required autofocus"} pattern="[a-z0-9][a-z0-9\\-]{0,39}" placeholder="job-radar"></label>
      <label>Instructions<textarea name="prompt" rows="12" required placeholder="What to check, where, and what to report.">${v(a.prompt)}</textarea></label>
      <div class="row">
        <label>Schedule <small>cron, UTC; empty = manual</small><input name="schedule" value="${v(a.schedule)}" placeholder="0 7 * * 1"></label>
        <label>Budget per run <small>USD</small><input name="budget" type="number" step="0.01" min="0.01" max="5" value="${a.budget}" required></label>
        <label>Max steps<input name="max_steps" type="number" min="1" max="30" value="${a.max_steps}" required></label>
      </div>
      <label>Model <small>OpenRouter slug; empty = ${esc(env.DEFAULT_MODEL)}</small><input name="model" value="${v(a.model)}"></label>
      <label>Reasoning model <small>OpenRouter slug; writes the report from what the model above collected; instructions after a line starting \"For the analyst:\" go only to it; empty = one stage</small><input name="reason_model" value="${v(a.reason_model)}"></label>
      <fieldset><legend>Tools</legend>${tools}</fieldset>
      <div class="row">
        <label>API host <small>fetch_page sends the secret as a Bearer token to this host only; empty = none</small><input name="auth_host" value="${v(a.auth?.host ?? null)}" placeholder="api.example.com"></label>
        <label>Secret name <small>a Worker secret</small><input name="auth_secret" value="${v(a.auth?.secret ?? null)}" placeholder="EXAMPLE_API_KEY"></label>
      </div>
      <label class="opt"><input type="checkbox" name="memory"${check(a.memory)}><span><b>Memory</b> <small>keep notes between runs</small></span></label>
      <label class="opt"><input type="checkbox" name="public"${check(a.public)}><span><b>Public</b> <small>publish reports at /p/${v(a.name) || "name"}</small></span></label>
      <div><button>Save</button></div>
    </form>
    ${
      existing
        ? `<div class="danger"><form method="post" action="/run/${existing.name}"><button class="ghost">Run now</button></form>
      <form method="post" action="/agents/${existing.name}/delete"><label class="opt"><input type="checkbox" required><span><small>I want to delete this agent</small></span></label><button class="ghost bad">Delete</button></form></div>`
        : ""
    }</main>`,
  );
}

async function publicPage(env: Env, agent: Agent) {
  const { results } = await env.DB.prepare(
    "SELECT output, started_at FROM runs WHERE agent = ? AND status = 'ok' AND output != 'NOTHING' ORDER BY id DESC LIMIT 10",
  )
    .bind(agent.name)
    .all<{ output: string; started_at: string }>();
  const reports = results
    .map((r) => `<article class="card"><span class="k">${esc(r.started_at)} UTC</span><div class="md">${markdown(r.output)}</div></article>`)
    .join("");
  return page(
    agent.name,
    `<main class="wrap narrow"><h1>${esc(agent.name)}</h1>${reports || "<p><small>No reports yet.</small></p>"}</main>`,
  );
}

const loginForm = (error = "") => `<form class="login panel" method="post" action="/login">
  <h1>watchd</h1>${error ? `<p class="err">${error}</p>` : ""}
  <input type="password" name="token" placeholder="RUN_TOKEN" autocomplete="current-password" required autofocus>
  <button>Sign in</button></form>`;

const page = (title: string, body: string, status = 200, head = "") =>
  new Response(
    `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex">${head}
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Figtree:wght@400;600;800&family=JetBrains+Mono:wght@400;700&display=swap" rel="stylesheet">
<title>${esc(title)}</title><style>
:root{--paper:#f4f0e8;--card:#f7f4ee;--rail:#efe9de;--ink:#2b2e45;--ink-2:#555a70;--ink-3:#6b6f82;--line:#c9c4b8;--yellow:#f2cb72;--mint:#a9d6c0;--coral:#e8704a;--coral-ink:#a84a28;
--sans:"Figtree",system-ui,sans-serif;--mono:"JetBrains Mono",ui-monospace,monospace;color-scheme:light}
*{box-sizing:border-box}body{margin:0;background:var(--paper);color:var(--ink);font:400 1rem/1.55 var(--sans)}
::selection{background:var(--yellow)}:focus-visible{outline:3px solid var(--coral);outline-offset:2px}
a{color:inherit}small{color:var(--ink-3)}form{margin:0}p{margin:0}
.wrap{max-width:1100px;margin:0 auto;padding:0 24px}.wrap.narrow{max-width:820px}main{padding-bottom:72px}
h1{font:800 clamp(1.8rem,3.6vw,2.6rem)/1.08 var(--sans);letter-spacing:-.025em;margin:40px 0 24px}
h2{font:800 clamp(1.4rem,2.4vw,1.8rem)/1.1 var(--sans);letter-spacing:-.02em;margin:0 0 16px}section{padding-top:48px}
.k{font:400 .72rem var(--mono);letter-spacing:.08em;text-transform:uppercase;color:var(--ink-2)}
code{font:.86em var(--mono);background:#ebe5d8;padding:.05em .35em;border-radius:4px;overflow-wrap:anywhere}
.bar{position:sticky;top:0;z-index:10;background:rgb(244 240 232/.92);backdrop-filter:blur(8px);border-bottom:1.5px solid var(--line)}
.bar .wrap{display:flex;align-items:center;gap:16px;height:60px}.bar b{font:800 1.05rem var(--sans);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;min-width:0}
.bar b a{text-decoration:none}.bar b span{font-weight:400;color:var(--ink-2)}.bar nav{display:flex;gap:8px;margin-left:auto;align-items:center}
.panel,.card,.tile{border:2px solid var(--ink);border-radius:16px;background:var(--card);box-shadow:5px 5px 0 var(--ink)}
.tiles{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:18px;margin-top:36px}.tile{padding:18px 20px}
.tile b{display:block;font:800 clamp(1.7rem,3vw,2.2rem)/1.15 var(--sans);letter-spacing:-.02em;margin-top:6px;font-variant-numeric:tabular-nums}
.tile p{margin-top:6px;color:var(--ink-2);font-size:.92rem}
.meter{height:10px;margin-top:10px;border:1.5px solid var(--ink);border-radius:999px;background:var(--paper);overflow:hidden}.meter i{display:block;height:100%;background:var(--yellow);border-right:1.5px solid var(--ink)}
.tbl{overflow-x:auto;padding:4px 20px 8px}.tbl table{width:100%;border-collapse:collapse}
.tbl th,.tbl td{text-align:left;vertical-align:middle;padding:12px 16px 12px 0;border-top:1.5px solid var(--line)}
.tbl thead th{border-top:0;border-bottom:2px solid var(--ink);font:400 .72rem var(--mono);letter-spacing:.08em;text-transform:uppercase;color:var(--ink-2);white-space:nowrap}
.tbl tbody tr:first-child>*{border-top:0}.tbl tbody th{font-weight:700;white-space:nowrap}.tbl tbody th a{text-decoration:none}.tbl tbody th a:hover{text-decoration:underline}
.tbl td{color:var(--ink-2)}.tbl td.n{font:400 .9rem var(--mono);color:var(--ink)}.tbl td.act{text-align:right;padding-right:0}
@media (max-width:640px){.tbl th:nth-child(2),.tbl td:nth-child(2),.tbl th:nth-child(3),.tbl td:nth-child(3){display:none}}
.s{display:inline-block;font:700 .68rem/1 var(--mono);letter-spacing:.08em;text-transform:uppercase;padding:5px 8px;border:1.5px solid var(--ink);border-radius:999px;background:var(--card);vertical-align:2px}
.s.ok{background:var(--mint)}.s.error{background:var(--coral);color:var(--card)}.s.running,.s.queued{background:var(--yellow)}
.tag{font:400 .68rem var(--mono);letter-spacing:.08em;text-transform:uppercase;border:1.5px solid var(--line);border-radius:6px;padding:1px 6px;margin-left:6px;text-decoration:none;color:var(--ink-2)}
details.card,article.card{padding:14px 20px;margin-bottom:16px}article.card{padding:18px 22px 6px;margin-bottom:22px}
details.card>summary{cursor:pointer;display:flex;flex-wrap:wrap;align-items:center;gap:6px 12px;list-style:none;font-size:1.05rem}
details.card>summary::-webkit-details-marker{display:none}details.card>summary small{font:400 .8rem var(--mono);margin-left:auto}
details.card[open]>summary{padding-bottom:12px;border-bottom:1.5px solid var(--line)}
details.sub{margin:16px 0 6px}details.sub>summary{cursor:pointer;font:400 .78rem var(--mono);letter-spacing:.08em;text-transform:uppercase;color:var(--ink-2)}
.trace{margin:10px 0 0;padding:10px 14px 10px 34px;border:1.5px solid var(--line);border-radius:10px;background:var(--rail);font:400 .8rem/1.6 var(--mono)}
.trace li{margin:3px 0;overflow-wrap:anywhere}.trace li::marker{color:var(--ink-3)}
pre{white-space:pre-wrap;word-break:break-word;font:.85rem/1.6 var(--mono);margin:14px 0 4px;padding:12px 14px;border-left:4px solid var(--coral);background:#fbeee6;border-radius:6px;color:var(--coral-ink)}
button,.button{display:inline-flex;align-items:center;white-space:nowrap;font:700 .85rem var(--mono);padding:7px 14px;border-radius:999px;border:2px solid var(--ink);background:var(--ink);color:var(--card);cursor:pointer;text-decoration:none}
button.ghost{background:var(--card);color:var(--ink)}button:hover,.button:hover{box-shadow:2px 2px 0 var(--ink);transform:translate(-1px,-1px)}
button.bad{border-color:var(--coral);color:var(--coral-ink)}button.bad:hover{box-shadow:2px 2px 0 var(--coral)}
.notice{margin:28px 0 0;padding:12px 16px;border:2px solid var(--ink);border-radius:12px;background:#fff1cc}.notice.err,.err{color:var(--coral-ink)}.notice.err{background:#fbeee6;border-color:var(--coral)}
input,textarea{font:inherit;padding:9px 12px;border-radius:10px;border:1.5px solid var(--line);background:var(--paper);color:var(--ink);width:100%}
input:focus,textarea:focus{border-color:var(--ink);outline:none;box-shadow:3px 3px 0 var(--ink)}input[readonly]{color:var(--ink-3)}
textarea{font:.85rem/1.6 var(--mono);resize:vertical}
.agent{display:grid;gap:18px;padding:24px 26px}.agent label{display:block;font-weight:600}.agent label>input,.agent label>textarea{display:block;margin-top:6px}.agent small{font-weight:400;margin-left:4px}
.row{display:grid;grid-template-columns:2fr 1fr 1fr;gap:14px;align-items:end}@media (max-width:640px){.row{grid-template-columns:1fr}}
fieldset{border:1.5px solid var(--line);border-radius:12px;padding:10px 16px 14px;display:grid;gap:8px;margin:0}legend{font:400 .72rem var(--mono);letter-spacing:.08em;text-transform:uppercase;color:var(--ink-2);padding:0 6px}
label.opt{display:flex;gap:12px;align-items:flex-start;font-weight:400}label.opt input{width:20px;height:20px;flex:none;margin:2px 0 0;accent-color:var(--ink)}label.opt small{display:block;margin:0;color:var(--ink-2)}
.danger{display:flex;gap:16px;align-items:center;flex-wrap:wrap;margin-top:32px;padding-top:20px;border-top:1.5px solid var(--line)}.danger form{display:flex;gap:12px;align-items:center}
.login{max-width:360px;margin:18vh auto 0;display:grid;gap:14px;padding:26px}.login h1{margin:0 0 4px}
.md{max-width:72ch;margin-top:14px;line-height:1.6;font-size:1.02rem}.md p{margin:0 0 14px}
.md h3,.md h4,.md h5{font-family:var(--sans);margin:28px 0 10px;line-height:1.3;letter-spacing:-.01em}.md h3{font-size:1.3rem;font-weight:800}.md h4{font-size:1.1rem;padding-bottom:6px;border-bottom:1.5px solid var(--line)}.md h5{font-size:1rem}
.md>:first-child{margin-top:4px}.md ul,.md ol{margin:0 0 14px;padding-left:22px}.md li{margin:0 0 12px}.md li::marker{color:var(--ink-3)}
.md strong a{font-weight:700}.md em{font-style:normal;color:var(--ink-3);font-size:.9em}
.md a{text-decoration:underline dotted var(--coral) 2px;text-underline-offset:4px;overflow-wrap:anywhere}.md a:hover{text-decoration-style:solid}
@media (max-width:640px){.wrap{padding:0 16px}details.card>summary small{margin-left:0;width:100%}}
@media (prefers-reduced-motion:reduce){*{transition:none!important}}
</style></head><body>${body}</body></html>`,
    { status, headers: HTML_HEADERS },
  );
