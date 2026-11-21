#!/usr/bin/env bash
# End-to-end regression suite for PeaceClaude.
# Runs in an isolated PEACECLAUDE_HOME sandbox on dedicated high ports so it NEVER disturbs live user data.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="${ROOT}/bin/peaceclaude"
TMP_HOME="$(mktemp -d /tmp/peaceclaude-regression.XXXXXX)"
export PEACECLAUDE_HOME="$TMP_HOME"
export PEACECLAUDE_NO_NOTIFY=1

HTTP_PORT=29897
SOCKS_PORT=29898
CTRL_PORT=29899

PASS=0
FAIL=0
DAEMON_PID=""

cleanup() {
  if [[ -n "$DAEMON_PID" ]]; then
    kill "$DAEMON_PID" 2>/dev/null || true
    wait "$DAEMON_PID" 2>/dev/null || true
  fi
  rm -rf "$TMP_HOME"
}
trap cleanup EXIT

ok() { PASS=$((PASS + 1)); printf 'PASS  %s — %s\n' "$1" "$2"; }
fail() { FAIL=$((FAIL + 1)); printf 'FAIL  %s — %s\n' "$1" "$2" >&2; }

echo "=== PeaceClaude End-to-End Regression Suite ==="
echo "sandbox: $TMP_HOME"

# 1. Build & smoke --version
(cd "$ROOT" && go build -o "$BIN" ./cmd/peaceclaude)
VER_OUT="$("$BIN" --version 2>&1)"
if [[ "$VER_OUT" == peaceclaude* ]]; then
  ok "binary_version" "$VER_OUT"
else
  fail "binary_version" "unexpected output: $VER_OUT"
fi

# 2. Seed isolated config with dedicated ports + test secret password (must NEVER leak)
SECRET_MARKER="ws-regression-secret-do-not-leak-98765"
EXPECTED_LIVE_IP="${EXPECTED_IP:-}"
if [[ -z "$EXPECTED_LIVE_IP" && -r "$HOME/.local/share/claude-egress/expected_ip" ]]; then
  EXPECTED_LIVE_IP="$(tr -d '[:space:]' < "$HOME/.local/share/claude-egress/expected_ip")"
fi
"$BIN" config show >/dev/null
python3 - "$TMP_HOME/config.json" "$HTTP_PORT" "$SOCKS_PORT" "$CTRL_PORT" "$SECRET_MARKER" "$EXPECTED_LIVE_IP" <<'PY'
import json, sys
p, hp, sp, cp, sec, exp = sys.argv[1], int(sys.argv[2]), int(sys.argv[3]), int(sys.argv[4]), sys.argv[5], sys.argv[6]
d = json.loads(open(p).read())
d["listen"]["http_port"] = hp
d["listen"]["socks5_port"] = sp
d["listen"]["control_port"] = cp
d["upstream"]["mode"] = "local-proxy"
d["upstream"]["address"] = "127.0.0.1:7897"
d["upstream"]["username"] = "reg-user"
d["upstream"]["password"] = sec
d["egress"]["fail_closed"] = True
d["egress"]["expected_ip"] = exp
d["capture"]["enabled"] = True
open(p, "w").write(json.dumps(d, indent=2) + "\n")
PY

CFG_SHOW="$("$BIN" config show)"
if [[ "$CFG_SHOW" == *"$SECRET_MARKER"* ]]; then
  fail "config_redaction" "password leaked in config show"
else
  ok "config_redaction" "credentials properly redacted"
fi

# 3. Start real daemon in sandbox
"$BIN" service run >"$TMP_HOME/daemon-stdout.log" 2>&1 &
DAEMON_PID=$!
READY=0
for _ in $(seq 1 40); do
  if curl -fsS "http://127.0.0.1:${CTRL_PORT}/api/health" >/dev/null 2>&1; then
    READY=1
    break
  fi
  sleep 0.25
done
if [[ "$READY" == 1 ]]; then
  ok "daemon_startup" "control API ready on :$CTRL_PORT"
else
  fail "daemon_startup" "control API failed to come up"
  cat "$TMP_HOME/daemon-stdout.log" || true
  exit 1
fi

# 4. Verify Web UI & PAC endpoint
UI_CODE="$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:${CTRL_PORT}/")"
PAC_BODY="$(curl -fsS "http://127.0.0.1:${CTRL_PORT}/peaceclaude.pac")"
if [[ "$UI_CODE" == "200" && "$PAC_BODY" == *"anthropic.com"* && "$PAC_BODY" == *"$HTTP_PORT"* ]]; then
  ok "webui_and_pac" "Web UI HTTP 200 and PAC serves port $HTTP_PORT"
else
  fail "webui_and_pac" "ui=$UI_CODE"
fi

# 5. Negative security test: Cross-origin CSRF mutation MUST be blocked (403)
CSRF_CODE="$(curl -s -o /dev/null -w '%{http_code}' -X POST -H 'Origin: https://evil.test' \
  -d '{"locked":true}' "http://127.0.0.1:${CTRL_PORT}/api/lock")"
if [[ "$CSRF_CODE" == "403" ]]; then
  ok "csrf_guard" "cross-origin POST rejected with HTTP 403"
else
  fail "csrf_guard" "expected 403, got $CSRF_CODE"
fi

# 6. Telemetry shield test: sentry.io MUST be blocked (403) via HTTP proxy
TEL_CODE="$(curl -s --max-time 5 -o /dev/null -w '%{http_code}' \
  -x "http://127.0.0.1:${HTTP_PORT}" "http://o1158394.ingest.us.sentry.io/api/1/store/")"
if [[ "$TEL_CODE" == "403" ]]; then
  ok "telemetry_shield" "sentry.io blocked with HTTP 403"
else
  fail "telemetry_shield" "expected 403 for telemetry, got $TEL_CODE"
fi

# 7. Fail-Closed manual lock MUST block Anthropic requests (503 / non-200) and refuse CLI launch
"$BIN" lock "regression fail-closed test" >/dev/null
LOCK_CODE="$(curl -s --max-time 5 -o /dev/null -w '%{http_code}' \
  -x "http://127.0.0.1:${HTTP_PORT}" "http://api.anthropic.com/v1/messages")"
if [[ "$LOCK_CODE" == "503" ]]; then
  ok "failclosed_http_block" "Anthropic HTTP request hard-blocked with 503 while locked"
else
  fail "failclosed_http_block" "expected 503 while locked, got $LOCK_CODE"
fi

if "$BIN" cli --version >/dev/null 2>&1; then
  fail "failclosed_cli_preflight" "CLI launch should be rejected when gate is locked"
else
  ok "failclosed_cli_preflight" "CLI launch refused while Egress Gate is locked"
fi

# 8. Unlock & test real live static IP egress (if local proxy 7897 is reachable and EXPECTED_LIVE_IP is set)
"$BIN" unlock >/dev/null
if [[ "${SKIP_LIVE_EGRESS:-0}" != "1" && -n "$EXPECTED_LIVE_IP" ]] && nc -z 127.0.0.1 7897 2>/dev/null; then
  TRACE_OUT="$(curl -fsS --max-time 16 -x "http://127.0.0.1:${HTTP_PORT}" "https://claude.ai/cdn-cgi/trace" 2>/dev/null || true)"
  GOT_IP="$(awk -F= '/^ip=/{print $2; exit}' <<<"$TRACE_OUT")"
  if [[ "$GOT_IP" == "$EXPECTED_LIVE_IP" ]]; then
    ok "live_http_proxy_egress" "claude.ai via PeaceClaude HTTP :$HTTP_PORT exited at expected static IP"
  else
    fail "live_http_proxy_egress" "expected $EXPECTED_LIVE_IP, got ${GOT_IP:-empty}"
  fi

  SOCKS_OUT="$(curl -fsS --max-time 16 --socks5-hostname "127.0.0.1:${SOCKS_PORT}" "https://claude.ai/cdn-cgi/trace" 2>/dev/null || true)"
  SOCKS_IP="$(awk -F= '/^ip=/{print $2; exit}' <<<"$SOCKS_OUT")"
  if [[ "$SOCKS_IP" == "$EXPECTED_LIVE_IP" ]]; then
    ok "live_socks5_proxy_egress" "claude.ai via PeaceClaude SOCKS5 :$SOCKS_PORT exited at expected static IP"
  else
    fail "live_socks5_proxy_egress" "expected $EXPECTED_LIVE_IP, got ${SOCKS_IP:-empty}"
  fi
fi

# 9. Verify capture sanitization (Authorization header must be redacted, secret never in files)
curl -s --max-time 5 -o /dev/null -x "http://127.0.0.1:${HTTP_PORT}" \
  -H "Authorization: Bearer sk-ant-regression-token-xyz" \
  "http://o1158394.ingest.us.sentry.io/test?token=query-secret-123" || true
CAP_JSON="$("$BIN" capture list --json)"
if [[ "$CAP_JSON" == *"sk-ant-regression-token-xyz"* || "$CAP_JSON" == *"query-secret-123"* || "$CAP_JSON" == *"$SECRET_MARKER"* ]]; then
  fail "capture_sanitization" "sensitive token leaked in capture output"
elif [[ "$CAP_JSON" == *"[REDACTED]"* ]]; then
  ok "capture_sanitization" "Authorization and query secrets properly [REDACTED]"
else
  fail "capture_sanitization" "expected [REDACTED] marker in capture records"
fi

# 10. Profile create / switch / rules sync / dry-run clean
"$BIN" profile create qa-us --note "QA isolated profile" --use >/dev/null
"$BIN" rules on >/dev/null
CLEAN_JSON="$("$BIN" clean --profile qa-us --dry-run --json)"
if [[ "$CLEAN_JSON" == *'"profile": "qa-us"'* && "$CLEAN_JSON" == *'"dry_run": true'* ]]; then
  ok "profile_rules_clean" "profile isolation, rules sync, and clean dry-run verified"
else
  fail "profile_rules_clean" "unexpected clean output"
fi

# 11. pf anchor generation + pfctl -n syntax check
VM_JSON="$("$BIN" vm-proxy status --json)"
if [[ "$VM_JSON" == *'"syntax_valid": true'* ]]; then
  ok "pf_syntax_valid" "generated macOS pf anchor passed pfctl -n"
else
  fail "pf_syntax_valid" "pf syntax check failed: $VM_JSON"
fi

# 12. Desktop dry-run regression guard: MUST NOT pass --host-resolver-rules (rejected by official Claude.app v6+)
if [[ -d /Applications/Claude.app ]]; then
  APP_DRY="$("$BIN" app run --dry-run)"
  if [[ "$APP_DRY" == *"--host-resolver-rules"* ]]; then
    fail "desktop_no_forbidden_switch" "desktop args unexpectedly contained --host-resolver-rules"
  elif [[ "$APP_DRY" == *"--proxy-server=http://127.0.0.1:${HTTP_PORT}"* && "$APP_DRY" == *"--disable-quic"* ]]; then
    ok "desktop_no_forbidden_switch" "Claude Desktop launch args valid & free of v6+ forbidden switches"
  else
    fail "desktop_no_forbidden_switch" "missing required isolation flags in: $APP_DRY"
  fi
fi

# 13. Status & Doctor JSON check + ensure zero secret leakage across status/logs
STATUS_JSON="$("$BIN" status --json)"
if [[ "$STATUS_JSON" == *"$SECRET_MARKER"* ]] || grep -q "$SECRET_MARKER" "$TMP_HOME/daemon-stdout.log" 2>/dev/null; then
  fail "no_secret_leak_anywhere" "secret marker found in status or daemon logs"
else
  ok "no_secret_leak_anywhere" "zero credentials leaked across status and logs"
fi

echo "=== Regression Summary: pass=$PASS fail=$FAIL ==="
exit "$FAIL"
