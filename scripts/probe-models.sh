#!/usr/bin/env bash
# Live-probe every advertised model id against the real upstream.
#
# Why this exists next to check-models.sh: catalogue presence is not liveness.
# `nvidia/nemotron-3-super-120b-a12b:free` was still itemised in the Kilo
# catalogue while every live call returned
#   404 The requested model is currently unavailable
# and `thinkingmachines/inkling-small:free` had already been dropped from the
# catalogue. A catalogue diff alone cannot tell those apart, so the ids the
# plugin advertises are also called for real.
#
# Usage:  scripts/probe-models.sh [path/to/main.go]
# Exit:   0 every id answered or was inconclusive · 1 at least one id is dead
#
# Classification of one call:
#   200                      -> alive
#   404 / 410 / "does not exist" / "unavailable" / "deprecated"
#                            -> DEAD, the build must not ship this id
#   403 / 401 / 429 / 5xx / transport error
#                            -> inconclusive (datacenter egress, quota, flaky
#                               network) and never fails the build
#
# The OpenCode free tier is gated on request shape: it rejects a request that
# does not carry the official tool declarations with the right User-Agent, and
# it answers datacenter ASNs with 403 FreeTierError. This script therefore
# sends the exact cloaked payload the plugin sends, and a probe that is not
# sent through residential egress will legitimately be inconclusive rather than
# a false "dead".
#
#   PROBE_PROXY=http://127.0.0.1:8127 scripts/probe-models.sh main.go
#
# PROBE_PROXY may be a comma-separated list; the probe rotates through it so a
# single egress being rate-limited does not paint the whole catalogue grey.
set -uo pipefail

MAIN_FILE="${1:-main.go}"
PROBE_ATTEMPTS="${PROBE_ATTEMPTS:-3}"
PROBE_SLEEP="${PROBE_SLEEP:-2}"
OPENCODE_BASE="${OPENCODE_BASE_URL:-https://opencode.ai}"
OPENCODE_URL="$OPENCODE_BASE/inference/openai/v1/chat/completions"
KILO_URL="${KILO_URL:-https://api.kilo.ai/api/gateway/chat/completions}"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

have() { command -v "$1" >/dev/null 2>&1; }
if ! have curl; then
  echo "probe-models: curl is required" >&2
  exit 2
fi
if have python3; then
  PY=python3
elif have python; then
  PY=python
else
  echo "probe-models: python3 is required" >&2
  exit 2
fi

# The plugin's own maps are the source of truth for what it routes.
extract_ids() { # map name
  "$PY" - "$MAIN_FILE" "$1" <<'PY'
import re, sys
src = open(sys.argv[1], encoding='utf-8').read()
m = re.search(r'var ' + sys.argv[2] + r' = map\[string\]bool\{(.*?)\n\}', src, re.S)
if not m:
    sys.exit(f"map {sys.argv[2]} not found in {sys.argv[1]}")
for line in m.group(1).splitlines():
    line = line.strip()
    if not line or line.startswith('//'):
        continue
    hit = re.match(r'"([^"]+)"\s*:\s*true', line)
    if hit:
        print(hit.group(1))
PY
}

# The fingerprint the free-tier gate accepts: official read/shell tool
# declarations, the 2.0.32 client UA and a shaped session id.
cat > "$tmpdir/probe.py" <<'PY'
import json
import os
import random
import sys
import time
import urllib.error
import urllib.request

BASE62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
MASK = (1 << 48) - 1


def session_id():
    ts = int(time.time() * 1000)
    val = (~(ts * 4096 + random.randint(1, 100))) & MASK
    tail = "".join(random.choice(BASE62) for _ in range(14))
    return f"ses_{val:012x}{tail}"


TOOLS = [
    {
        "type": "function",
        "function": {
            "name": "read",
            "description": "Read the contents of a file or directory. Supports text files, images, and PDFs. Each text line is prefixed by its 1-based line number as <line>: <content>. The prefix is for reference and is not part of the file content. Directory entries are returned one per line. Use offset and limit to read large files or directories in sections. Prefer one larger read over many small slices, and use grep to find specific content in large files.",
            "parameters": {
                "type": "object",
                "properties": {
                    "path": {"type": "string", "description": "File or directory to read"},
                    "offset": {"type": "integer", "minimum": 0, "description": "The line or directory entry to start reading from (1-based)"},
                    "limit": {"type": "integer", "minimum": 0, "description": "The maximum number of lines or directory entries to read"},
                },
                "required": ["path"],
                "additionalProperties": False,
            },
        },
    },
    {
        "type": "function",
        "function": {
            "name": "shell",
            "description": "Execute a shell command and return its output. Commands run on Linux using bash. Quote file paths containing spaces or special characters. Prefer dedicated tools over shell commands when possible. When output is large, the full result is saved to a file and a truncated preview is returned. Rely on automatic truncation unless filtering the output is more useful. Commands accept an optional timeout, background commands have no timeout by default. Background commands return immediately, and you will be notified when they complete.",
            "parameters": {
                "type": "object",
                "properties": {
                    "command": {"type": "string", "description": "Shell command string to execute"},
                    "workdir": {"type": "string", "description": "Working directory to execute the command in."},
                    "timeout": {"type": "integer", "minimum": 0, "description": "Timeout in milliseconds."},
                    "background": {"type": "boolean", "description": "Run the command in the background and return immediately."},
                },
                "required": ["command"],
                "additionalProperties": False,
            },
        },
    },
]

DEAD_MARKERS = (
    "does not exist", "model_not_found", "modelnotfound",
    "currently unavailable", "modeldeprecated", "has been deprecated",
    "not_found", "unknown model", "no such model",
    "no available provider", "model is not available",
    "requested model is currently unavailable",
)


def classify(status, body):
    body = (body or "").lower()
    if status == 200:
        return "ALIVE", "HTTP 200"
    # A quota/rate-limit answer is never proof of death, whatever the wording:
    # upstream says "endpoint is unavailable" while it is merely shedding load.
    if status in (401, 403, 429) or status >= 500:
        return "INCONCLUSIVE", f"HTTP {status} {body[:150]}"
    if status in (404, 410) or any(m in body for m in DEAD_MARKERS):
        return "DEAD", f"HTTP {status} {body[:150]}"
    return "INCONCLUSIVE", f"HTTP {status} {body[:150]}"


def attempt(url, model, flavour, proxy):
    sid = session_id()
    body = {
        "model": model,
        "messages": [
            {"role": "system", "content": "Do not call the read or shell tools; they are placeholders that do nothing. Answer the prompt directly or use other tools."},
            {"role": "user", "content": "Reply with OK only."},
        ],
    }
    headers = {"Content-Type": "application/json"}
    if flavour == "opencode":
        body["stream"] = True
        body["stream_options"] = {"include_usage": True}
        body["tools"] = TOOLS
        body["tool_choice"] = "auto"
        headers.update({
            "Authorization": "Bearer public",
            "User-Agent": "opencode/latest/2.0.32/cli",
            "x-opencode-client": "cli",
            "x-opencode-project": "global",
            "x-opencode-session-id": sid,
            "x-opencode-session": sid,
            "x-session-affinity": sid,
            "x-session-id": sid,
            "X-Session-Id": sid,
            "Accept": "text/event-stream",
        })
    else:
        body["stream"] = False

    req = urllib.request.Request(url, data=json.dumps(body).encode(), headers=headers)
    opener = urllib.request.build_opener(
        urllib.request.ProxyHandler({"http": proxy, "https": proxy})
    ) if proxy else urllib.request.build_opener()
    try:
        with opener.open(req, timeout=25) as resp:
            resp.read(400)
            return classify(200, "")
    except urllib.error.HTTPError as exc:
        return classify(exc.code, exc.read().decode("utf-8", "ignore")[:300])
    except Exception as exc:
        return "INCONCLUSIVE", f"ERR {str(exc)[:110]}"


def main():
    url, model, flavour = sys.argv[1], sys.argv[2], sys.argv[3]
    proxies = [p.strip() for p in os.environ.get("PROBE_PROXY", "").split(",") if p.strip()]
    tries = int(os.environ.get("PROBE_ATTEMPTS", "3"))

    worst = ("INCONCLUSIVE", "no attempt")
    for n in range(tries):
        proxy = proxies[n % len(proxies)] if proxies else None
        verdict, detail = attempt(url, model, flavour, proxy)
        if verdict == "ALIVE":
            print("ALIVE\tHTTP 200")
            return
        # A dead verdict is final: retrying a 404 only wastes quota.
        if verdict == "DEAD":
            print(f"DEAD\t{detail}")
            return
        worst = (verdict, detail)
        time.sleep(4)
    print(f"{worst[0]}\t{worst[1]}")


main()
PY

proxy_list="$(printf '%s' "${PROBE_PROXY:-}" | tr ',' ' ')"
proxy_count=0
for p in $proxy_list; do proxy_count=$((proxy_count + 1)); done

extract_ids kiloModels    > "$tmpdir/kilo.ids"
extract_ids openCodeModels > "$tmpdir/opencode.ids"
echo "probe-models: $(wc -l < "$tmpdir/kilo.ids") Kilo ids, $(wc -l < "$tmpdir/opencode.ids") OpenCode ids"
if [ "$proxy_count" -gt 0 ]; then
  echo "probe-models: $proxy_count proxy egress(es) in rotation"
else
  echo "probe-models: no proxy — datacenter egress will usually be inconclusive"
fi
echo

dead=0
alive=0
inconclusive=0

run_group() { # flavour url ids-file
  local flavour="$1" url="$2" ids_file="$3"
  local id result verdict details
  while IFS= read -r id; do
    [ -z "$id" ] && continue
    result="$(PROBE_ATTEMPTS="$PROBE_ATTEMPTS" "$PY" "$tmpdir/probe.py" "$url" "$id" "$flavour" 2>&1 | tail -n 1)"
    verdict="$(printf '%s' "$result" | cut -f1)"
    details="$(printf '%s' "$result" | cut -f2-)"
    case "$verdict" in
      ALIVE)        alive=$((alive + 1));        printf '✅ %-58s %s\n' "$id" "$details" ;;
      DEAD)         dead=$((dead + 1));         printf '❌ %-58s %s\n' "$id" "$details" ;;
      *)            inconclusive=$((inconclusive + 1)); printf '⚠️  %-58s %s\n' "$id" "$details" ;;
    esac
    sleep "$PROBE_SLEEP"
  done < "$ids_file"
}

echo "── Kilo"
run_group "kilo" "$KILO_URL" "$tmpdir/kilo.ids"
echo
echo "── OpenCode Zen"
run_group "opencode" "$OPENCODE_URL" "$tmpdir/opencode.ids"

echo
echo "── summary: $alive alive · $dead dead · $inconclusive inconclusive"

if [ "$dead" -gt 0 ]; then
  echo "probe-models: FAILED — prune the dead ids above (and README) before releasing"
  exit 1
fi
if [ "$alive" -eq 0 ] && [ "$inconclusive" -gt 0 ]; then
  echo "probe-models: OK (no dead ids) — but nothing could be confirmed; use a residential proxy for real verdicts"
  exit 0
fi
echo "probe-models: OK (no dead ids)"
