# watchd

Autonomous AI agents that watch, understand, and act. On a schedule. With memory.

One SQLite file. No Redis, no Docker, no queue.

## Install

```bash
uv add "watchd[ai]"
```

## Quick start

```bash
watchd init          # creates watchd.toml + watchd_agents/
watchd new my_agent  # scaffold a new agent
watchd run my_agent  # run once
watchd up            # start all agents on their schedules
```

## What makes this different

Before LLMs, scheduled tasks were dumb: check a threshold, send an alert. watchd agents **understand context**, **build memory across runs**, and **take intelligent action**. Things that required a team of analysts now fit in a single Python file.

### Contract compliance watchdog

An agent that reads your vendor contracts, cross-references them against incoming invoices, and flags discrepancies. It remembers pricing terms across runs, so it catches slow drift that no human would notice.

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

### Incident post-mortem writer

Watches your monitoring stack. When an incident resolves, it pulls logs, metrics, and the alert timeline, then drafts a post-mortem with root cause analysis, impact summary, and action items. By the time your team opens Slack on Monday, the write-up is already there.

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
        timeline = inc["alert_history"]

        resp = completion(model="claude-sonnet-4-20250514", messages=[{"role": "user", "content": f"""
            Write a post-mortem for this incident.

            Service: {inc['service']}
            Duration: {inc['start']} to {inc['end']}
            Alert timeline: {timeline}
            Logs (last 200 lines): {logs[-5000:]}
            Metrics: {metrics}

            Include: summary, root cause, impact, timeline, action items.
            Be specific. Reference actual log lines and metric values."""}])

        post_to_confluence(f"Post-mortem: {inc['service']} - {inc['id']}", resp.choices[0].message.content)
        drafted.append(inc["id"])

    ctx.state["drafted"] = drafted[-100:]
    ctx.state["last_seen"] = now_iso()
```

### Customer churn predictor

Analyzes support tickets, usage metrics, and billing patterns to identify customers showing early signs of churn. It builds a profile per customer over time, so each week's analysis has more context than the last.

```python
@agent(schedule=every.monday.at("06:00"))
def churn_radar(ctx):
    profiles = ctx.state.get("customer_profiles", {})
    customers = fetch_active_customers()

    at_risk = []
    for cust in customers:
        tickets = fetch_tickets(cust["id"], days=30)
        usage = fetch_usage_trend(cust["id"], days=90)
        history = profiles.get(cust["id"], "New customer, no prior analysis.")

        resp = completion(model="gpt-4o", messages=[{"role": "user", "content": f"""
            Customer: {cust['name']} ({cust['plan']}, ${cust['mrr']}/mo)
            Previous analysis: {history}
            Recent tickets: {tickets}
            Usage trend (90d): {usage}

            Assess churn risk (low/medium/high). Explain your reasoning.
            Compare against your previous analysis: is the trend improving or worsening?"""}])

        analysis = resp.choices[0].message.content
        profiles[cust["id"]] = analysis

        if "high" in analysis.lower()[:100]:
            at_risk.append({"name": cust["name"], "mrr": cust["mrr"], "analysis": analysis})

    ctx.state["customer_profiles"] = profiles
    if at_risk:
        total_mrr = sum(c["mrr"] for c in at_risk)
        post_to_slack(f"{len(at_risk)} customers at risk (${total_mrr:,.0f} MRR)")
```

### Security log analyst

Reads authentication logs, network events, and access patterns. Learns what "normal" looks like for your environment over weeks of observation, then flags anomalies that rule-based systems miss: unusual access sequences, subtle privilege escalation patterns, logins that are technically valid but contextually suspicious.

```python
@agent(schedule=every.minutes(10))
def security_analyst(ctx):
    baseline = ctx.state.get("baseline", "No baseline established yet.")
    alert_history = ctx.state.get("alerts", [])
    run_count = ctx.state.get("runs", 0) + 1

    auth_logs = fetch_auth_logs(minutes=10)
    network = fetch_network_events(minutes=10)

    resp = completion(model="claude-sonnet-4-20250514", messages=[{"role": "user", "content": f"""
        You are a security analyst reviewing the last 10 minutes of activity.

        Established baseline: {baseline}
        Recent alerts you've raised: {alert_history[-10:]}

        Auth logs: {auth_logs[-3000:]}
        Network events: {network[-3000:]}

        Identify anything suspicious. Consider:
        - Access patterns that are technically allowed but contextually unusual
        - Sequences of actions that suggest lateral movement
        - Timing anomalies (off-hours access, rapid sequential logins)
        - Anything that deviates from the established baseline

        Respond with JSON: {{"suspicious": true/false, "findings": [...], "baseline_update": "..."}}"""}])

    result = parse_json(resp.choices[0].message.content)

    if run_count % 500 == 0:
        ctx.state["baseline"] = result.get("baseline_update", baseline)

    if result.get("suspicious"):
        alert_history.extend(result["findings"])
        ctx.log.error("security_alert", findings=result["findings"])
        page_oncall(result["findings"])

    ctx.state["alerts"] = alert_history[-200:]
    ctx.state["runs"] = run_count
```

### Regulatory change tracker

Monitors government and regulatory websites for policy changes relevant to your industry. Compares new language against previous versions it has stored, identifies what changed, assesses business impact, and routes to the right compliance team.

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
            A regulatory page has changed.
            Source: {name}

            Previous version (truncated): {prev[:3000]}
            Current version (truncated): {current[:3000]}

            1. What specifically changed?
            2. Is this a material change or cosmetic (formatting, typos)?
            3. If material: what business functions are affected?
            4. Recommended action and urgency (low/medium/high)."""}])

        analysis = resp.choices[0].message.content
        previous[name] = current

        if "cosmetic" not in analysis.lower()[:200]:
            post_to_slack(f"Regulatory change detected: {name}\n{analysis}")

    ctx.state["previous_content"] = previous
```

## Agent context

Every agent receives a `ctx` object:

- **`ctx.state`** - persistent key/value store across runs (dict-like, SQLite-backed)
- **`ctx.log`** - structured logger
- **`ctx.history`** - last 10 runs for this agent
- **`ctx.agent_name`** / **`ctx.run_id`** - identity

State is the key primitive. It's what turns a script into an agent: each run builds on what the previous run learned.

## Scheduling

```python
from watchd import every

every.minutes(5)                 # every 5 minutes
every.hour                       # every hour
every.day.at("09:00")            # daily at 9 AM
every.monday.at("08:00")         # weekly
every.cron("*/5 * * * *")        # raw crontab
```

## CLI

```bash
watchd init              # scaffold project
watchd new <name>        # create agent file
watchd list              # show agents + schedules
watchd run <name>        # run one now
watchd up                # start scheduler
watchd logs <name>       # view captured output
watchd history           # run history
watchd state <name>      # inspect persisted state
```

## How it works

1. **Scheduler** wraps APScheduler 3.x. Your agents run on their defined schedules.
2. **Run tracker** logs every execution with status, timing, captured stdout, and errors.
3. **State store** gives each agent persistent memory across runs.

Everything lives in one SQLite file. No external services.

## Local development

```bash
git clone https://github.com/level09/watchd.git
cd watchd
uv sync
uv run python -m pytest
uv run watchd init
uv run watchd run example
```

## License

MIT
