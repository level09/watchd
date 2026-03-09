def call(*, prompt, system="", model="claude-sonnet-4-20250514", max_tokens=4096, **kwargs):
    """Call Anthropic's API. Returns the text response."""
    from anthropic import Anthropic

    client = Anthropic()
    msg = client.messages.create(
        model=model,
        system=system or None,
        messages=[{"role": "user", "content": prompt}],
        max_tokens=max_tokens,
        **kwargs,
    )
    return msg.content[0].text
