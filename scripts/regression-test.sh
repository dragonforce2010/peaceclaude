#!/usr/bin/env bash
# End-to-end regression suite for PeaceClaude.
# Runs in an isolated PEACECLAUDE_HOME sandbox on dedicated high ports so it NEVER disturbs live user data.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="${ROOT}/bin/peaceclaude"
TMP_HOME="$(mktemp -d /tmp/peaceclaude-regression.XXXXXX)"
export PEACECLAUDE_HOME="$TMP_HOME"
export PEACECLAUDE_NO_NOTIFY=1
export PEACECLAUDE_DISABLE_KEYCHAIN=1
export PEACECLAUDE_DISABLE_AUTO_PEACE=1

HTTP_PORT=29897
SOCKS_PORT=29898
CTRL_PORT=29899

PASS=0
FAIL=0
DAEMON_PID=""
CURL_PIDS=()

cleanup() {
  for p in "${CURL_PIDS[@]:-}"; do
    [[ -n "$p" ]] && kill "$p" 2>/dev/null || true
  done
  if [[ -n "$DAEMON_PID" ]]; then
    kill "$DAEMON_PID" 2>/dev/null || true
    for _ in 1 2 3 4 5 6 7 8 9 10 11 12; do
      if ! kill -0 "$DAEMON_PID" 2>/dev/null; then
        break
      fi
      sleep 0.25
    done
    if kill -0 "$DAEMON_PID" 2>/dev/null; then
      kill -9 "$DAEMON_PID" 2>/dev/null || true
    fi
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
d["remote"]["mobile_port"] = 29900
d["capture"]["enabled"] = True
open(p, "w").write(json.dumps(d, indent=2) + "\n")
PY

CFG_SHOW="$("$BIN" config show)"
if [[ "$CFG_SHOW" == *"$SECRET_MARKER"* ]]; then
  fail "config_redaction" "password leaked in config show"
else
  ok "config_redaction" "credentials properly redacted"
fi

# 3. Ensure sandbox ports are free, then start real daemon in sandbox
for port in "$HTTP_PORT" "$SOCKS_PORT" "$CTRL_PORT" 29900; do
  if nc -z 127.0.0.1 "$port" 2>/dev/null; then
    fail "sandbox_port_free" "port $port already in use before regression start"
    exit 1
  fi
done

"$BIN" service run >"$TMP_HOME/daemon-stdout.log" 2>&1 &
DAEMON_PID=$!
READY=0
for _ in $(seq 1 40); do
  if ! kill -0 "$DAEMON_PID" 2>/dev/null; then
    break
  fi
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

# 8. Sync live configured static-IP upstream into sandbox (prefer direct-http for reliable concurrency), unlock & test real static IP egress
LIVE_CFG="$HOME/.peaceclaude/config.json"
LIVE_UPSTREAM_READY=0
if [[ "${SKIP_LIVE_EGRESS:-0}" != "1" && -r "$LIVE_CFG" ]]; then
  LIVE_UPSTREAM_READY="$(python3 - "$LIVE_CFG" "http://127.0.0.1:${CTRL_PORT}" <<'PY'
import json, sys, urllib.request
cfg = json.load(open(sys.argv[1]))
up = cfg.get("upstream", {})
exp = cfg.get("egress", {}).get("expected_ip", "")
if up.get("mode") in ("direct-socks5", "direct-http") and up.get("address") and exp:
    mode = "direct-http" if up.get("mode") == "direct-socks5" else up.get("mode")
    payload = json.dumps({
        "upstream_mode": mode,
        "upstream_address": up.get("address"),
        "upstream_username": up.get("username", ""),
        "upstream_password": up.get("password", ""),
        "expected_ip": exp,
    }).encode()
    req = urllib.request.Request(sys.argv[2] + "/api/config/update", data=payload, method="POST",
        headers={"Content-Type": "application/json", "Origin": sys.argv[2]})
    with urllib.request.urlopen(req, timeout=15) as r:
        res = json.loads(r.read().decode())
        print("1" if res.get("ok") else "0")
else:
    print("0")
PY
)"
fi
"$BIN" unlock >/dev/null || true
CAN_LIVE=0
if [[ "${SKIP_LIVE_EGRESS:-0}" != "1" && -n "$EXPECTED_LIVE_IP" ]]; then
  if [[ "$LIVE_UPSTREAM_READY" == "1" ]] || nc -z 127.0.0.1 7897 2>/dev/null; then
    CAN_LIVE=1
  fi
fi
if [[ "$CAN_LIVE" == "1" ]]; then
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

# 10. Multi-account CRUD, detail, usage redaction & rules sync
ACCOUNTS_CREATE_CODE="$(curl -s -o /dev/null -w '%{http_code}' -X POST -H "Origin: http://127.0.0.1:${CTRL_PORT}" \
  -H "Content-Type: application/json" \
  -d '{"name":"qa-us","display_name":"QA 备用 Max","note":"QA isolated profile","manual_plan":"Max 5x","manual_registered_at":"2026-06-15"}' \
  "http://127.0.0.1:${CTRL_PORT}/api/profiles/create")"
ACCOUNTS_JSON="$(curl -fsS "http://127.0.0.1:${CTRL_PORT}/api/accounts")"
DETAIL_JSON="$(curl -fsS "http://127.0.0.1:${CTRL_PORT}/api/accounts/detail?name=qa-us")"
if [[ "$ACCOUNTS_CREATE_CODE" == "200" && "$ACCOUNTS_JSON" == *"Max 5x"* && "$DETAIL_JSON" == *"QA 备用 Max"* ]]; then
  ok "multi_account_api" "accounts list, plan/tenure metadata, and detail API verified"
else
  fail "multi_account_api" "unexpected account API output (code=$ACCOUNTS_CREATE_CODE)"
fi

"$BIN" rules on >/dev/null
CLEAN_JSON="$("$BIN" clean --profile qa-us --dry-run --json)"
if [[ "$CLEAN_JSON" == *'"profile": "qa-us"'* && "$CLEAN_JSON" == *'"dry_run": true'* ]]; then
  ok "profile_rules_clean" "profile isolation, rules sync, and clean dry-run verified"
else
  fail "profile_rules_clean" "unexpected clean output"
fi

# Negative security test: OAuth accessToken injected into profile MUST NOT leak via /api/accounts or /api/accounts/detail
echo '{"claudeAiOauth":{"accessToken":"sk-ant-oauth-secret-leak-check-888"}}' > "$TMP_HOME/profiles/qa-us/claude-config/.credentials.json"
chmod 600 "$TMP_HOME/profiles/qa-us/claude-config/.credentials.json"
DETAIL_AFTER_TOK="$(curl -fsS "http://127.0.0.1:${CTRL_PORT}/api/accounts/detail?name=qa-us")"
if [[ "$DETAIL_AFTER_TOK" == *"sk-ant-oauth-secret-leak-check-888"* ]]; then
  fail "oauth_token_redaction" "accessToken leaked in /api/accounts/detail!"
else
  ok "oauth_token_redaction" "OAuth accessToken strictly kept private from API responses"
fi

# Update & delete qa-us cleanly
UPD_CODE="$(curl -s -o /dev/null -w '%{http_code}' -X POST -H "Origin: http://127.0.0.1:${CTRL_PORT}" \
  -H "Content-Type: application/json" -d '{"name":"qa-us","manual_plan":"Ultra"}' \
  "http://127.0.0.1:${CTRL_PORT}/api/accounts/update")"
DEL_CODE="$(curl -s -o /dev/null -w '%{http_code}' -X POST -H "Origin: http://127.0.0.1:${CTRL_PORT}" \
  -H "Content-Type: application/json" -d '{"name":"qa-us"}' \
  "http://127.0.0.1:${CTRL_PORT}/api/accounts/delete")"
if [[ "$UPD_CODE" == "200" && "$DEL_CODE" == "200" ]]; then
  ok "account_update_delete" "account metadata update and delete endpoints verified"
else
  fail "account_update_delete" "update=$UPD_CODE delete=$DEL_CODE"
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

# 14. Appearance (legacy + original preset names), workbench lock, remote QR pair + redeem + revoke end-to-end
APP_CODE="$(curl -s -o /dev/null -w '%{http_code}' -X POST -H "Origin: http://127.0.0.1:${CTRL_PORT}" \
  -H "Content-Type: application/json" -d '{"theme_mode":"light","preset_light":"paper","preset_dark":"obsidian"}' \
  "http://127.0.0.1:${CTRL_PORT}/api/appearance/update")"
APP_LEGACY_CODE="$(curl -s -o /dev/null -w '%{http_code}' -X POST -H "Origin: http://127.0.0.1:${CTRL_PORT}" \
  -H "Content-Type: application/json" -d '{"theme_mode":"dark","preset_light":"codex","preset_dark":"ayu"}' \
  "http://127.0.0.1:${CTRL_PORT}/api/appearance/update")"
PAIR_JSON="$(curl -fsS -X POST -H "Origin: http://127.0.0.1:${CTRL_PORT}" \
  -H "Content-Type: application/json" -d '{"host":"127.0.0.1"}' \
  "http://127.0.0.1:${CTRL_PORT}/api/remote/pair")"
PAIR_CODE="$(python3 -c 'import json,sys; d=json.loads(sys.stdin.read()); print(d.get("ticket",{}).get("code",""))' <<<"$PAIR_JSON")"
if [[ "$APP_CODE" == "200" && "$APP_LEGACY_CODE" == "200" && "$PAIR_JSON" == *"<svg "* && "$PAIR_CODE" == PEACE-* ]]; then
  # Verify /api/status does NOT expose raw pairing code while active pair exists.
  PRE_REDEEM_STATUS="$(curl -fsS "http://127.0.0.1:${CTRL_PORT}/api/status")"
  if [[ "$PRE_REDEEM_STATUS" == *"$PAIR_CODE"* ]]; then
    fail "status_redacts_pairing_code" "/api/status leaked plaintext active pairing code"
  fi
  REDEEM_JSON="$(curl -fsS -X POST -H "Content-Type: application/json" \
    -d "{\"code\":\"${PAIR_CODE}\",\"device_name\":\"RegTest\",\"os\":\"iOS\"}" \
    "http://127.0.0.1:29900/mobile/api/redeem")"
  MOB_TOK="$(python3 -c 'import json,sys; print(json.loads(sys.stdin.read()).get("token",""))' <<<"$REDEEM_JSON")"
  DEV_ID="$(python3 -c 'import json,sys; print(json.loads(sys.stdin.read()).get("device",{}).get("id",""))' <<<"$REDEEM_JSON")"
  MOB_STATE_CODE="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer ${MOB_TOK}" "http://127.0.0.1:29900/mobile/api/state")"
  # Single-device revoke keeps listener up on :29900 so we can observe strict 401 before Shutdown.
  REV_DEV_CODE="$(curl -s -o /dev/null -w '%{http_code}' -X POST -H "Origin: http://127.0.0.1:${CTRL_PORT}" -H "Content-Type: application/json" -d "{\"id\":\"${DEV_ID}\"}" "http://127.0.0.1:${CTRL_PORT}/api/remote/revoke")"
  AFTER_DEV_REV_CODE="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer ${MOB_TOK}" "http://127.0.0.1:29900/mobile/api/state")"
  REV_ALL_CODE="$(curl -s -o /dev/null -w '%{http_code}' -X POST -H "Origin: http://127.0.0.1:${CTRL_PORT}" -H "Content-Type: application/json" -d '{}' "http://127.0.0.1:${CTRL_PORT}/api/remote/revoke")"
  if [[ "$MOB_STATE_CODE" == "200" && "$REV_DEV_CODE" == "200" && "$AFTER_DEV_REV_CODE" == "401" && "$REV_ALL_CODE" == "200" ]]; then
    ok "appearance_and_remote_pairing" "appearance hot update (original + legacy) + redacted status + QR SVG + mobile token auth & :29900 revoke verified"
  else
    fail "appearance_and_remote_pairing" "state=$MOB_STATE_CODE dev_rev=$REV_DEV_CODE after_dev=$AFTER_DEV_REV_CODE all_rev=$REV_ALL_CODE"
  fi
else
  fail "appearance_and_remote_pairing" "app=$APP_CODE legacy=$APP_LEGACY_CODE pair=$PAIR_CODE"
fi

# 14b. API contract integrity check (capture records, leaks host_direct_sockets, browser-pac missing field 400)
CAP_CONTRACT="$(curl -fsS "http://127.0.0.1:${CTRL_PORT}/api/capture")"
LEAK_CONTRACT="$(curl -fsS "http://127.0.0.1:${CTRL_PORT}/api/leaks")"
PAC_BAD_CODE="$(curl -s -o /dev/null -w '%{http_code}' -X POST -H "Origin: http://127.0.0.1:${CTRL_PORT}" -H "Content-Type: application/json" -d '{}' "http://127.0.0.1:${CTRL_PORT}/api/browser-pac")"
if [[ "$CAP_CONTRACT" == *'"records"'* && "$LEAK_CONTRACT" == *'"host_direct_sockets"'* && "$PAC_BAD_CODE" == "400" ]]; then
  ok "api_contract_integrity" "capture records, leaks host_direct_sockets, and strict /api/browser-pac 400 validation verified"
else
  fail "api_contract_integrity" "pac_bad=$PAC_BAD_CODE"
fi

# 15. Execute actual JavaScript FindProxyForURL from /peaceclaude.pac to verify strict Claude-only routing & DIRECT Google/regular browsing
PAC_JS="$(curl -fsS "http://127.0.0.1:${CTRL_PORT}/peaceclaude.pac")"
PAC_EVAL_RES="$(osascript -l JavaScript -e "$(cat <<JXA
function dnsDomainIs(host, domain) {
  return host === domain || (host.length > domain.length && host.slice(-domain.length) === domain);
}
function isPlainHostName(host) { return host.indexOf('.') === -1; }
function ipToNum(ip) {
  var p = ip.split('.');
  if (p.length !== 4) return 0;
  return (((+p[0]) << 24) | ((+p[1]) << 16) | ((+p[2]) << 8) | (+p[3])) >>> 0;
}
function isInNet(host, pattern, mask) {
  if (!/^\d+\.\d+\.\d+\.\d+$/.test(host)) return false;
  return (ipToNum(host) & ipToNum(mask)) === (ipToNum(pattern) & ipToNum(mask));
}
$PAC_JS
var proxied = ['claude.ai', 'claude.ai.', 'api.anthropic.com', 'claude.com', 'claudeusercontent.com', '160.79.104.10'];
var direct = ['accounts.google.com', 'accounts.google.com.', 'oauth2.googleapis.com', 'www.googleapis.com', 'www.google.com', 'google.com', 'challenges.cloudflare.com', 'github.com', 'update.googleapis.com', 'safebrowsing.googleapis.com', 'sentry.io'];
var bad = [];
proxied.forEach(function(h) {
  var r = FindProxyForURL('https://' + h + '/', h);
  if (r.indexOf('127.0.0.1:${HTTP_PORT}') === -1) bad.push('expected PROXY for ' + h + ', got ' + r);
});
direct.forEach(function(h) {
  var r = FindProxyForURL('https://' + h + '/', h);
  if (r !== 'DIRECT') bad.push('expected DIRECT for ' + h + ', got ' + r);
});
bad.length === 0 ? 'OK' : bad.join('; ');
JXA
)")"
if [[ "$PAC_EVAL_RES" == "OK" ]]; then
  ok "pac_js_execution_oauth_and_direct_google" "PAC JS routes strictly Claude domains/CIDRs while keeping Google OAuth, www.google.com, update.googleapis.com & sentry.io DIRECT"
else
  fail "pac_js_execution_oauth_and_direct_google" "$PAC_EVAL_RES"
fi

# 16. Live Anthropic static-IP proxy + direct Google OAuth chain + 6-way concurrent proxy load + zero stream leak verification
if [[ "$CAN_LIVE" == "1" ]]; then
  ANTHROPIC_CODE="$(curl -s -o /dev/null -w '%{http_code}' --max-time 12 -x "http://127.0.0.1:${HTTP_PORT}" "https://api.anthropic.com/" || true)"
  CLAUDE_STATUS_CODE="$(curl -s -o /dev/null -w '%{http_code}' --max-time 12 -x "http://127.0.0.1:${HTTP_PORT}" "https://claude.ai/cdn-cgi/trace" || true)"
  GOOGLE_AUTH_CODE="$(curl -s -o /dev/null -w '%{http_code}' --max-time 8 -x "http://127.0.0.1:${HTTP_PORT}" "https://accounts.google.com/gsi/client" || true)"
  CONC_FAIL=0
  CURL_PIDS=()
  for i in 1 2 3 4 5 6; do
    curl -fsS --max-time 14 -x "http://127.0.0.1:${HTTP_PORT}" "https://claude.ai/cdn-cgi/trace" >/dev/null &
    CURL_PIDS+=("$!")
  done
  for pid in "${CURL_PIDS[@]}"; do
    wait "$pid" || CONC_FAIL=$((CONC_FAIL + 1))
  done
  CURL_PIDS=()
  LEAKED_STREAMS=1
  for _ in 1 2 3 4 5 6; do
    POST_STATUS="$(curl -fsS "http://127.0.0.1:${CTRL_PORT}/api/status")"
    LEAKED_STREAMS="$(python3 -c 'import json,sys; d=json.loads(sys.stdin.read()); g=d.get("gate",{}); print(g.get("active_streams", -1))' <<<"$POST_STATUS")"
    if [[ "$LEAKED_STREAMS" == "0" ]]; then
      break
    fi
    sleep 0.4
  done
  if [[ "$ANTHROPIC_CODE" =~ ^(200|403|404)$ && "$CLAUDE_STATUS_CODE" == "200" && "$GOOGLE_AUTH_CODE" =~ ^(200|301|302|303)$ && "$CONC_FAIL" == "0" && "$LEAKED_STREAMS" == "0" ]]; then
    ok "live_oauth_chain_and_concurrency" "api.anthropic=$ANTHROPIC_CODE claude.ai=$CLAUDE_STATUS_CODE accounts.google(direct-auth)=$GOOGLE_AUTH_CODE, 6x concurrent OK, active_streams=0"
  else
    fail "live_oauth_chain_and_concurrency" "anthropic=$ANTHROPIC_CODE claude=$CLAUDE_STATUS_CODE google=$GOOGLE_AUTH_CODE conc_fail=$CONC_FAIL active_streams=$LEAKED_STREAMS"
  fi
fi

# 17. Ensure sandbox tests NEVER spawned or touched host Claude.app desktop processes
if ps -axo command= | grep -F "$TMP_HOME" | grep -qv grep; then
  fail "sandbox_zero_desktop_interference" "sandbox regression spawned unexpected process referencing $TMP_HOME"
else
  ok "sandbox_zero_desktop_interference" "zero sandbox interference on host Claude.app desktop"
fi

echo "=== Regression Summary: pass=$PASS fail=$FAIL ==="
exit "$FAIL"
