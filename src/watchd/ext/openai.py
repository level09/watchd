def call(*, prompt, system="", model="gpt-4o", max_tokens=4096, **kwargs):
    """Call OpenAI's API. Returns the text response."""
    from openai import OpenAI

    client = OpenAI()
    messages = []
    if system:
        messages.append({"role": "system", "content": system})
    messages.append({"role": "user", "content": prompt})
    resp = client.chat.completions.create(
        model=model,
        messages=messages,
        max_tokens=max_tokens,
        **kwargs,
    )
    return resp.choices[0].message.content
