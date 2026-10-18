#!/usr/bin/env bash
# Verify that every model id the plugin advertises still exists upstream.
#
# Why: the free catalogues rotate. v1.2.4 shipped five Kilo ids that had already
# disappeared (404 "does not exist") plus three router-map entries with no catalog
# entry, so agents picked models that could never answer. This check fails the
# build instead of shipping that again.
#
# Usage:  scripts/check-models.sh [path/to/main.go]
# Exit:   0 all ids present · 1 at least one id is gone · 2 catalogue unreachable
#
# Only "the model does not exist" is fatal. A 429/5xx from the catalogue host is
# reported as a warning so a flaky upstream cannot block a release.
set -uo pipefail

MAIN_FILE="${1:-main.go}"
KILO_MODELS_URL="${KILO_MODELS_URL:-https://api.kilo.ai/api/gateway/models}"
ZEN_MODELS_URL="${ZEN_MODELS_URL:-https://opencode.ai/zen/v1/models}"
CURL_OPTS=(-sS -m 30 -H 'Accept: application/json')

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

fail=0
warn=0

have() { command -v "$1" >/dev/null 2>&1; }
if ! have curl; then
  echo "check-models: curl is required" >&2
  exit 2
fi

fetch_json() { # url dest
  curl "${CURL_OPTS[@]}" -o "$2" -w '%{http_code}' "$1" 2>/dev/null || echo "000"
}

if have python3; then
  PY=python3
elif have python; then
  PY=python
else
  echo "check-models: python3 is required" >&2
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

check_group() { # label, url, ids-file, fatal(1/0)
  local label="$1"
  local url="$2"
  local ids_file="$3"
  local fatal="${4:-1}"
  local body
  body="$tmpdir/$(printf '%s' "$url" | tr -c 'a-zA-Z0-9' '_').json"
  local code
  code="$(fetch_json "$url" "$body")"
  if [ "$code" != "200" ]; then
    echo "⚠️  $label catalogue unreachable (HTTP $code) — skipped, not treated as failure"
    warn=$((warn + 1))
    return
  fi

  "$PY" - "$body" "$ids_file" "$label" "$fatal" <<'PY'
import json, sys
body, idfile, label, fatal = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4] == "1"
try:
    payload = json.load(open(body, encoding='utf-8'))
except Exception as exc:                       # noqa: BLE001 - report and skip
    print(f"⚠️  {label} catalogue is not JSON ({exc}) — skipped")
    sys.exit(0)

listed = {m.get("id") for m in payload.get("data", []) if isinstance(m, dict)}
ids = [l.strip() for l in open(idfile, encoding='utf-8') if l.strip()]

gone, router_only = [], []
for model in ids:
    if model in listed:
        continue
    # Router aliases (kilo-auto/*, openrouter/*) are resolved server-side and are
    # not itemised in the catalogue, so absence is not proof they are dead.
    if model.startswith(("kilo-auto/", "openrouter/")):
        router_only.append(model)
        continue
    gone.append(model)

print(f"── {label}: {len(ids) - len(gone)}/{len(ids)} ids present in the catalogue")
for model in router_only:
    print(f"ℹ️  {label}: {model} is a router alias (not itemised upstream)")
for model in gone:
    # Absence from a catalogue is not proof of death (the OpenCode Zen list does
    # not itemise every served id), so only the strict group fails the build.
    if fatal:
        print(f"❌ {label}: {model} is advertised by the plugin but missing upstream")
    else:
        print(f"⚠️  {label}: {model} is not itemised upstream — confirm with a live call")
sys.exit(1 if (gone and fatal) else 0)
PY
  local rc=$?
  if [ $rc -eq 1 ]; then
    fail=1
  fi
}

extract_ids kiloModels  > "$tmpdir/kilo.ids"
extract_ids openCodeModels > "$tmpdir/opencode.ids"
echo "check-models: $(wc -l < "$tmpdir/kilo.ids") Kilo ids, $(wc -l < "$tmpdir/opencode.ids") OpenCode ids"

check_group "Kilo" "$KILO_MODELS_URL" "$tmpdir/kilo.ids" 1
check_group "OpenCode Zen" "$ZEN_MODELS_URL" "$tmpdir/opencode.ids" 0

if [ "$fail" -eq 1 ]; then
  echo "check-models: FAILED — prune the ids above (and README) before releasing"
  exit 1
fi
if [ "$warn" -gt 0 ]; then
  echo "check-models: OK with warnings ($warn catalogue(s) unreachable)"
else
  echo "check-models: OK"
fi
