# CPA Free-Tier Plugin

<p align="center">
  <img src="https://img.shields.io/badge/CLIProxyAPI-ABI%20v1%20Compliant-00ADD8?style=for-the-badge&logo=go" alt="CPA ABI v1" />
  <img src="https://img.shields.io/badge/Platforms-Linux%20%7C%20Darwin%20%7C%20Windows-4A154B?style=for-the-badge" alt="Cross Platform" />
  <img src="https://img.shields.io/badge/Release-v1.3.0-success?style=for-the-badge" alt="Release" />
  <img src="https://img.shields.io/badge/License-MIT-blue?style=for-the-badge" alt="License MIT" />
</p>

A high-performance, native dynamic C ABI plugin (`.so` / `.dylib` / `.dll`) for **[CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI)** (v7+). It gives you out-of-the-box access to curated, **100% free frontier AI models** without requiring user accounts, API keys, credits, or subscriptions.

---

## 🌟 Key Features

* **Zero-Key & Zero-Config:** Ready to use immediately. No account generation, credit cards, or API keys required.
* **OpenCode v2 Wire Protocol:** Fully compliant with OpenCode's v2 CLI contract (`opencode/latest/2.0.32/cli`), descending session affinity, and automatic tool declaration spoofing to prevent `403 FreeTierError`.
* **Universal Agent Compatibility:** Built-in `request_normalizer` engine that automatically strips unsupported parameters (`thinking`, `enable_thinking`, `thinking_budget`, `thinking_config`, `search`, `web_search`, `return_citations`) sent by coding agents like **Hermes Agent**, **Cursor**, **Claude Code**, and **Aider**. `reasoning_effort` is deliberately **kept** — the upstream gateway accepts it and it controls how much the model reasons. Custom fields via `CPA_STRIP_FIELDS` (add) and `CPA_KEEP_FIELDS` (re-enable).
* **Dual Execution Modes:**
  * **Native Zero-Buffering Streaming:** Emits raw SSE chunks via host callbacks (`host.stream.emit`) with clean termination on `[DONE]`.
  * **Intelligent Non-Streaming Assembler:** Seamlessly translates streaming upstream responses into full OpenAI-standard `chat.completion` JSON payloads.
* **Full Multi-Platform Coverage:** First-class pre-compiled binaries for **Linux** (`amd64`, `arm64`), **macOS / Apple Silicon** (`darwin_arm64`, `darwin_amd64`), and **Windows** (`windows_amd64 .dll`).
* **Resilient Network & Proxy Support:** Operates 100% direct out-of-the-box. Includes support for custom residential/egress proxies via `OPENCODE_PROXY` alongside non-blocking local proxy pool probing.

---

## 📋 Supported Free Models

### 1. OpenCode Zen Frontier Models (Zero-Key)
| Model Identifier | Parameter Size / Specialization | Context Window | Max Output |
| :--- | :--- | :--- | :--- |
| `flint-40b-free` | 40B MoE coding agent specialist (~2.5B active) with reasoning | **262,144** | 65,536 |
| `nemotron-3-ultra-free` | 1M context open-weight reasoning flagship (OpenCode) | **1,000,000** | 128,000 |
| `glm-5.3-flash-free` | Flagship multimodal agentic reasoning model | **200,000** | 32,768 |
| `big-pickle` | Frontier deliberate deep-thought reasoning model | **200,000** | 32,000 |
| `deepseek-v4.1-flash-free` | DeepSeek V4.1 Flash reasoning & code specialist | **1,000,000** | 384,000 |
| `mimo-v2.6-flash-free` | Xiaomi MiMo 2.6 Flash reasoning & instruction model | **262,144** | 65,536 |
| `beam-free` | Low-latency versatile general chat model | **200,000** | 32,768 |
| `nemotron-3.5-super-vl-free` | Multimodal vision-language reasoning model | **1,000,000** | 65,536 |
| `cat-jam-free` | Agentic text generation & coding specialist | **1,048,576** | 131,072 |
| `nemotron-3.5-lightning-free`| Low-latency, high-throughput code helper | **1,000,000** | 65,536 |

> **Note on retired ids:** `minimax-m3.1-flash-free`, `exo-free`, `space-bunny-free`, `muse-spark-1.2-contributor-free`, and `muse-spark-1.3-contributor-free` were retired upstream (`410 ModelDeprecated` / `400 unknown model`). `longcat-2.5-preview-free` is currently unavailable upstream. `scripts/check-models.sh` guards against re-shipping ids that vanished.

> **Note on `jev-1.13-free`:** While present in OpenCode's backend catalog, `jev-1.13-free` rejects standard chat completions upstream (`ModelProtocolUnsupported`) and is omitted from the chat provider catalog.

### 2. Kilo AI Curated Models
| Model Identifier | Specialization | Context Window | Max Output |
| :--- | :--- | :--- | :--- |
| `kilo-auto/free` | Smart router across active free pools | **256,000** | 32,768 |
| `nvidia/nemotron-3-ultra-550b-a55b:free` | 550B flagship frontier reasoning model | **1,000,000** | 65,536 |
| `cohere/north-mini-code:free` | Fast syntax, code completion & linting | **256,000** | 64,000 |
| `openrouter/free` | Community-curated fallback pool | **200,000** | 65,536 |
| `nvidia/nemotron-3-nano-omni-30b-a3b-reasoning:free` | Compact multimodal vision & reasoning | **256,000** | 65,536 |
| `nvidia/nemotron-3.5-lightning:free` | Ultra-fast general assistant model | **1,000,000** | 65,536 |
| `nvidia/nemotron-3.5-super-vl:free` | Multimodal vision-language reasoning | **1,000,000** | 131,072 |
| `poolside/laguna-s-2.1:free` | Code synthesis & refactoring specialist | **262,144** | 32,768 |
| `poolside/laguna-xs-2.1:free` | Lightweight low-latency code assistant | **262,144** | 32,768 |
| `dots-studio/dots-3-note-preview:free` | 512k context document summarization | **512,000** | 460,800 |
| `liquid/lfm-2.5-2.6b:free` | Liquid Neural Network adaptive model | **65,536** | 8,192 |
| `reflection/beam-free` | 1M context general assistant model | **1,048,576** | 131,072 |
| `xingchen-agi/xing4.0-29b-a4b:free` | 29B conversational reasoning model | **262,144** | 235,929 |
| `nvidia/nemotron-3.5-content-safety:free` | Safety classification & guardrails | **128,000** | 8,192 |

> **Note on retired Kilo ids:** `minimax/minimax-m3.1-flash:free`, `thinkingmachines/inkling-small:free` and `nvidia/nemotron-3-super-120b-a12b:free` were removed upstream (`404 The requested model is currently unavailable`) and are no longer advertised. `scripts/check-models.sh` guards against re-shipping ids that vanished.

---

## 🚀 Installation & Setup

### Method 1: CLIProxyAPI Plugin Store (1-Click Install)

1. Open your CLIProxyAPI Management Center (`#/plugin-store`).
2. Search for **CPA Free-Tier Models** and click **Install**.
3. **That's it!** The plugin automatically generates the provider descriptor (`~/.cli-proxy-api/cpa-freetier-plugin.json`) upon initialization.
4. Restart CLIProxyAPI if prompted.

---

### Method 2: Manual Installation (All Platforms)

Download the verified platform package from [Releases](https://github.com/tsaQB/cpa-freetier-plugin/releases/latest):

#### 🐧 Linux (x86_64 / amd64)
```bash
mkdir -p ~/.cli-proxy-api/plugins
curl -sL https://github.com/tsaQB/cpa-freetier-plugin/releases/download/v1.3.0/cpa-freetier-plugin_1.3.0_linux_amd64.zip -o /tmp/plugin.zip
unzip -o /tmp/plugin.zip -d ~/.cli-proxy-api/plugins/
rm /tmp/plugin.zip
```

#### 🐧 Linux (ARM64 / aarch64)
```bash
mkdir -p ~/.cli-proxy-api/plugins
curl -sL https://github.com/tsaQB/cpa-freetier-plugin/releases/download/v1.3.0/cpa-freetier-plugin_1.3.0_linux_arm64.zip -o /tmp/plugin.zip
unzip -o /tmp/plugin.zip -d ~/.cli-proxy-api/plugins/
rm /tmp/plugin.zip
```

#### 🍏 macOS (Apple Silicon M1/M2/M3/M4)
```bash
mkdir -p ~/.cli-proxy-api/plugins
curl -sL https://github.com/tsaQB/cpa-freetier-plugin/releases/download/v1.3.0/cpa-freetier-plugin_1.3.0_darwin_arm64.zip -o /tmp/plugin.zip
unzip -o /tmp/plugin.zip -d ~/.cli-proxy-api/plugins/
rm /tmp/plugin.zip
```

#### 🍏 macOS (Intel x86_64)
```bash
mkdir -p ~/.cli-proxy-api/plugins
curl -sL https://github.com/tsaQB/cpa-freetier-plugin/releases/download/v1.3.0/cpa-freetier-plugin_1.3.0_darwin_amd64.zip -o /tmp/plugin.zip
unzip -o /tmp/plugin.zip -d ~/.cli-proxy-api/plugins/
rm /tmp/plugin.zip
```

#### 🪟 Windows (x86_64)
Extract `cpa-freetier-plugin_1.3.0_windows_amd64.zip` into your configured `plugins/` directory so that `cpa-freetier-plugin.dll` resides next to `cli-proxy-api.exe`.

#### Register Provider Descriptor & Enable
In your `~/.cli-proxy-api/` directory, create `cpa-freetier-plugin.json`:
```json
{
  "disabled": false,
  "provider": "cpa-freetier-plugin",
  "type": "cpa-freetier-plugin",
  "excluded_models": [
    "muse*"
  ]
}
```

> **Note on Model Exclusion:** You can exclude unstable or undesired models from `/v1/models` and prevent routing them upstream by specifying `"excluded_models"` in `cpa-freetier-plugin.json` (supports wildcards/globs like `"muse*"` or full IDs) or via environment variable `CPA_EXCLUDED_MODELS="muse*,kilo-auto/free"`.
Enable the plugin in `config.yaml`:
```yaml
plugins:
  enabled: true
  dir: "plugins" # or full path to directory
  configs:
    cpa-freetier-plugin:
      enabled: true
```

---

## 💡 How to Make OpenCode Free Models Work (Bypass 403 & 429 Errors)

OpenCode's free tier is zero-key, but its upstream Cloudflare WAF applies strict network and client heuristics. Follow these guidelines depending on your setup:

### 🏠 1. Running on a Laptop, Home PC, or Residential Broadband
* **Direct Out-of-the-Box:** It connects directly through your host network without needing any proxy. Cloudflare accepts residential broadband IPs seamlessly.
* **Pro-tip:** If your machine doesn't have a local proxy pool running, set `OPENCODE_PROXY_POOL=""` in your environment to skip loopback probe checks and connect immediately.

### ☁️ 2. Running on a Cloud VPS / Datacenter (AWS, Hetzner, DO, etc.)
* **The Error:** `403 Forbidden` (`FreeTierError: OpenCode's free tier can only be used from within OpenCode`).
* **Root Cause:** Cloudflare WAF blocks datacenter Autonomous System Numbers (ASNs).
* **The Fix:** Route OpenCode outbound requests through a residential, mobile (4G/LTE), or home proxy using `OPENCODE_PROXY`:
  ```bash
  export OPENCODE_PROXY="http://user:password@residential-proxy.net:8080"
  # or SOCKS5h:
  export OPENCODE_PROXY="socks5h://127.0.0.1:1080"
  ```
  *(Note: Kilo AI models work direct from anywhere and do not require residential proxies).*

### 🛡️ 3. Upstream TLS Fingerprinting & Rate Limits (429 Bypass)
* **New in v1.3.0:** The plugin automatically detects if a local Bun sidecar TLS bridge is running on `127.0.0.1:8789`. If present, it routes OpenCode traffic locally through the sidecar, completely bypassing HTTP client TLS-fingerprint blocks.
* **Custom Sidecar:** You can also point to any custom sidecar gateway via:
  ```bash
  export OPENCODE_BASE_URL="http://127.0.0.1:8789"
  ```

### 🤖 4. Coding Agent Payload Compatibility (Cursor, Cline, Hermes, Synaplan)
* **New in v1.3.0:** Full automatic message role normalization (`role: ""` -> `"user"`, `developer` -> `"system"`, nil content repair) to eliminate `failed to convert messages` errors.
* Unsupported agent parameters (`thinking_budget`, `enable_thinking`, `search`) are automatically stripped, while official `reasoning_effort` is preserved for frontier models like `flint-40b-free`.

---

## 🌐 Optional Outbound Proxy Support

The plugin **works 100% direct out-of-the-box without requiring any proxy**.

However, if your host is hosted in a restricted cloud datacenter and you want to route OpenCode requests through custom residential, mobile, or SOCKS5 proxies to eliminate rate-limits, define `OPENCODE_PROXY`:

```bash
# Supports HTTP, HTTPS, or SOCKS5 proxies
export OPENCODE_PROXY="http://127.0.0.1:8080"
# or
export OPENCODE_PROXY="socks5h://127.0.0.1:1080"
```

If `OPENCODE_PROXY` is unset, the plugin probes a residential/local pool and otherwise falls back to the host's standard network interface.

> **Datacenter egress is opt-in.** The OpenCode free tier rejects datacenter ASNs with `403 FreeTierError`, so the built-in Xray chains are **not** used as an OpenCode fallback. If you really want them, set `OPENCODE_ALLOW_DATACENTER=1` — expect 403s.

---

## 🛠 Environment Variables

| Variable | Default | Purpose |
| :--- | :--- | :--- |
| `OPENCODE_PROXY` | *(unset)* | Explicit egress for OpenCode (`http://`, `socks5h://`). Wins over the pools below. |
| `OPENCODE_PROXY_POOL` | `127.0.0.1:8127` | Ordered candidates probed before falling back to the host network. Comma-separated `host:port` or full URLs. **Set it to an empty value to disable probing entirely** (recommended on a laptop/VPS that has no local proxy). |
| `OPENCODE_DATACENTER_PROXY_POOL` | `127.0.0.1:8123..8126` | Only consulted when `OPENCODE_ALLOW_DATACENTER=1`. |
| `OPENCODE_ALLOW_DATACENTER` | `0` | Allow datacenter egress for OpenCode. Off because it is rejected upstream. |
| `CPA_FREETIER_LOG` | `<home>/.cli-proxy-api/logs/cpa-freetier-plugin.log` | Log destination (portable; falls back to `os.TempDir()` only if no home directory is resolvable). |
| `CPA_FREETIER_DEBUG` | `0` | Verbose diagnostics. Request **bodies are never logged**, with or without this. |
| `CPA_STRIP_FIELDS` | *(unset)* | Extra comma-separated request fields to strip before forwarding. |
| `CPA_KEEP_FIELDS` | *(unset)* | Comma-separated request fields to keep even if they are in the default strip list. Wins over `CPA_STRIP_FIELDS`. |
| `CPA_NO_AUTH_DESCRIPTOR` | `0` | Set to `1` to stop the plugin writing the provider descriptor file. |
| `OPENCODE_BASE_URL` | *(unset)* | Override the OpenCode base URL. |

Logs rotate at 8 MiB with a single `.1` generation kept, so the file cannot grow without bound. The log file is created `0600`.

---

## 🩺 Verifying the Catalogue

Two scripts, because catalogue presence is not liveness:

| Command | What it does |
| :--- | :--- |
| `make check-models` | Diffs the advertised ids against the live upstream catalogues. Fails the build when an id vanished from the catalogue. |
| `make probe-models PROBE_PROXY=http://127.0.0.1:8127` | **Calls** every advertised model and classifies the answer: alive / dead / inconclusive. Fails the build on a dead id. |

`check-models` alone is not enough: `nvidia/nemotron-3-super-120b-a12b:free` was still itemised in the Kilo catalogue while every live call returned `404 The requested model is currently unavailable`. The probe is what catches that class of drift.

`probe-models` needs **residential** egress for real verdicts — the free tier answers datacenter ASNs with `403 FreeTierError`, which is reported as *inconclusive* rather than a false "dead". `PROBE_PROXY` accepts a comma-separated list and rotates through it, so one rate-limited egress does not paint the whole catalogue grey.

---

## 💻 Running on a Laptop or Your Own VPS

**Nothing extra is required.** The plugin has no dependency on this project's lab
setup:

* With no proxy configured it connects directly through the host's normal
  network path (respecting `HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY`).
* The built-in proxy pools are loopback addresses that simply do not answer
  elsewhere; the probe fails and the request goes direct. If you would rather not
  even probe, set `OPENCODE_PROXY_POOL=` (empty).
* The log lands in your home directory, not in `/tmp`, and rotates.

If you see `403 FreeTierError` from a model, that is an **egress-IP** decision by
OpenCode — the free tier refuses hosts on datacenter ASNs. The fix is a
residential/mobile egress, not a plugin setting:

```bash
export OPENCODE_PROXY="http://user:pass@your-residential-proxy:8080"
# probing a pool instead of a single proxy
export OPENCODE_PROXY_POOL="127.0.0.1:8127,proxy.example.net:3128"
```

If you see `500 Internal Server Error` with an empty body and an
`x-opencode-log-id` header, that is the upstream gateway failing — the plugin
reports it verbatim and the model is left alone.

---

## 🔒 Reliability Notes (v1.3.0)

* **OpenCode v2 Gate Bypass & Sidecar Routing:** Resolves the upstream `429 FreeUsageLimitError` caused by OpenCode's October 2026 TLS-fingerprinting and harness validation. The plugin now automatically checks for a local Bun sidecar running on `127.0.0.1:8789` (or custom via `OPENCODE_BASE_URL`) and routes OpenCode requests to it locally, preserving residential proxy egress.
* **Empty Role Sanitization:** Sanitizes client messages with empty or invalid roles (`developer` -> `system`, missing/empty -> `user`) to prevent upstream schema rejection.
* **Operator Model Exclusion:** Supports `CPA_EXCLUDED_MODELS` (glob/comma-separated) to selectively disable specific free models without rebuilding the binary.
* **Bounded logging.** Request bodies are replaced by a length + short SHA-256 prefix. The previous build wrote the full conversation to a fixed path with no cap — on a 512 MiB tmpfs that reached 393 MiB in ~90 minutes.
* **Connection reuse.** Transports are process-wide and cached per egress, with `ResponseHeaderTimeout` so a hung upstream cannot pin a goroutine forever.
* **Correct fallback.** Datacenter proxies are no longer used for OpenCode; the reachability probe is cached for 30s instead of dialing on every request.
* **Model health.** A model the upstream reports as missing (`404` / "does not exist" / "unknown provider") is temporarily hidden from the published catalog and restored automatically when it answers again.
* **Bounded SSE lines.** A single `data:` line may reach 16 MiB; the old 2 MiB scanner limit aborted legitimately large tool-call streams.
* **Exactly-once stream close**, session-cache sweeping, and size-safe C buffer copies.
* **`reasoning_effort` is forwarded.** The upstream gateway accepts it on every advertised model and it measurably changes how much the model reasons, so the sanitizer no longer removes it. Parameters the upstream genuinely rejects (`enable_thinking`, `thinking_budget`, …) are still stripped, and `CPA_KEEP_FIELDS` re-enables any of them for a deployment whose upstream tolerates them.
* **Live liveness probe.** `make probe-models` calls every advertised model and verifies actual response (200 OK) instead of trusting catalogue listings alone.

---

## 🧪 Verification & Usage Example

Test your deployment via `curl` against your local CLIProxyAPI instance (`:8317`):

### Streaming Call
```bash
curl -N http://127.0.0.1:8317/v1/chat/completions \
  -H "Authorization: Bearer YOUR_CPA_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "big-pickle",
    "messages": [{"role": "user", "content": "Explain quantum tunneling in two sentences."}],
    "stream": true
  }'
```

### Python OpenAI SDK
```python
import os
from openai import OpenAI

client = OpenAI(
    base_url="http://127.0.0.1:8317/v1",
    api_key=os.getenv("CLIPROXY_API_KEY", "your-cpa-key")
)

response = client.chat.completions.create(
    model="glm-5.3-flash-free",
    messages=[{"role": "user", "content": "Hello world!"}],
    stream=True
)

for chunk in response:
    if chunk.choices and chunk.choices[0].delta.content:
        print(chunk.choices[0].delta.content, end="", flush=True)
print()
```

---

## 🏗️ Building From Source

Prerequisites: Go 1.22+ and GCC toolchains.

```bash
# Clone the repository
git clone https://github.com/tsaQB/cpa-freetier-plugin.git
cd cpa-freetier-plugin

# Build Linux AMD64 (.so)
make build-linux-amd64

# Build Linux ARM64 (.so)
make build-linux-arm64

# Build Windows AMD64 (.dll)
make build-windows-amd64
```

---

## 📄 License

Distributed under the **MIT License**. See `LICENSE` for more information.
