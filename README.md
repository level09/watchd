<div align="center">
  <h1>watchd</h1>
  <h3>Scheduled AI agents with persistent memory. One SQLite file. No infra.</h3>
</div>

<div align="center">
  <a href="https://pypi.org/project/watchd/"><img src="https://img.shields.io/pypi/v/watchd.svg" alt="PyPI"></a>
  <a href="https://pypi.org/project/watchd/"><img src="https://img.shields.io/pypi/pyversions/watchd.svg" alt="Python"></a>
  <a href="https://github.com/level09/watchd/blob/master/LICENSE"><img src="https://img.shields.io/github/license/level09/watchd.svg" alt="License"></a>
</div>

---

Drop a Python file in a directory. Give it a schedule. It runs, remembers what it learned, and picks up where it left off next time. No Docker, no Redis, no queue. One SQLite file holds the schedule, the run history, and the agent state.

## Install

```bash
uv add "watchd[ai]"
```

Or `pip install "watchd[ai]"` if that's more your speed.

## 30-second version

```bash
watchd init          # creates watchd.toml + watchd_agents/
watchd new my_agent  # scaffold a new agent
watchd run my_agent  # run once, see what happens
watchd up            # start all agents on their schedules
```

An agent is just a function with a schedule:

```python
from watchd import agent, every

@agent(schedule=every.hour)
def my_agent(ctx):
    previous = ctx.state.get("last_result")
    # do something, remember it for next time
    ctx.state["last_result"] = "done"
```

`ctx.state` persists between runs. That's the whole trick. A cron job forgets everything. A watchd agent accumulates knowledge.

## Why this exists

Scheduled tasks used to be dumb: check a threshold, send an alert. With LLMs, an agent can read documents, compare against prior runs, spot trends, and take action. But the tooling assumes you want Kubernetes, a message queue, and a deployment pipeline. Sometimes you just want a Python file that runs every hour and remembers things.

## Examples

### Contract compliance

Reads invoices, compares against stored contract terms, flags drift:

```python
from watchd import agent, every
from litellm import completion

@agent(schedule=every.day.at("07:00"))
def contract_compliance(ctx):
    invoices = fetch_new_invoices(since=ctx.state.get("last_check"))
    terms = ctx.state.get("contract_terms", {})

    for inv in invoices:
        resp = completion(model="gpt-4o", messages=[{"role": "user", "content": f"""
            Contract terms for {inv['vendor']}: {terms.get(inv['vendor'], 'unknown')}
            Invoice: {inv['line_items']}
            Flag any line item that exceeds contracted rates or introduces
            charges not covered by the agreement."""}])

        analysis = resp.choices[0].message.content
        if "flag" in analysis.lower() or "exceeds" in analysis.lower():
            ctx.log.error("invoice_discrepancy", vendor=inv["vendor"])
            post_to_slack(f"Invoice issue: {inv['vendor']}\n{analysis}")

    ctx.state["last_check"] = now_iso()
```

### Incident post-mortems

Watches your monitoring stack. When an incident resolves, it pulls logs, metrics, and the alert timeline, drafts a post-mortem. By Monday morning the write-up is already in Confluence.

```python
@agent(schedule=every.minutes(15))
def postmortem_drafter(ctx):
    incidents = fetch_resolved_incidents(since=ctx.state.get("last_seen"))
    drafted = ctx.state.get("drafted", [])

    for inc in incidents:
        if inc["id"] in drafted:
            continue

        logs = fetch_logs(inc["service"], inc["start"], inc["end"])
        metrics = fetch_metrics(inc["service"], inc["start"], inc["end"])

        resp = completion(model="claude-sonnet-4-20250514", messages=[{"role": "user", "content": f"""
            Write a post-mortem for this incident.
            Service: {inc['service']}
            Duration: {inc['start']} to {inc['end']}
            Alert timeline: {inc['alert_history']}
            Logs (last 200 lines): {logs[-5000:]}
            Metrics: {metrics}
            Include: summary, root cause, impact, timeline, action items."""}])

        post_to_confluence(f"Post-mortem: {inc['service']} - {inc['id']}", resp.choices[0].message.content)
        drafted.append(inc["id"])

    ctx.state["drafted"] = drafted[-100:]
    ctx.state["last_seen"] = now_iso()
```

### Churn prediction

Builds a per-customer profile over time. Each week's analysis has more context than the last.

```python
@agent(schedule=every.monday.at("06:00"))
def churn_radar(ctx):
    profiles = ctx.state.get("customer_profiles", {})

    at_risk = []
    for cust in fetch_active_customers():
        tickets = fetch_tickets(cust["id"], days=30)
        usage = fetch_usage_trend(cust["id"], days=90)
        history = profiles.get(cust["id"], "New customer, no prior analysis.")

        resp = completion(model="gpt-4o", messages=[{"role": "user", "content": f"""
            Customer: {cust['name']} ({cust['plan']}, ${cust['mrr']}/mo)
            Previous analysis: {history}
            Recent tickets: {tickets}
            Usage trend (90d): {usage}
            Assess churn risk (low/medium/high). Compare against previous analysis."""}])

        analysis = resp.choices[0].message.content
        profiles[cust["id"]] = analysis
        if "high" in analysis.lower()[:100]:
            at_risk.append({"name": cust["name"], "mrr": cust["mrr"]})

    ctx.state["customer_profiles"] = profiles
    if at_risk:
        total_mrr = sum(c["mrr"] for c in at_risk)
        post_to_slack(f"{len(at_risk)} customers at risk (${total_mrr:,.0f} MRR)")
```

### Security log analysis

Learns what "normal" looks like over weeks, then flags anomalies that rule-based systems miss.

```python
@agent(schedule=every.minutes(10))
def security_analyst(ctx):
    baseline = ctx.state.get("baseline", "No baseline established yet.")
    alert_history = ctx.state.get("alerts", [])
    run_count = ctx.state.get("runs", 0) + 1

    auth_logs = fetch_auth_logs(minutes=10)
    network = fetch_network_events(minutes=10)

    resp = completion(model="claude-sonnet-4-20250514", messages=[{"role": "user", "content": f"""
        Security analyst reviewing the last 10 minutes.
        Established baseline: {baseline}
        Recent alerts: {alert_history[-10:]}
        Auth logs: {auth_logs[-3000:]}
        Network events: {network[-3000:]}
        Flag anything suspicious. Respond with JSON."""}])

    result = parse_json(resp.choices[0].message.content)
    if run_count % 500 == 0:
        ctx.state["baseline"] = result.get("baseline_update", baseline)
    if result.get("suspicious"):
        alert_history.extend(result["findings"])
        page_oncall(result["findings"])

    ctx.state["alerts"] = alert_history[-200:]
    ctx.state["runs"] = run_count
```

### Regulatory tracking

Monitors government websites, diffs against stored versions, routes material changes to the right team.

```python
@agent(schedule=every.day.at("08:00"))
def regulatory_watch(ctx):
    sources = ctx.state.get("sources", REGULATORY_URLS)
    previous = ctx.state.get("previous_content", {})

    for name, url in sources.items():
        current = fetch_page_text(url)
        prev = previous.get(name, "")
        if current == prev:
            continue

        resp = completion(model="gpt-4o", messages=[{"role": "user", "content": f"""
            Regulatory page changed. Source: {name}
            Previous: {prev[:3000]}
            Current: {current[:3000]}
            What changed? Material or cosmetic? Business impact? Urgency?"""}])

        analysis = resp.choices[0].message.content
        previous[name] = current
        if "cosmetic" not in analysis.lower()[:200]:
            post_to_slack(f"Regulatory change: {name}\n{analysis}")

    ctx.state["previous_content"] = previous
```

## Agent context

Every agent gets a `ctx`:

| | |
|---|---|
| `ctx.state` | Persistent dict, SQLite-backed. Survives restarts. |
| `ctx.log` | Structured logger (structlog) |
| `ctx.history` | Last 10 runs for this agent |
| `ctx.agent_name` | Agent name |
| `ctx.run_id` | Current run ID |

## Scheduling

```python
from watchd import every

every.minutes(5)              # every 5 minutes
every.hour                    # every hour
every.day.at("09:00")         # daily at 9 AM
every.monday.at("08:00")      # weekly
every.cron("*/5 * * * *")    # raw crontab
```

## CLI

```
watchd init              scaffold project
watchd new <name>        create agent file
watchd list              show agents + schedules
watchd run <name>        run one now
watchd up                start scheduler
watchd logs <name>       view captured output
watchd history           run history
watchd state <name>      inspect persisted state
```

## How it works

Three things happen at runtime:

1. APScheduler 3.x fires your agents on schedule
2. Each run is logged with status, timing, stdout, and errors
3. State is flushed to SQLite after each execution

That's it. One file, one process.

## Development

```bash
git clone https://github.com/level09/watchd.git
cd watchd
uv sync
uv run pytest
```

## License

MIT
