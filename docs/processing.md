# Privacy shield and prompt compression

Both run on every request after the router key is checked and before a model
is chosen: compression first, then the privacy shield. They are configured per
account under **Access → Privacy & compression** and are off until you turn
them on. Only counts are stored (values hidden, tokens saved), never content.

## Privacy shield

Private values are replaced with typed, numbered placeholders such as
`[EMAIL_1]` or `[PERSON_2]`. The same value always gets the same placeholder
in a request, so the model can still tell people and accounts apart. A short
system instruction tells the model to keep placeholders as written. On the
way back NeoRouter restores the real values, also in streamed answers (a
placeholder split across chunks is held back until it is complete) and in
tool-call arguments. The mapping exists only in memory for one request.

Each kind of data has a mode:

| Mode | Effect |
| --- | --- |
| Off | Not detected |
| Hide & restore | Hidden from the provider, put back into the answer |
| Hide only | Hidden for good (default for passwords, API keys and cards) |

Detection is layered:

1. **Patterns with validation**: email, phone, IBAN (mod-97 checksum),
   payment cards (issuer prefix, length and Luhn), IPv4.
2. **Secrets**: known key formats (OpenAI, Anthropic, OpenRouter, Groq,
   GitHub, AWS, Google, Stripe, Slack, JWT, private keys, NeoRouter keys),
   `password: …` style assignments in several languages, passwords in URLs,
   and random-looking tokens (Shannon entropy, git hashes excluded).
3. **Your private terms**: names, companies, streets you list.
4. **AI detection (optional)**: a fast model reads new messages and returns
   names, addresses, organizations and ID numbers as exact substrings. It runs
   through NeoRouter's own routing (free models by default), has a 6 s
   deadline and caches results per message, so a conversation's history is
   not analysed again. If it fails, the pattern layers still apply. The
   detection model sees the original text; use a local model (Ollama) or a
   provider you trust.

If the privacy shield fails, the request fails rather than going out
unprotected.

## Compression

| Level | Steps |
| --- | --- |
| Off | Requests are sent as written |
| Lossless | Whitespace clean-up outside code, minified JSON, duplicate blocks dropped |
| Balanced | + repeated log lines collapsed, uniform JSON lists as tables, very long tool output cut to head and tail |
| Aggressive | + older tool output hidden (only the last three stay), older long messages shortened, optional token target |

Rules that always hold:

- System messages and the latest user message only get lossless clean-up.
- Code blocks are never reformatted; images and tool-call structure are never touched.
- Each step is kept only if it makes the request smaller.
- Steps that leave content out only start above a size threshold
  (default 2,000 tokens), so short chats are never shortened.
- If compression fails, the request goes out uncompressed.

The aggressive level follows JetBrains Research's finding that hiding old
tool observations ("observation masking") keeps coding agents about as
accurate as LLM summaries at roughly half the cost
([Lindenbauer et al., 2025](https://arxiv.org/abs/2508.21433)).

A request may lower the level with the header
`X-NeoRouter-Compression: off|lossless|balanced`; it can never raise it.

## Response headers

- `X-NeoRouter-Redacted: 3`: values hidden in this request.
- `X-NeoRouter-Compression: balanced; saved~=1840`: level and estimated tokens saved (chars / 4).
