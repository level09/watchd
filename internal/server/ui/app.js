const refreshInterval = 5000;
const elements = Object.fromEntries(
  [
    'mode', 'error', 'freshness', 'summary', 'goals', 'agents', 'activity',
    'agent-dialog', 'agent-form', 'agent-goal', 'agent-form-error', 'agent-preview',
    'agent-preview-path', 'agent-preview-content', 'agent-preview-error', 'create-agent',
    'run-dialog', 'run-title', 'run-status', 'run-job', 'run-message', 'run-details',
    'run-rating', 'run-note', 'run-rating-error',
  ]
    .map((id) => [id, document.getElementById(id)]),
);
let lastSuccessfulRefresh = 'No successful refresh yet';
let approvedDraft = null;

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
    cell.colSpan = 9;
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
    const actions = node('td', 'actions');
    const runButton = node('button', 'button', 'Run now');
    runButton.type = 'button';
    runButton.addEventListener('click', () => runAgent(agent.name));
    actions.append(runButton);
    row.append(
      identity,
      node('td', null, agent.goal || '-'),
      node('td', null, agent.manual_only ? 'Manual only' : agent.schedule || '-'),
      node('td', null, agent.authority),
      node('td', 'money', money(agent.budget_usd)),
      node('td', 'score', score),
      node('td', null, agent.decision || '-'),
      lastRun,
      actions,
    );
    return row;
  }));
}

function showRunDetails(run) {
  elements['run-details'].hidden = false;
  elements['run-details'].replaceChildren(
    node('span', null, `Status: ${run.status || 'unknown'}`),
    node('span', 'money', `Cost: ${money(run.cost_usd)}`),
    node('span', 'count', `Duration: ${run.duration_ms || 0} ms`),
  );
  const result = run.error || run.result || 'No result recorded.';
  elements['run-message'].textContent = result;
}

function resetRunDialog(agentName) {
  elements['run-title'].textContent = `Run ${agentName}`;
  elements['run-status'].textContent = 'Queued';
  elements['run-status'].className = 'status status-pending';
  elements['run-job'].textContent = '';
  elements['run-message'].textContent = 'Starting run...';
  elements['run-details'].hidden = true;
  elements['run-details'].replaceChildren();
  elements['run-rating'].hidden = true;
  elements['run-note'].value = '';
  elements['run-rating-error'].textContent = '';
}

function sleep(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

async function runAgent(agentName) {
  resetRunDialog(agentName);
  elements['run-dialog'].showModal();
  try {
    const job = await postJSON('/api/v1/runs', { agent: agentName });
    elements['run-job'].textContent = job.job_id;
    for (let attempt = 0; attempt < 120; attempt += 1) {
      const response = await fetch(`/api/v1/runs/${encodeURIComponent(job.job_id)}`, { headers: { Accept: 'application/json' } });
      const current = await response.json();
      if (!response.ok) throw new Error(current.error?.message || `HTTP ${response.status}`);
      elements['run-status'].textContent = current.status;
      elements['run-status'].className = `status ${statusClass(current.status)}`;
      if (current.status === 'running') {
        elements['run-message'].textContent = 'Claude is working. Research runs can take a few minutes.';
      }
      if (current.status === 'complete') {
        showRunDetails(current.run || {});
        elements['run-rating'].hidden = !current.run || !['success', 'satisfied', 'incomplete', 'error'].includes(current.run.status);
        await refresh();
        return;
      }
      if (current.status === 'failed') {
        elements['run-message'].textContent = current.error || 'Run failed.';
        return;
      }
      await sleep(1500);
    }
    throw new Error('Run is taking longer than expected. Check recent activity for the result.');
  } catch (error) {
    elements['run-message'].textContent = error instanceof Error ? error.message : String(error);
    elements['run-status'].textContent = 'Failed';
    elements['run-status'].className = 'status status-error';
  }
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
  const selectedGoal = elements['agent-goal'].value;
  elements['agent-goal'].replaceChildren(
    new Option('No goal', ''),
    ...snapshot.goals.map((goal) => new Option(goal.name, goal.name)),
  );
  elements['agent-goal'].value = selectedGoal;
  elements.error.textContent = '';
  lastSuccessfulRefresh = `Updated ${new Date(snapshot.generated_at).toLocaleTimeString()}`;
  elements.freshness.textContent = lastSuccessfulRefresh;
}

async function postJSON(path, value) {
  const response = await fetch(path, {
    method: 'POST',
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    body: JSON.stringify(value),
  });
  const body = await response.json();
  if (!response.ok) throw new Error(body.error?.message || `HTTP ${response.status}`);
  return body;
}

function closeAgentDialog() {
  elements['agent-dialog'].close();
  elements['agent-form'].reset();
  elements['agent-form'].hidden = false;
  elements['agent-preview'].hidden = true;
  elements['agent-form-error'].textContent = '';
  elements['agent-preview-error'].textContent = '';
  approvedDraft = null;
}

document.getElementById('open-agent-form').addEventListener('click', () => {
  elements['agent-dialog'].showModal();
  elements['agent-form'].elements.name.focus();
});
document.getElementById('close-agent-form').addEventListener('click', closeAgentDialog);
document.getElementById('cancel-agent-form').addEventListener('click', closeAgentDialog);
document.getElementById('back-agent-form').addEventListener('click', () => {
  elements['agent-form'].hidden = false;
  elements['agent-preview'].hidden = true;
  elements['agent-preview-error'].textContent = '';
  approvedDraft = null;
});

elements['agent-form'].addEventListener('submit', async (event) => {
  event.preventDefault();
  elements['agent-form-error'].textContent = '';
  const draft = Object.fromEntries(new FormData(elements['agent-form']));
  draft.budget = Number(draft.budget || 0);
  try {
    const preview = await postJSON('/api/v1/agents/preview', draft);
    approvedDraft = { ...draft, approved_content: preview.content };
    elements['agent-preview-path'].textContent = preview.path;
    elements['agent-preview-content'].textContent = preview.content;
    elements['agent-form'].hidden = true;
    elements['agent-preview'].hidden = false;
  } catch (error) {
    elements['agent-form-error'].textContent = error instanceof Error ? error.message : String(error);
  }
});

elements['create-agent'].addEventListener('click', async () => {
  if (!approvedDraft) return;
  elements['agent-preview-error'].textContent = '';
  elements['create-agent'].disabled = true;
  try {
    await postJSON('/api/v1/agents', approvedDraft);
    closeAgentDialog();
    await refresh();
  } catch (error) {
    elements['agent-preview-error'].textContent = error instanceof Error ? error.message : String(error);
  } finally {
    elements['create-agent'].disabled = false;
  }
});

function showError(message) {
  elements.error.textContent = message;
  elements.freshness.textContent = `${lastSuccessfulRefresh} (stale)`;
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

document.getElementById('close-run').addEventListener('click', () => elements['run-dialog'].close());
document.querySelectorAll('[data-rating]').forEach((button) => {
  button.addEventListener('click', async () => {
    const value = button.dataset.rating;
    const runID = elements['run-job'].textContent;
    const note = elements['run-note'].value.trim();
    elements['run-rating-error'].textContent = '';
    try {
      const job = await fetch(`/api/v1/runs/${encodeURIComponent(runID)}`, { headers: { Accept: 'application/json' } }).then((response) => response.json());
      if (!job.run?.id) throw new Error('Run result is not ready.');
      await postJSON('/api/v1/outcomes', { run_id: job.run.id, value, note });
      elements['run-rating'].hidden = true;
      elements['run-message'].textContent = `Marked ${value}. This signal will improve the next decision.`;
      await refresh();
    } catch (error) {
      elements['run-rating-error'].textContent = error instanceof Error ? error.message : String(error);
    }
  });
});
