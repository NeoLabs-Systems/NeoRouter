<p align="center">
  <img src=".github/banner.jpg" alt="NeoRouter: every model, one endpoint" width="100%">
</p>

<h1 align="center">NeoRouter</h1>

<p align="center"><strong>Every model. One endpoint.</strong></p>

Browser and coarse server analytics are enabled by default and can be disabled; see [analytics](docs/analytics.md).

<p align="center">
  Connect any inference provider, from free tiers to paid APIs and your own servers.<br>
  NeoRouter checks every key, imports every model and routes each request to the best one right now.
</p>

<p align="center">
  <a href="https://github.com/NeoLabs-Systems/NeoRouter/releases/latest"><img alt="Latest release" src="https://img.shields.io/github/v/release/NeoLabs-Systems/NeoRouter?style=flat-square&label=release&color=111827"></a>
  <a href="LICENSE"><img alt="AGPL-3.0 license" src="https://img.shields.io/badge/license-AGPL--3.0-a855f7?style=flat-square"></a>
  <a href="https://nodejs.org"><img alt="Node.js 20 or newer" src="https://img.shields.io/badge/node-20%2B-5fa04e?style=flat-square&logo=nodedotjs&logoColor=white"></a>
  <a href="https://neolabs-systems.github.io/NeoRouter/api/"><img alt="API docs: OpenAI and Anthropic compatible" src="https://img.shields.io/badge/API%20docs-OpenAI%20%C2%B7%20Anthropic%20%C2%B7%20Responses-e3b655?style=flat-square"></a>
</p>

<p align="center">
  <a href="https://discord.gg/f59rg2RwUT"><img src="https://img.shields.io/badge/Join%20NeoLabs-Discord-5865F2?style=for-the-badge&amp;logo=discord&amp;logoColor=white" alt="Join the NeoLabs Discord"></a>
</p>

https://github.com/user-attachments/assets/f42be679-1e12-4cb1-9c00-d0f3c15bf403


<table>
  <tr>
    <td width="33%" valign="top">
      <h3>🔌 80+ providers</h3>
      Free tiers, paid APIs, aggregators,<br>
      local Ollama and any custom endpoint.
    </td>
    <td width="33%" valign="top">
      <h3>🧠 Learns as it routes</h3>
      <code>model: "auto"</code> picks by quality,<br>
      speed, reliability and price.
    </td>
    <td width="33%" valign="top">
      <h3>💸 Strictly free if you want</h3>
      Free-only keys never touch a paid model.<br>
      Budgets stop paid keys at the limit.
    </td>
  </tr>
</table>

---

## 🚀 Install

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/NeoLabs-Systems/NeoRouter/main/install.sh)
```

or with npm:

```bash
npm i -g --allow-git=root --allow-scripts=bcrypt,better-sqlite3,onnxruntime-node,protobufjs github:NeoLabs-Systems/NeoRouter && neorouter install
```

On fish, wrap the first command: `bash -c 'bash <(curl -fsSL https://raw.githubusercontent.com/NeoLabs-Systems/NeoRouter/main/install.sh)'`. Afterwards the `neorouter` command works in a new terminal (`neorouter update`, `neorouter doctor`, …); if your shell does not pick up `~/.local/bin` yet, the installer prints the one line to add it.

You need Node.js 20+ and git. The web app comes prebuilt; if it cannot be downloaded, the installer sets up Flutter for you and builds it.

Then open **http://localhost:4600/app**. The first account you create becomes the administrator. Add a provider, create a router key, and point any OpenAI or Anthropic SDK at `http://localhost:4600/v1`.

```python
from openai import OpenAI

client = OpenAI(base_url="http://localhost:4600/v1", api_key="nrt_…")
client.chat.completions.create(model="auto:fast", messages=[{"role": "user", "content": "Hi"}])
```

```bash
ANTHROPIC_BASE_URL=http://localhost:4600 ANTHROPIC_AUTH_TOKEN=nrt_… claude
```

## ✨ What it does

<table>
  <tr>
    <td width="33%" valign="top"><b>🔑 Keys checked on the spot</b><br>Every key is validated, encrypted and its models imported automatically. OpenRouter connects with one click.</td>
    <td width="33%" valign="top"><b>🗂️ Live model catalog</b><br>Prices, context sizes and an intelligence estimate from public metadata, refreshed in the background.</td>
    <td width="33%" valign="top"><b>🧭 Auto router</b><br>Presets from <i>most reliable</i> to <i>smartest</i> and <i>cheapest</i>, or your own weights and limits.</td>
  </tr>
  <tr>
    <td valign="top"><b>🩹 Self-healing</b><br>Errors, rate limits and slow answers earn penalties and cooldowns. Hanging providers are hedged, failures fall back silently.</td>
    <td valign="top"><b>⚖️ Spreads the load</b><br>Bursts are distributed across models and queue briefly when every free tier is saturated.</td>
    <td valign="top"><b>📊 Quotas and analytics</b><br>Free-tier limits learned from headers and errors. Charts for traffic, latency, tokens, cost and providers.</td>
  </tr>
  <tr>
    <td valign="top"><b>🎟️ Router keys</b><br>Free-only, paid or both. USD budgets per day, month or total, RPM limits and model allow-lists.</td>
    <td valign="top"><b>🔀 Three APIs</b><br>OpenAI Chat Completions, Anthropic Messages and OpenAI Responses, with streaming, tools and vision.</td>
    <td valign="top"><b>👥 Multi-user</b><br>Provider keys stay private per account, with optional authenticator 2FA. Only request metadata is logged, never prompts.</td>
  </tr>
  <tr>
    <td valign="top"><b>🛡️ Privacy shield</b><br>Emails, IBANs, cards, secrets, your own terms and (optionally AI-detected) names become placeholders; answers get the real values back.</td>
    <td valign="top"><b>🗜️ Prompt compression</b><br>Lossless, balanced or aggressive: minified JSON, trimmed tool output and masked old observations, never touching code or your latest message.</td>
    <td valign="top"><b>💸 Savings</b><br>See what free models saved you and how many tokens compression removed.</td>
  </tr>
  <tr>
    <td valign="top"><b>🧬 Embeddings</b><br><code>/v1/embeddings</code> across providers. "auto" pins each key to one model so vectors stay comparable.</td>
    <td valign="top"><b>🎨 Images, speech, video</b><br>Image generation, text to speech, transcription with subtitles and video jobs, all routed with fallback.</td>
    <td valign="top"><b>⚡ System One models</b><br>Fast guard models (Llama Guard, NemoGuard, gpt-oss-safeguard, Prompt Guard) behind one <code>/v1/moderations</code>.</td>
  </tr>
  <tr>
    <td valign="top"><b>🔎 Web search</b><br>Tavily, Exa, Linkup, Parallel, Firecrawl, Serper, Brave, Jina and your own SearXNG behind <code>/v1/search</code>, with fallback.</td>
    <td valign="top"><b>🌐 Search for any model</b><br>Turn on web search in OpenAI, OpenRouter, Responses or Anthropic style; NeoRouter runs the searches for whichever model answers.</td>
    <td valign="top"><b>🔑 Several keys per provider</b><br>Connect two Groq keys (or any provider twice) and NeoRouter spreads requests by allowance left and learned concurrency limits.</td>
  </tr>
</table>

**Subscriptions:** ChatGPT (Plus/Pro, through the Codex sign-in), Ollama Cloud Pro/Max, OpenCode Go, Synthetic, MiniMax M Plan and Kimi Code as prepaid plans: used first while the allowance lasts, then stopped or billed per use.

**Local models:** admins can run text, speech, transcription, embedding and safety models on the server itself; NeoRouter shows what fits the hardware, then downloads, runs and repairs them.

**Search engines:** Tavily, Exa, Linkup, Parallel, Firecrawl, Serper, Brave Search, Jina and self-hosted SearXNG.

**Providers:** Groq, Google Gemini, Deepgram, ElevenLabs, Cerebras, Mistral, OpenRouter, NVIDIA NIM, Cloudflare, Cohere, Z.ai, SiliconFlow, Hugging Face, Ollama Cloud, Kilo, OpenAI, Anthropic, xAI, DeepSeek, Together, Fireworks, DeepInfra, Nous, local Ollama and LM Studio, and any OpenAI- or Anthropic-compatible endpoint.

## 📚 Docs

| Start | Understand | Extend |
| --- | --- | --- |
| [Configuration](docs/configuration.md) | [Routing](docs/routing.md) | [Providers](docs/providers.md) |
| [CLI](#-cli) | [Security](SECURITY.md) | [Contributing](CONTRIBUTING.md) |

## 🛠️ CLI

`neorouter install · start · stop · restart · status · doctor · logs · backup · update · env · reset-password · reset-2fa · providers import · rebuild-web · version`

## 🧪 Project status

- **Beta.** Expect breaking changes.
- **Help.** [Issues](https://github.com/NeoLabs-Systems/NeoRouter/issues) for bugs, [SECURITY.md](SECURITY.md) for vulnerabilities.

## 📄 License

[AGPL-3.0-only](LICENSE).

<p align="center"><sub>Made with ❤️ by <a href="https://github.com/neooriginal">Neo</a> · <a href="https://github.com/NeoLabs-Systems">NeoLabs Systems</a></sub></p>

### Outbound proxies

In **Admin → Outbound proxies**, import proxies, test connections, then enable
rotation. This is off by default. SOCKS5 (including `socks5h` URLs), HTTP and
HTTPS CONNECT proxies are supported, with optional username/password credentials.
Paste a list or load a UTF-8 text, CSV or TSV file, up to 5,000 lines per import.
Accepted entries are proxy URLs, `user:password@host:port`, `host:port`,
`host:port:user:password`, or comma/tab-separated host, port, user, password.
Entries without a scheme use the selected default protocol. Use URL syntax for
IPv6 addresses and URL-encode special characters in URL credentials. Blank lines
and `#` comments are ignored; duplicates are skipped and invalid lines remain
in the editor for correction. Simple CSV/TSV columns are supported, without a
header or quoted fields.

The shared provider HTTP transport uses the pool, including streamed responses.
Local/private destinations stay direct; relay and WebSocket transports are
unchanged. **Allow direct fallback** controls whether public provider requests
can use the server connection when no proxy can connect. Disable it to require
proxies for these requests. Test URLs must be public HTTP(S) addresses, with no
credentials; tests send GET without provider keys.

Selection spreads requests using measured latency, active requests (including
streams) and recent connection failures. Failed proxies cool down exponentially
from 30 seconds to 15 minutes, then become eligible again; a successful manual
test restores them immediately. There are at most three proxy attempts per
request, with eight-second timeouts for connection/handshake stages. Requests that may
already have reached an upstream are never automatically replayed through
another proxy or direct fallback. Provider HTTP errors such as 429 do not mark
a working proxy unhealthy. Long response bodies stream with backpressure;
retained dispatchers are bounded and idle ones are evicted. Live health resets
on restart; proxy configuration persists and credentials are encrypted with the
instance master key and omitted from API responses.

### Benchmarks and model news

**Overview → Benchmarks** brings together measured results already collected
from Artificial Analysis, OpenRouter benchmarks, Artificial Analysis media
arenas and MTEB. Search and filter by model kind, source or confirmed free
listing, then select two models for a side-by-side comparison. Original scores,
source links, collection dates and coverage remain visible. No routing guesses
or derived intelligence estimates are presented as benchmark measurements.

The combined score averages test percentiles within each source, then gives
sources equal weight. Percentiles use only the same model kind and test; missing
measurements do not become zero. This is a relative ranking over the collected
models, not accuracy. Models with different coverage are best compared on their
shared tests. The blurred NeoRouter section is a labelled preview, with no
internal benchmark scoring or synthetic results.

**Overview → News** highlights confirmed zero-price models in OpenRouter's
public catalog that reach the top half of currently benchmarked free models.
All published price components must be known zero. Unknown prices and models
without measured evidence are excluded. First detection is stored once per
canonical model, independently of provider aliases; it means first highlighted
by this instance, not first released. Historical entries remain when a model is
no longer confirmed free. News is updated after metadata collection and survives
restarts; page refresh reloads collected data without triggering external work.
The timeline is loaded in bounded pages against a stable snapshot. Both pages
respect team provider-view permissions and reduced-motion preferences.
