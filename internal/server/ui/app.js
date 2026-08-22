const refreshInterval = 5000;
const elements = Object.fromEntries(
  ['mode', 'error', 'freshness', 'summary', 'goals', 'agents', 'activity']
    .map((id) => [id, document.getElementById(id)]),
);

function money(value) {
  return value == null ? '-' : `$${Number(value).toFixed(4)}`;
}

function node(tag, className, text) {
  const value = document.createElement(tag);
  if (className) value.className = className;
  if (text != null) value.textContent = text;
  return value;
}

function age(timestamp) {
  const elapsed = Math.max(0, Math.floor((Date.now() - new Date(timestamp).getTime()) / 1000));
  if (!Number.isFinite(elapsed)) return '-';
  if (elapsed < 60) return `${elapsed}s ago`;
  if (elapsed < 3600) return `${Math.floor(elapsed / 60)}m ago`;
  if (elapsed < 86400) return `${Math.floor(elapsed / 3600)}h ago`;
  return `${Math.floor(elapsed / 86400)}d ago`;
}

function statusClass(status) {
  const safe = String(status).toLowerCase().replace(/[^a-z0-9-]/g, '');
  return `status-${safe || 'unknown'}`;
}

function runLabel(run) {
  if (!run) return '-';
  return `${run.status || 'unknown'} · ${age(run.started_at)}`;
}

function renderSummary(summary) {
  const pending = summary.max_pending == null
    ? String(summary.pending)
    : `${summary.pending} / ${summary.max_pending}`;
  const unrated = summary.max_unrated == null
    ? String(summary.unrated)
    : `${summary.unrated} / ${summary.max_unrated}`;
  const items = [
    ['Daily budget', money(summary.daily_budget_usd), 'money'],
    ['Spent today', money(summary.spent_usd), 'money'],
    ['Remaining', money(summary.remaining_usd), 'money useful'],
    ['Pending', pending, 'count pending'],
    ['Unrated', unrated, 'count'],
  ];
  elements.summary.replaceChildren(...items.map(([label, value, className]) => {
    const card = node('div', 'summary-item');
    card.append(node('span', 'label', label), node('strong', className, value));
    return card;
  }));
}

function renderGoals(goals) {
  if (goals.length === 0) {
    elements.goals.replaceChildren(node('p', 'empty', 'No goals configured.'));
    return;
  }
  elements.goals.replaceChildren(...goals.map((goal) => {
    const card = node('article', 'card');
    const details = node('div', 'metadata');
    details.append(
      node('span', null, `Weight ${Number(goal.weight).toFixed(2)}`),
      node('span', null, goal.authority),
      node('span', 'money', `Spent ${money(goal.spent_usd)}`),
      node('span', 'money', `Budget ${money(goal.daily_budget_usd)}`),
      node('span', 'count useful', `${goal.useful_outcomes} useful`),
      node('span', 'money', `Cost/useful ${money(goal.cost_per_useful_usd)}`),
    );
    card.append(
      node('h2', null, goal.name),
      node('p', 'description', goal.description || 'No description.'),
      details,
    );
    return card;
  }));
}

function renderAgents(agents) {
  if (agents.length === 0) {
    const row = node('tr');
    const cell = node('td', 'empty', 'No agents configured.');
    cell.colSpan = 8;
    row.append(cell);
    elements.agents.replaceChildren(row);
    return;
  }
  elements.agents.replaceChildren(...agents.map((agent) => {
    const row = node('tr');
    const identity = node('td');
    identity.append(node('strong', null, agent.name), node('small', 'metadata', `${agent.model} · ${agent.mode}`));
    const score = agent.score == null
      ? '-'
      : `${Number(agent.score).toFixed(2)} (${agent.rated_samples} rated)`;
    const lastRun = node('td');
    if (agent.latest_run) {
      lastRun.append(node('span', `status ${statusClass(agent.latest_run.status)}`, runLabel(agent.latest_run)));
    } else {
      lastRun.textContent = '-';
    }
    row.append(
      identity,
      node('td', null, agent.goal || '-'),
      node('td', null, agent.manual_only ? 'Manual only' : agent.schedule || '-'),
      node('td', null, agent.authority),
      node('td', 'money', money(agent.budget_usd)),
      node('td', 'score', score),
      node('td', null, agent.decision || '-'),
      lastRun,
    );
    return row;
  }));
}

function renderActivity(runs) {
  if (runs.length === 0) {
    elements.activity.replaceChildren(node('p', 'empty', 'No recent activity.'));
    return;
  }
  elements.activity.replaceChildren(...runs.map((run) => {
    const entry = node('article', 'activity-item');
    const heading = node('div', 'activity-heading');
    heading.append(
      node('strong', null, run.agent),
      node('span', `status ${statusClass(run.status)}`, run.status || 'unknown'),
      node('span', 'metadata', age(run.started_at)),
    );
    const details = node('div', 'metadata');
    details.append(
      node('span', null, run.goal || 'No goal'),
      node('span', 'money', money(run.cost_usd)),
      node('span', 'count', `${run.duration_ms} ms`),
      node('span', null, run.outcome || ''),
      node('span', null, run.allocation_reason || ''),
    );
    entry.append(heading, node('p', 'description', run.error || run.result || 'No result recorded.'), details);
    return entry;
  }));
}

function renderSnapshot(snapshot) {
  elements.mode.textContent = snapshot.mode;
  renderSummary(snapshot.summary);
  renderGoals(snapshot.goals);
  renderAgents(snapshot.agents);
  renderActivity(snapshot.recent_runs);
  elements.error.textContent = '';
  elements.freshness.textContent = `Updated ${new Date(snapshot.generated_at).toLocaleTimeString()}`;
}

function showError(message) {
  elements.error.textContent = message;
  elements.freshness.textContent = 'Showing last successful refresh';
}

async function refresh() {
  try {
    const response = await fetch('/api/v1/snapshot', { headers: { Accept: 'application/json' } });
    const body = await response.json();
    if (!response.ok) throw new Error(body.error?.message || `HTTP ${response.status}`);
    renderSnapshot(body);
  } catch (error) {
    showError(error instanceof Error ? error.message : String(error));
  }
}

refresh();
setInterval(refresh, refreshInterval);
