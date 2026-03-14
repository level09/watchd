from watchd import agent


@agent(every="1h")
def example(ctx):
    """Example agent. Replace with your own logic."""
    ctx.log.info("running")
    return "ok"
