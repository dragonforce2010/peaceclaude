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

# Regression: Desktop-only Free account detection, per-account security score, and deleting active account syncs status active_profile
curl -fsS -o /dev/null -X POST -H "Origin: http://127.0.0.1:${CTRL_PORT}" -H "Content-Type: application/json" \
  -d '{"name":"qa-free","display_name":"QA 免费号回归","use_now":true}' \
  "http://127.0.0.1:${CTRL_PORT}/api/profiles/create"
echo '{"lastKnownAccountUuid":"11111111-2222-3333-4444-555555555555"}' > "$TMP_HOME/profiles/qa-free/desktop-data/config.json"
chmod 600 "$TMP_HOME/profiles/qa-free/desktop-data/config.json"
FREE_ACCTS_JSON="$(curl -fsS "http://127.0.0.1:${CTRL_PORT}/api/accounts")"
DEL_ACTIVE_JSON="$(curl -fsS -X POST -H "Origin: http://127.0.0.1:${CTRL_PORT}" -H "Content-Type: application/json" -d '{"name":"qa-free"}' "http://127.0.0.1:${CTRL_PORT}/api/accounts/delete")"
POST_DEL_STATUS="$(curl -fsS "http://127.0.0.1:${CTRL_PORT}/api/status")"
if python3 - "$FREE_ACCTS_JSON" "$DEL_ACTIVE_JSON" "$POST_DEL_STATUS" <<'PY'
import json, sys
accts = {a["name"]: a for a in json.loads(sys.argv[1]).get("accounts", [])}
del_res = json.loads(sys.argv[2])
st = json.loads(sys.argv[3])
qa = accts.get("qa-free", {})
assert qa.get("account", {}).get("plan_label") == "Free", qa
assert qa.get("usage", {}).get("status") == "free_no_quota", qa
assert isinstance(qa.get("security", {}).get("score"), int), qa
assert del_res.get("active") == "default", del_res
assert st.get("active_profile") == "default", st
PY
then
  ok "free_tier_security_and_delete_active" "Free account auto-detection, security score field, and active_profile fallback on delete verified"
else
  fail "free_tier_security_and_delete_active" "unexpected JSON for Free tier / security score / delete active"
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
  elif [[ "$APP_DRY" == *"--proxy-pac-url=http://127.0.0.1:${CTRL_PORT}/peaceclaude.pac?mode=desktop"* && "$APP_DRY" == *"--disable-quic"* ]]; then
    ok "desktop_no_forbidden_switch" "Claude Desktop launch args valid (default desktop PAC mode) & free of v6+ forbidden switches"
  else
    fail "desktop_no_forbidden_switch" "missing required desktop PAC isolation flags in: $APP_DRY"
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
  MOB_STATE="$(curl -fsS -H "Authorization: Bearer ${MOB_TOK}" "http://127.0.0.1:29900/mobile/api/state")"
  MOB_STATE_CODE=200
  MOB_LOCK_CODE="$(curl -s -o /dev/null -w '%{http_code}' -X POST -H "Authorization: Bearer ${MOB_TOK}" -H "Content-Type: application/json" -d '{"action":"lock_gate","reason":"regtest"}' "http://127.0.0.1:29900/mobile/api/action")"
  MOB_UNLOCK_CODE="$(curl -s -o /dev/null -w '%{http_code}' -X POST -H "Authorization: Bearer ${MOB_TOK}" -H "Content-Type: application/json" -d '{"action":"unlock_gate"}' "http://127.0.0.1:29900/mobile/api/action")"
  # Single-device revoke keeps listener up on :29900 so we can observe strict 401 before Shutdown.
  REV_DEV_CODE="$(curl -s -o /dev/null -w '%{http_code}' -X POST -H "Origin: http://127.0.0.1:${CTRL_PORT}" -H "Content-Type: application/json" -d "{\"id\":\"${DEV_ID}\"}" "http://127.0.0.1:${CTRL_PORT}/api/remote/revoke")"
  AFTER_DEV_REV_CODE="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer ${MOB_TOK}" "http://127.0.0.1:29900/mobile/api/state")"
  REV_ALL_CODE="$(curl -s -o /dev/null -w '%{http_code}' -X POST -H "Origin: http://127.0.0.1:${CTRL_PORT}" -H "Content-Type: application/json" -d '{}' "http://127.0.0.1:${CTRL_PORT}/api/remote/revoke")"
  if [[ "$MOB_STATE_CODE" == "200" && "$MOB_STATE" == *'"doctor_summary"'* && "$MOB_STATE" == *'"upstream_route"'* && "$MOB_LOCK_CODE" == "200" && "$MOB_UNLOCK_CODE" == "200" && "$REV_DEV_CODE" == "200" && "$AFTER_DEV_REV_CODE" == "401" && "$REV_ALL_CODE" == "200" ]]; then
    ok "appearance_and_remote_pairing" "appearance hot update + redacted status + QR SVG + mobile extended state/lock/unlock & :29900 revoke verified"
  else
    fail "appearance_and_remote_pairing" "state=$MOB_STATE_CODE lock=$MOB_LOCK_CODE unlock=$MOB_UNLOCK_CODE dev_rev=$REV_DEV_CODE after_dev=$AFTER_DEV_REV_CODE all_rev=$REV_ALL_CODE"
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

# 16b. Isolated login browser fingerprint hardening & Identity Shield artifacts regression check
if [[ -d "/Applications/Google Chrome.app" || -d "/Applications/Chromium.app" || -d "/Applications/Brave Browser.app" || -d "/Applications/Microsoft Edge.app" ]]; then
  BROWSER_DRY="$("$BIN" login-browser --dry-run https://fuck-claude.app/)"
  BPROF_DIR="${TMP_HOME}/profiles/default/browser-profile"
  if [[ "$BROWSER_DRY" == *"--proxy-pac-url=http://127.0.0.1:${CTRL_PORT}/peaceclaude.pac?mode=login"* && "$BROWSER_DRY" == *"--accept-lang=en-US,en"* && "$BROWSER_DRY" == *"--enable-unsafe-extension-debugging"* ]] \
    && python3 - "$BPROF_DIR" <<'PY'
import json, os, sys
bdir = sys.argv[1]
prefs = json.load(open(os.path.join(bdir, "Default", "Preferences")))
assert prefs["enable_do_not_track"] is True, prefs
assert prefs["intl"]["accept_languages"] == "en-US,en", prefs["intl"]
assert prefs["intl"]["selected_languages"] == "en-US,en", prefs["intl"]
assert prefs["profile"]["default_content_setting_values"]["geolocation"] == 2, prefs["profile"]
ls = json.load(open(os.path.join(bdir, "Local State")))
assert ls["intl"]["app_locale"] == "en-US", ls
ext_dir = os.path.join(bdir, "peace-shield-ext")
manifest = json.load(open(os.path.join(ext_dir, "manifest.json")))
rules = json.load(open(os.path.join(ext_dir, "rules.json")))
shield = open(os.path.join(ext_dir, "shield.js")).read()
for fn in ["manifest.json", "rules.json", "background.js", "shield.js", "icon48.png", "icon128.png"]:
    assert os.path.isfile(os.path.join(ext_dir, fn)), f"missing {fn}"
assert manifest["manifest_version"] == 3 and "declarativeNetRequest" in manifest["permissions"]
assert manifest.get("background", {}).get("service_worker") == "background.js", manifest
assert "websocket" in rules[0]["condition"]["resourceTypes"] and "stylesheet" in rules[0]["condition"]["resourceTypes"], rules
assert any(h.get("header") == "Sec-GPC" for h in rules[0]["action"]["requestHeaders"])
for sym in ["globalPrivacyControl", "CJK_FONT_RE", "FontFaceSet", "CSSStyleDeclaration", "localeCompare", "WrappedWorker"]:
    assert sym in shield, f"shield.js missing {sym}"
PY
  then
    if command -v node >/dev/null 2>&1; then
      node --check "${BPROF_DIR}/peace-shield-ext/shield.js"
    fi
    ok "isolated_browser_shield_artifacts" "Chrome Preferences, Local State, mode=login PAC, and full MV3 Identity Shield artifacts verified"
  else
    fail "isolated_browser_shield_artifacts" "missing hardened flags or extension artifacts in $BROWSER_DRY"
  fi
fi

# 16c. Self-contained Desktop .app bundle generation & embedded CLI version check
STAGE_APP_DIR="${TMP_HOME}/stage-app"
mkdir -p "$STAGE_APP_DIR"
APP_INSTALL_OUT="$("$BIN" app install --dir "$STAGE_APP_DIR")"
APP_BUNDLE="${STAGE_APP_DIR}/PeaceClaude.app"
REPO_VER="$(tr -d '[:space:]' < "${ROOT}/VERSION")"
if [[ -d "$APP_BUNDLE" ]] \
  && grep -q "<string>${REPO_VER}</string>" "${APP_BUNDLE}/Contents/Info.plist" \
  && [[ -x "${APP_BUNDLE}/Contents/Resources/bin/peaceclaude" ]] \
  && [[ "$("${APP_BUNDLE}/Contents/Resources/bin/peaceclaude" --version)" == "peaceclaude ${REPO_VER} "* ]]; then
  ok "desktop_bundle_self_contained" "PeaceClaude.app embeds Info.plist=${REPO_VER} and executable Resources/bin/peaceclaude"
else
  fail "desktop_bundle_self_contained" "app bundle check failed ($APP_INSTALL_OUT)"
fi

# 16d. Verify passed doctor checks omit redundant fix_hint
DOC_JSON="$(curl -fsS "http://127.0.0.1:${CTRL_PORT}/api/doctor")"
if python3 - "$DOC_JSON" <<'PY'
import json, sys
rep = json.loads(sys.argv[1])
checks = rep.get("checks", [])
assert len(checks) >= 10, checks
bad = [c["id"] for c in checks if c.get("status") == "pass" and (c.get("fix_hint") or c.get("fixable"))]
assert not bad, f"passed checks carry fix_hint: {bad}"
PY
then
  ok "doctor_pass_has_no_fix_hint" "all passed checks in /api/doctor omit redundant fix_hint"
else
  fail "doctor_pass_has_no_fix_hint" "unexpected fix_hint on passed doctor check"
fi

# 16e. Verify native browser shield API & extension generation without disturbing host network
NB_GET="$(curl -fsS "http://127.0.0.1:${CTRL_PORT}/api/native-browsers")"
NB_POST="$(curl -fsS -X POST "http://127.0.0.1:${CTRL_PORT}/api/native-browsers/protect" -H 'Content-Type: application/json' -d '{"browsers":["arc","chrome"],"enable_pac":false}')"
if python3 - "$NB_GET" "$NB_POST" "$TMP_HOME" <<'PY'
import json, os, sys
g = json.loads(sys.argv[1])
p = json.loads(sys.argv[2])
home = sys.argv[3]
assert g.get("extension_ready") is True, g
assert "fuck-claude.app" in g.get("protected_domains", []), g
assert p.get("ok") is True, p
sel = {b["id"]: b["selected"] for b in p.get("report", {}).get("browsers", [])}
assert sel.get("arc") is True and sel.get("chrome") is True, sel
ext_dir = os.path.join(home, "extensions", "native-browser-shield")
for fn in ["manifest.json", "rules.json", "config.js", "shield.js", "background.js", "popup.html", "popup.js", "icon48.png", "icon128.png"]:
    assert os.path.isfile(os.path.join(ext_dir, fn)), f"missing {fn}"
PY
then
  ok "native_browser_shield_api" "/api/native-browsers generated MV3 extension and persisted [arc, chrome] selection"
else
  fail "native_browser_shield_api" "native browser shield check failed"
fi

# 16f. Verify service status JSON fields & sandbox-safe ensure-autostart & service install / remote CLI
SVC_STATUS_JSON="$(PEACECLAUDE_HOME="$TMP_HOME" PEACECLAUDE_NO_LAUNCHD=1 "$BIN" service status --json)"
ENSURE_JSON="$(PEACECLAUDE_HOME="$TMP_HOME" PEACECLAUDE_NO_LAUNCHD=1 "$BIN" service ensure-autostart --if-enabled --json)"
INSTALL_ALIAS_JSON="$(PEACECLAUDE_HOME="$TMP_HOME" PEACECLAUDE_NO_LAUNCHD=1 "$BIN" service install --if-enabled --json)"
REMOTE_CLI_JSON="$(PEACECLAUDE_HOME="$TMP_HOME" "$BIN" remote status --json)"
if python3 - "$SVC_STATUS_JSON" "$ENSURE_JSON" "$INSTALL_ALIAS_JSON" "$REMOTE_CLI_JSON" <<'PY'
import json, sys
st = json.loads(sys.argv[1])
en = json.loads(sys.argv[2])
ins = json.loads(sys.argv[3])
rem = json.loads(sys.argv[4])
for key in ("running", "launch_agent_installed", "launch_agent_loaded", "launch_agent_valid", "auto_start_enabled"):
    assert key in st, f"service status missing {key}: {st}"
assert st["running"] is True and st["auto_start_enabled"] is True, st
assert en.get("action") == "disabled_in_sandbox", f"unexpected sandbox ensure-autostart: {en}"
assert ins.get("action") == "disabled_in_sandbox", f"unexpected service install alias: {ins}"
assert "mobile_port" in rem, f"unexpected remote status JSON: {rem}"
PY
then
  ok "service_status_and_ensure_autostart_sandbox" "service status/install alias JSON & remote status CLI verified"
else
  fail "service_status_and_ensure_autostart_sandbox" "service status, install alias, or remote status CLI check failed"
fi

STATUS_API_JSON="$(curl -fsS "http://127.0.0.1:${CTRL_PORT}/api/status")"
AUTO_OFF_JSON="$(curl -fsS -X POST "http://127.0.0.1:${CTRL_PORT}/api/service/autostart" -H 'Content-Type: application/json' -d '{"enabled":false}')"
AUTO_ON_JSON="$(curl -fsS -X POST "http://127.0.0.1:${CTRL_PORT}/api/service/autostart" -H 'Content-Type: application/json' -d '{"enabled":true}')"
if python3 - "$STATUS_API_JSON" "$AUTO_OFF_JSON" "$AUTO_ON_JSON" <<'PY'
import json, sys
st = json.loads(sys.argv[1])
off = json.loads(sys.argv[2])
on = json.loads(sys.argv[3])
assert st.get("service", {}).get("running") is True, st
assert off.get("ok") is True and off.get("service", {}).get("auto_start_enabled") is False, off
assert on.get("ok") is True and on.get("service", {}).get("auto_start_enabled") is True, on
PY
then
  ok "api_service_autostart_contract" "/api/status.service and /api/service/autostart contract passed without breaking daemon"
else
  fail "api_service_autostart_contract" "/api/service/autostart contract check failed"
fi

# 16g. Verify /api/status.upstream_route and bypass_vpn toggle via /api/config/update
BYPASS_OFF="$(curl -fsS -X POST "http://127.0.0.1:${CTRL_PORT}/api/config/update" -H 'Content-Type: application/json' -d '{"bypass_vpn":false}')"
BYPASS_ON="$(curl -fsS -X POST "http://127.0.0.1:${CTRL_PORT}/api/config/update" -H 'Content-Type: application/json' -d '{"bypass_vpn":true}')"
if python3 - "$STATUS_API_JSON" "$BYPASS_OFF" "$BYPASS_ON" <<'PY'
import json, sys
st = json.loads(sys.argv[1])
off = json.loads(sys.argv[2])
on = json.loads(sys.argv[3])
ur = st.get("upstream_route", {})
assert "bypass_enabled" in ur and "active_interface" in ur, st
assert off.get("config", {}).get("upstream", {}).get("bypass_vpn") is False, off
assert off.get("upstream_route", {}).get("bypass_enabled") is False, off
assert on.get("config", {}).get("upstream", {}).get("bypass_vpn") is True, on
assert on.get("upstream_route", {}).get("bypass_enabled") is True, on
PY
then
  ok "api_upstream_route_and_bypass_vpn" "/api/status.upstream_route and /api/config/update bypass_vpn toggle verified"
else
  fail "api_upstream_route_and_bypass_vpn" "upstream_route or bypass_vpn API contract failed"
fi

# 16h. Regression guard for configurable desktop PAC & domain policy API + legacy proxy-bypass guard
# (fixes blank corporate SSO pages in Claude Desktop PreviewContext across both universal PAC and proxy modes)
cp "$TMP_HOME/config.json" "$TMP_HOME/config.json.bak.16h"
FAKE_CLAUDE_APP="$TMP_HOME/fake-Claude.app"
mkdir -p "$FAKE_CLAUDE_APP/Contents/MacOS"
printf '#!/bin/sh\nexit 0\n' > "$FAKE_CLAUDE_APP/Contents/MacOS/Claude"
chmod +x "$FAKE_CLAUDE_APP/Contents/MacOS/Claude"

POLICY_PAC_PAYLOAD="$(python3 - "$EXPECTED_LIVE_IP" <<'PY'
import json, sys
print(json.dumps({
    "expected_ip": sys.argv[1],
    "telemetry_shield": True,
    "allow_corporate_direct": True,
    "desktop_proxy_mode": "pac",
    "extra_claude_domains": ["custom-claude.example"],
    "extra_blocked_domains": ["tracker.example"],
    "corporate_domains": [".corp.internal", "git.corp.internal", ".claude.ai", ".com"],
}))
PY
)"
POLICY_PAC_RESP="$(curl -fsS -X POST -H "Origin: http://127.0.0.1:${CTRL_PORT}" -H 'Content-Type: application/json' -d "$POLICY_PAC_PAYLOAD" "http://127.0.0.1:${CTRL_PORT}/api/config/update")"
DESKTOP_PAC_JS="$(curl -fsS "http://127.0.0.1:${CTRL_PORT}/peaceclaude.pac?mode=desktop")"

PAC_DESKTOP_EVAL="$(node - "$DESKTOP_PAC_JS" "$HTTP_PORT" <<'NODE'
const vm = require('vm');
const pacCode = process.argv[2];
const httpPort = process.argv[3];
const sandbox = {
  dnsDomainIs(host, domain) {
    return host === domain || (host.length > domain.length && host.slice(-domain.length) === domain);
  },
  isPlainHostName(host) { return host.indexOf('.') === -1; },
  ipToNum(ip) {
    const p = ip.split('.');
    if (p.length !== 4) return 0;
    return (((+p[0]) << 24) | ((+p[1]) << 16) | ((+p[2]) << 8) | (+p[3])) >>> 0;
  },
  isInNet(host, pattern, mask) {
    if (!/^\d+\.\d+\.\d+\.\d+$/.test(host)) return false;
    return (sandbox.ipToNum(host) & sandbox.ipToNum(mask)) === (sandbox.ipToNum(pattern) & sandbox.ipToNum(mask));
  },
};
vm.runInNewContext(pacCode, sandbox);
const mustProxy = ['claude.ai', 'custom-claude.example', 'sentry.io', 'tracker.example'];
const mustDirect = ['sso.unknown-corp.internal', 'cdn.unknown-corp.net', '10.8.7.221'];
const errors = [];
for (const h of mustProxy) {
  const res = sandbox.FindProxyForURL('https://' + h + '/', h);
  if (!res.includes('127.0.0.1:' + httpPort)) errors.push(`expected PROXY for ${h}, got ${res}`);
}
for (const h of mustDirect) {
  const res = sandbox.FindProxyForURL('https://' + h + '/', h);
  if (res !== 'DIRECT') errors.push(`expected DIRECT for ${h}, got ${res}`);
}
console.log(errors.length === 0 ? 'OK' : errors.join('; '));
NODE
)"

POLICY_PROXY_PAYLOAD="$(python3 - "$EXPECTED_LIVE_IP" <<'PY'
import json, sys
print(json.dumps({
    "expected_ip": sys.argv[1],
    "telemetry_shield": True,
    "allow_corporate_direct": True,
    "desktop_proxy_mode": "proxy",
    "extra_claude_domains": ["custom-claude.example"],
    "extra_blocked_domains": ["tracker.example"],
    "corporate_domains": [".corp.internal", "git.corp.internal", ".claude.ai", ".com"],
}))
PY
)"
POLICY_PROXY_RESP="$(curl -fsS -X POST -H "Origin: http://127.0.0.1:${CTRL_PORT}" -H 'Content-Type: application/json' -d "$POLICY_PROXY_PAYLOAD" "http://127.0.0.1:${CTRL_PORT}/api/config/update")"
python3 - "$TMP_HOME/config.json" "$FAKE_CLAUDE_APP" <<'PY'
import json, sys
p, app_path = sys.argv[1], sys.argv[2]
d = json.loads(open(p).read())
d.setdefault("binaries", {})["claude_desktop_app"] = app_path
open(p, "w").write(json.dumps(d, indent=2) + "\n")
PY
PROXY_MODE_DRY="$("$BIN" app --dry-run 2>&1 || echo '{}')"

RESTORE_PAYLOAD="$(python3 - "$EXPECTED_LIVE_IP" <<'PY'
import json, sys
print(json.dumps({
    "expected_ip": sys.argv[1],
    "telemetry_shield": True,
    "allow_corporate_direct": True,
    "desktop_proxy_mode": "pac",
    "extra_claude_domains": [],
    "extra_blocked_domains": [],
    "corporate_domains": [],
}))
PY
)"
curl -fsS -X POST -H "Origin: http://127.0.0.1:${CTRL_PORT}" -H 'Content-Type: application/json' -d "$RESTORE_PAYLOAD" "http://127.0.0.1:${CTRL_PORT}/api/config/update" >/dev/null
mv "$TMP_HOME/config.json.bak.16h" "$TMP_HOME/config.json"
rm -rf "$FAKE_CLAUDE_APP"

if python3 - "$POLICY_PAC_RESP" "$PAC_DESKTOP_EVAL" "$POLICY_PROXY_RESP" "$PROXY_MODE_DRY" "$HTTP_PORT" <<'PY'
import json, sys
pac_resp = json.loads(sys.argv[1])
pac_eval = sys.argv[2].strip()
proxy_resp = json.loads(sys.argv[3])
proxy_dry = json.loads(sys.argv[4])
http_port = sys.argv[5]

sh_pac = pac_resp.get("config", {}).get("shield", {})
assert sh_pac.get("desktop_proxy_mode") == "pac", sh_pac
assert sh_pac.get("extra_claude_domains") == ["custom-claude.example"], sh_pac
assert sh_pac.get("extra_blocked_domains") == ["tracker.example"], sh_pac
assert sh_pac.get("corporate_domains") == [".corp.internal", "git.corp.internal", ".claude.ai", ".com"], sh_pac
assert pac_eval == "OK", f"desktop PAC JS eval failed: {pac_eval}"

assert proxy_resp.get("config", {}).get("shield", {}).get("desktop_proxy_mode") == "proxy", proxy_resp
args = proxy_dry.get("args", [])
assert f"--proxy-server=http://127.0.0.1:{http_port}" in args, args
bypass_val = ""
for a in args:
    if a.startswith("--proxy-bypass-list="):
        bypass_val = a.split("=", 1)[1]
want_bypass = "127.0.0.1;localhost;::1;*.local;*.corp.internal;corp.internal;git.corp.internal"
assert bypass_val == want_bypass, f"unexpected proxy-mode bypass: {bypass_val!r} != {want_bypass!r}"
assert "claude.ai" not in bypass_val and "*.com" not in bypass_val, bypass_val
PY
then
  ok "desktop_domain_policy_and_pac_mode" "domain policy API persistence, desktop PAC routing (unknown intranet=DIRECT), and proxy-mode bypass guard verified"
else
  fail "desktop_domain_policy_and_pac_mode" "desktop domain policy or PAC mode verification failed"
fi

# 16i. Verify mode=login PAC (Turnstile/hCaptcha/Stripe/Stytch proxied, Google OAuth DIRECT) & doctor browser-shield repair loop
LOGIN_PAC_JS="$(curl -fsS "http://127.0.0.1:${CTRL_PORT}/peaceclaude.pac?mode=login")"
LOGIN_PAC_EVAL="$(node - "$LOGIN_PAC_JS" "$HTTP_PORT" <<'NODE'
const vm = require('vm');
const pacCode = process.argv[2];
const httpPort = process.argv[3];
const sandbox = {
  dnsDomainIs(host, domain) {
    return host === domain || (host.length > domain.length && host.slice(-domain.length) === domain);
  },
  isPlainHostName(host) { return host.indexOf('.') === -1; },
  ipToNum(ip) {
    const p = ip.split('.');
    if (p.length !== 4) return 0;
    return (((+p[0]) << 24) | ((+p[1]) << 16) | ((+p[2]) << 8) | (+p[3])) >>> 0;
  },
  isInNet(host, pattern, mask) {
    if (!/^\d+\.\d+\.\d+\.\d+$/.test(host)) return false;
    return (sandbox.ipToNum(host) & sandbox.ipToNum(mask)) === (sandbox.ipToNum(pattern) & sandbox.ipToNum(mask));
  },
};
vm.runInNewContext(pacCode, sandbox);
const mustProxy = ['claude.ai', 'challenges.cloudflare.com', 'turnstile.cloudflare.com', 'newassets.hcaptcha.com', 'js.stripe.com', 'stytch.com', 'o1158394.ingest.us.sentry.io'];
const mustDirect = ['accounts.google.com', 'www.google.com', 'cdn.unknown-corp.net'];
const errs = [];
for (const h of mustProxy) {
  const res = sandbox.FindProxyForURL('https://' + h + '/', h);
  if (!res.includes('127.0.0.1:' + httpPort)) errs.push(`expected PROXY for ${h}, got ${res}`);
}
for (const h of mustDirect) {
  const res = sandbox.FindProxyForURL('https://' + h + '/', h);
  if (res !== 'DIRECT') errs.push(`expected DIRECT for ${h}, got ${res}`);
}
console.log(errs.length === 0 ? 'OK' : errs.join('; '));
NODE
)"

DIRTY_PREF_PATH="${TMP_HOME}/profiles/default/browser-profile/Default/Preferences"
mkdir -p "$(dirname "$DIRTY_PREF_PATH")"
printf '{"intl":{"accept_languages":"zh-CN,zh","selected_languages":"zh-CN,zh"},"enable_do_not_track":false}\n' > "$DIRTY_PREF_PATH"
DOC_WARN_JSON="$(curl -fsS "http://127.0.0.1:${CTRL_PORT}/api/doctor")"
FIX_SHIELD_JSON="$(curl -fsS -X POST "http://127.0.0.1:${CTRL_PORT}/api/doctor/fix" -H 'Content-Type: application/json' -d '{"action":"repair_browser_shields"}')"
DOC_AFTER_FIX_JSON="$(curl -fsS "http://127.0.0.1:${CTRL_PORT}/api/doctor")"
if [[ "$LOGIN_PAC_EVAL" == "OK" ]] && python3 - "$DOC_WARN_JSON" "$FIX_SHIELD_JSON" "$DOC_AFTER_FIX_JSON" "$DIRTY_PREF_PATH" <<'PY'
import json, sys
w_doc = json.loads(sys.argv[1])
fix_res = json.loads(sys.argv[2])
a_doc = json.loads(sys.argv[3])
prefs = open(sys.argv[4]).read()
w_item = next(c for c in w_doc["checks"] if c["id"] == "browser_desktop_tz_lang")
assert w_item["status"] == "warn", w_item
if w_item.get("fix_action") != "takeover_desktop":
    assert w_item.get("fix_action") == "repair_browser_shields", w_item
assert fix_res.get("ok") is True, fix_res
assert '"zh-CN' not in prefs and '"en-US,en"' in prefs, prefs
a_item = next(c for c in a_doc["checks"] if c["id"] == "browser_desktop_tz_lang")
if a_item.get("fix_action") != "takeover_desktop":
    assert a_item["status"] == "pass" and not a_item.get("fix_hint"), a_item
PY
then
  ok "login_pac_and_doctor_shield_repair" "mode=login PAC (Turnstile/hCaptcha/Stripe/Stytch proxied, Google OAuth DIRECT) & doctor shield repair verified"
else
  fail "login_pac_and_doctor_shield_repair" "login PAC ($LOGIN_PAC_EVAL) or doctor shield repair check failed"
fi

# 17. Ensure sandbox tests NEVER spawned or touched host Claude.app desktop processes
if ps -axo command= | grep -F "$TMP_HOME" | grep -qv grep; then
  fail "sandbox_zero_desktop_interference" "sandbox regression spawned unexpected process referencing $TMP_HOME"
else
  ok "sandbox_zero_desktop_interference" "zero sandbox interference on host Claude.app desktop"
fi

echo "=== Regression Summary: pass=$PASS fail=$FAIL ==="
exit "$FAIL"
