def call(*, prompt, system="", model="claude-sonnet-4-20250514", max_tokens=4096, **kwargs):
    """Call Anthropic's API. Returns the text response."""
    from anthropic import Anthropic

    client = Anthropic()
    kwargs_full = dict(
        model=model,
        messages=[{"role": "user", "content": prompt}],
        max_tokens=max_tokens,
        **kwargs,
    )
    if system:
        kwargs_full["system"] = [{"type": "text", "text": system}]
    msg = client.messages.create(**kwargs_full)
    return msg.content[0].text
