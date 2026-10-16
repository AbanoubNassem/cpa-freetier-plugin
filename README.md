# CPA Free-Tier Plugin

<p align="center">
  <img src="https://img.shields.io/badge/CLIProxyAPI-ABI%20v1%20Compliant-00ADD8?style=for-the-badge&logo=go" alt="CPA ABI v1" />
  <img src="https://img.shields.io/badge/Platforms-Linux%20%7C%20Darwin%20%7C%20Windows-4A154B?style=for-the-badge" alt="Cross Platform" />
  <img src="https://img.shields.io/badge/Release-v1.2.1-success?style=for-the-badge" alt="Release" />
  <img src="https://img.shields.io/badge/License-MIT-blue?style=for-the-badge" alt="License MIT" />
</p>

A high-performance, native dynamic C ABI plugin (`.so` / `.dylib` / `.dll`) for **[CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI)** (v7+). It gives you out-of-the-box access to curated, **100% free frontier AI models** without requiring user accounts, API keys, credits, or subscriptions.

---

## 🌟 Key Features

* **Zero-Key & Zero-Config:** Ready to use immediately. No account generation, credit cards, or API keys required.
* **OpenCode v2 Wire Protocol:** Fully compliant with OpenCode's v2 CLI contract (`opencode/latest/2.0.26/cli`), descending session affinity, and automatic tool declaration spoofing to prevent `403 FreeTierError`.
* **Universal Agent Compatibility:** Built-in `request_normalizer` engine that automatically strips unsupported parameters (`reasoning_effort`, `thinking`) sent by coding agents like **Hermes Agent**, **Cursor**, **Claude Code**, and **Aider**.
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
| `glm-5.3-flash-free` | Flagship multimodal agentic reasoning model | **200,000** | 32,768 |
| `big-pickle` | Frontier deliberate deep-thought reasoning model | **200,000** | 32,000 |
| `deepseek-v4.1-flash-free` | DeepSeek V4.1 Flash reasoning & code specialist | **1,000,000** | 384,000 |
| `space-bunny-free` | Ultra-long context frontier model | **1,048,576** | 131,072 |
| `cat-jam-free` | Agentic text generation & coding specialist | **1,048,576** | 131,072 |
| `nemotron-3.5-lightning-free`| Low-latency, high-throughput code helper | **1,000,000** | 65,536 |
| `muse-spark-1.2-contributor-free` | Collaborative contributor reasoning model | **1,048,576** | 131,072 |
| `muse-spark-1.3-contributor-free` | Enhanced collaborative multimodal model | **1,048,576** | 131,072 |

### 2. Kilo AI Curated Models
| Model Identifier | Specialization | Context Window | Max Output |
| :--- | :--- | :--- | :--- |
| `nvidia/nemotron-3-ultra-550b-a55b:free` | 550B flagship frontier reasoning model | **1,000,000** | 65,536 |
| `nvidia/nemotron-3-super-120b-a12b:free` | 120B high-throughput reasoning model | **1,000,000** | 65,536 |
| `nvidia/nemotron-3.5-lightning:free` | Ultra-fast general assistant model | **1,000,000** | 65,536 |
| `stealth/space-bunny-alpha` | 1M ultra-long context model | **1,000,000** | 65,536 |
| `stepfun/step-3.7-flash:free` | Fast multi-step reasoning model | **262,144** | 262,144 |
| `inclusionai/ling-3.0-flash-fin:free` | Finance & domain reasoning specialist | **262,144** | 32,768 |
| `dots-studio/dots-3-note-preview:free` | 512k context document summarization | **512,000** | 65,536 |
| `poolside/laguna-s-2.1:free` | Code synthesis & refactoring specialist | **262,144** | 32,768 |
| `poolside/laguna-xs-2.1:free` | Lightweight low-latency code assistant | **262,144** | 32,768 |
| `liquid/lfm-2.5-2.6b:free` | Liquid Neural Network adaptive model | **65,536** | 16,384 |
| `nvidia/nemotron-3-nano-omni-30b-a3b-reasoning:free` | Compact multimodal vision & reasoning | **256,000** | 65,536 |
| `nex-agi/nex-n2.5-pro:free` | Agentic & complex multi-turn workflows | **256,000** | 32,000 |
| `nex-agi/nex-n2.5-mini:free` | Ultra-low latency chat assistant | **256,000** | 32,000 |
| `cohere/north-mini-code:free` | Fast syntax, code completion & linting | **256,000** | 64,000 |
| `kilo-auto/free` | Smart router across active free pools | **256,000** | 10,000 |
| `openrouter/free` | Community-curated fallback pool | **200,000** | 65,536 |

---

## 🚀 Installation & Setup

### Method 1: CLIProxyAPI Plugin Store (1-Click Install)

1. Ensure plugin store support is enabled in your `config.yaml`:
   ```yaml
   plugins:
     enabled: true
     store-sources:
       - "https://raw.githubusercontent.com/tsaQB/cpa-freetier-plugin/master/registry.json"
     configs:
       cpa-freetier-plugin:
         enabled: true
   ```
2. Open your CLIProxyAPI Management Center (`#/plugin-store`).
3. Search for **CPA Free-Tier Models** and click **Install**.
4. Create the provider descriptor in your `auth-dir` (default `~/.cli-proxy-api/`):
   ```bash
   echo '{"disabled":false,"provider":"cpa-freetier-plugin","type":"cpa-freetier-plugin"}' > ~/.cli-proxy-api/cpa-freetier-plugin.json
   ```
5. Restart CLIProxyAPI.

---

### Method 2: Manual Installation (All Platforms)

Download the verified platform package from [Releases](https://github.com/tsaQB/cpa-freetier-plugin/releases/latest):

#### 🐧 Linux (x86_64 / amd64)
```bash
mkdir -p ~/.cli-proxy-api/plugins
curl -sL https://github.com/tsaQB/cpa-freetier-plugin/releases/download/v1.2.1/cpa-freetier-plugin_1.2.1_linux_amd64.zip -o /tmp/plugin.zip
unzip -o /tmp/plugin.zip -d ~/.cli-proxy-api/plugins/
rm /tmp/plugin.zip
```

#### 🐧 Linux (ARM64 / aarch64)
```bash
mkdir -p ~/.cli-proxy-api/plugins
curl -sL https://github.com/tsaQB/cpa-freetier-plugin/releases/download/v1.2.1/cpa-freetier-plugin_1.2.1_linux_arm64.zip -o /tmp/plugin.zip
unzip -o /tmp/plugin.zip -d ~/.cli-proxy-api/plugins/
rm /tmp/plugin.zip
```

#### 🍏 macOS (Apple Silicon M1/M2/M3/M4)
```bash
mkdir -p ~/.cli-proxy-api/plugins
curl -sL https://github.com/tsaQB/cpa-freetier-plugin/releases/download/v1.2.1/cpa-freetier-plugin_1.2.1_darwin_arm64.zip -o /tmp/plugin.zip
unzip -o /tmp/plugin.zip -d ~/.cli-proxy-api/plugins/
rm /tmp/plugin.zip
```

#### 🍏 macOS (Intel x86_64)
```bash
mkdir -p ~/.cli-proxy-api/plugins
curl -sL https://github.com/tsaQB/cpa-freetier-plugin/releases/download/v1.2.1/cpa-freetier-plugin_1.2.1_darwin_amd64.zip -o /tmp/plugin.zip
unzip -o /tmp/plugin.zip -d ~/.cli-proxy-api/plugins/
rm /tmp/plugin.zip
```

#### 🪟 Windows (x86_64)
Extract `cpa-freetier-plugin_1.2.1_windows_amd64.zip` into your configured `plugins/` directory so that `cpa-freetier-plugin.dll` resides next to `cli-proxy-api.exe`.

#### Register Provider Descriptor & Enable
In your `~/.cli-proxy-api/` directory, create `cpa-freetier-plugin.json`:
```json
{
  "disabled": false,
  "provider": "cpa-freetier-plugin",
  "type": "cpa-freetier-plugin"
}
```
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

## 🌐 Optional Outbound Proxy Support

The plugin **works 100% direct out-of-the-box without requiring any proxy**.

However, if your host is hosted in a restricted cloud datacenter and you want to route OpenCode requests through custom residential, mobile, or SOCKS5 proxies to eliminate rate-limits, define `OPENCODE_PROXY`:

```bash
# Supports HTTP, HTTPS, or SOCKS5 proxies
export OPENCODE_PROXY="http://127.0.0.1:8080"
# or
export OPENCODE_PROXY="socks5h://127.0.0.1:1080"
```

If `OPENCODE_PROXY` is unset, the plugin connects direct to upstream via the host's standard network interface.

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
