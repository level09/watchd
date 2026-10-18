import { timingSafeEqual } from "node:crypto";
import { runAgent } from "./run";
import type { Agent, Env } from "./types";

const HTML_HEADERS = {
  "content-type": "text/html; charset=utf-8",
  "content-security-policy":
    "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'",
  "x-content-type-options": "nosniff",
  "referrer-policy": "no-referrer",
  "cache-control": "no-store",
};

const esc = (s: string) => s.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`);
const linkify = (s: string) =>
  esc(s).replace(/https?:\/\/[^\s<>"')\]]+/g, (u) => `<a href="${u}" rel="noreferrer">${u}</a>`);
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

export async function handle(request: Request, env: Env, agents: Agent[]): Promise<Response> {
  if (!env.RUN_TOKEN || env.RUN_TOKEN.length < 24)
    return new Response("RUN_TOKEN must be a random string of at least 24 characters", { status: 500 });
  const { pathname } = new URL(request.url);
  const method = request.method;

  const shared = /^\/p\/([\w-]+)$/.exec(pathname)?.[1];
  if (shared && method === "GET") {
    const agent = agents.find((a) => a.name === shared && a.public);
    return agent ? publicPage(env, agent) : notFound();
  }
  if (pathname === "/login" && method === "POST") {
    const form = await request.formData();
    if (!same(String(form.get("token") ?? ""), env.RUN_TOKEN))
      return page("watchd", loginForm("Wrong token."), 401);
    return redirect(
      `watchd=${await session(env.RUN_TOKEN)}; Path=/; HttpOnly; Secure; SameSite=Strict; Max-Age=2592000`,
    );
  }

  const who = await caller(request, env);
  if (pathname === "/" && method === "GET")
    return who ? dashboard(env, agents) : page("watchd", loginForm());
  if (!who) return new Response("Unauthorized", { status: 401 });
  if (pathname === "/logout" && method === "POST")
    return redirect("watchd=; Path=/; HttpOnly; Secure; SameSite=Strict; Max-Age=0");
  const name = /^\/run\/([\w-]+)$/.exec(pathname)?.[1];
  const agent = agents.find((a) => a.name === name);
  if (agent && method === "POST") {
    const result = await runAgent(env, agent, Date.now());
    return who === "bearer" ? Response.json(result) : redirect();
  }
  return notFound();
}

const notFound = () => new Response("Not found", { status: 404 });
const redirect = (cookie?: string) =>
  new Response(null, { status: 303, headers: { location: "/", ...(cookie && { "set-cookie": cookie }) } });

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

async function dashboard(env: Env, agents: Agent[]) {
  const day = new Date().toISOString().slice(0, 10);
  const [spent, latest, recent] = await env.DB.batch<Record<string, unknown>>([
    env.DB.prepare("SELECT COALESCE(SUM(COALESCE(cost, reserved)), 0) AS usd FROM runs WHERE day = ?").bind(day),
    env.DB.prepare("SELECT * FROM runs WHERE id IN (SELECT MAX(id) FROM runs GROUP BY agent)"),
    env.DB.prepare("SELECT * FROM runs ORDER BY id DESC LIMIT 30"),
  ]);
  const last = new Map((latest.results as Run[]).map((r) => [r.agent, r]));
  const rows = agents
    .map((a) => {
      const r = last.get(a.name);
      return `<tr>
        <td><b>${esc(a.name)}</b>${a.public ? ` <a class="tag" href="/p/${a.name}">public</a>` : ""}</td>
        <td><code>${esc(a.schedule)}</code></td>
        <td>${usd(a.budget)}</td>
        <td>${r ? `<span class="s ${r.status}">${r.status}</span> <small>${esc(r.started_at)}</small>` : "<small>never</small>"}</td>
        <td><form method="post" action="/run/${a.name}"><button>Run now</button></form></td>
      </tr>`;
    })
    .join("");
  const runs = (recent.results as Run[])
    .map(
      (r) => `<details>
        <summary><span class="s ${r.status}">${r.status}</span> <b>${esc(r.agent)}</b>
          <small>${esc(r.started_at)} UTC · ${usd(r.cost ?? r.reserved)}</small></summary>
        <pre>${linkify(r.error ?? r.output ?? "")}</pre>
      </details>`,
    )
    .join("");
  const usdSpent = Number((spent.results[0] as { usd: number }).usd);
  return page(
    "watchd",
    `<header><h1>watchd</h1>
      <span>${usd(usdSpent)} of ${usd(Number(env.DAILY_CAP_USD))} today</span>
      <form method="post" action="/logout"><button class="ghost">Sign out</button></form></header>
    <table><thead><tr><th>Agent</th><th>Schedule (UTC)</th><th>Budget</th><th>Last run</th><th></th></tr></thead>
      <tbody>${rows}</tbody></table>
    <h2>Recent runs</h2>${runs || "<p><small>No runs yet.</small></p>"}`,
  );
}

async function publicPage(env: Env, agent: Agent) {
  const { results } = await env.DB.prepare(
    "SELECT output, started_at FROM runs WHERE agent = ? AND status = 'ok' AND output != 'NOTHING' ORDER BY id DESC LIMIT 10",
  )
    .bind(agent.name)
    .all<{ output: string; started_at: string }>();
  const reports = results
    .map((r) => `<article><small>${esc(r.started_at)} UTC</small><pre>${linkify(r.output)}</pre></article>`)
    .join("");
  return page(agent.name, `<header><h1>${esc(agent.name)}</h1></header>${reports || "<p><small>No reports yet.</small></p>"}`);
}

const loginForm = (error = "") => `<form class="login" method="post" action="/login">
  <h1>watchd</h1>${error ? `<p class="err">${error}</p>` : ""}
  <input type="password" name="token" placeholder="RUN_TOKEN" autocomplete="current-password" required autofocus>
  <button>Sign in</button></form>`;

const page = (title: string, body: string, status = 200) =>
  new Response(
    `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex">
<title>${esc(title)}</title><style>
:root{--bg:#fafaf9;--fg:#1c1917;--mute:#78716c;--line:#e7e5e4;--card:#fff;--ok:#15803d;--err:#b91c1c;--run:#a16207}
@media (prefers-color-scheme:dark){:root{--bg:#0c0a09;--fg:#e7e5e4;--mute:#a8a29e;--line:#292524;--card:#1c1917;--ok:#4ade80;--err:#f87171;--run:#facc15}}
*{box-sizing:border-box}body{margin:0 auto;max-width:960px;padding:24px 16px;background:var(--bg);color:var(--fg);font:15px/1.5 system-ui,sans-serif}
header{display:flex;align-items:center;gap:16px;margin-bottom:24px}header h1{margin:0;font-size:22px;flex:1}header span{color:var(--mute)}
h2{font-size:16px;margin:32px 0 8px}small{color:var(--mute)}a{color:inherit}
table{width:100%;border-collapse:collapse;background:var(--card);border:1px solid var(--line);border-radius:8px;overflow:hidden}
th,td{text-align:left;padding:10px 12px;border-bottom:1px solid var(--line)}td b{white-space:nowrap}th{font-size:12px;color:var(--mute);font-weight:500}
@media (max-width:640px){th:nth-child(2),td:nth-child(2),th:nth-child(3),td:nth-child(3){display:none}}
details,article{background:var(--card);border:1px solid var(--line);border-radius:8px;margin-bottom:8px;padding:10px 12px}
summary{cursor:pointer}pre{white-space:pre-wrap;word-break:break-word;font:13px/1.55 ui-monospace,monospace;margin:10px 0 0}
.s{font-size:12px;font-weight:600}.ok{color:var(--ok)}.error{color:var(--err)}.running{color:var(--run)}
.tag{font-size:11px;border:1px solid var(--line);border-radius:4px;padding:1px 5px;text-decoration:none;color:var(--mute)}
button{white-space:nowrap;font:inherit;padding:6px 12px;border-radius:6px;border:1px solid var(--fg);background:var(--fg);color:var(--bg);cursor:pointer}
button.ghost{background:none;color:var(--fg);border-color:var(--line)}form{margin:0}
.login{max-width:320px;margin:15vh auto;display:grid;gap:12px}.login input{font:inherit;padding:8px 10px;border-radius:6px;border:1px solid var(--line);background:var(--card);color:var(--fg)}
.err{color:var(--err);margin:0}
</style></head><body>${body}</body></html>`,
    { status, headers: HTML_HEADERS },
  );
