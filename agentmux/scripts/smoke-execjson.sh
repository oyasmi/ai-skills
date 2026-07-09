#!/bin/bash
# Full smoke test for codex-cli-execjson against the real Codex CLI.
set -uo pipefail

# Requires a logged-in `codex` CLI. Consumes real tokens.
#   go build -o ./bin/agentmux ./cmd/agentmux && ./scripts/smoke-execjson.sh
# Override the binary with AGENTMUX_BIN, the scratch root with SMOKE_TMPDIR.
REPO=$(cd "$(dirname "$0")/.." && pwd)
ROOT="${SMOKE_TMPDIR:-$(mktemp -d -t agentmux-smoke-XXXXXX)}"
export XDG_CONFIG_HOME=$ROOT/config XDG_STATE_HOME=$ROOT/state
WORK=$ROOT/work
AM="${AGENTMUX_BIN:-$REPO/bin/agentmux}"
mkdir -p "$WORK" "$XDG_CONFIG_HOME" "$XDG_STATE_HOME"
cd "$WORK" || exit 1

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); printf '  \033[32mPASS\033[0m %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  \033[31mFAIL\033[0m %s\n     got: %s\n' "$1" "$2"; }
check(){ [ "$2" = "$3" ] && ok "$1" || bad "$1" "$2 (want $3)"; }
step() { printf '\n\033[1m== %s\033[0m\n' "$1"; }

# ---------------------------------------------------------------- 1. summon
step "1. summon spawns no process"
S=$($AM summon --template codex-cli-execjson --name smoke --json)
check "status is idle"        "$(jq -r .status <<<"$S")" "idle"
check "harness_type recorded" "$(jq -r .data.harness_type <<<"$S")" "codex-cli-execjson"
L=$($AM list --json)
check "process_id absent/0"   "$(jq -r '.data.instances[0].process_id // 0' <<<"$L")" "0"
check "thread_id not yet set" "$(jq -r '.data.instances[0].thread_id // "none"' <<<"$L")" "none"

step "2. config validation rejects bad prefixes at summon"
E=$($AM summon --template codex-cli-execjson --name bad1 --command 'codex exec --ask-for-approval never' --json)
check "--ask-for-approval rejected" "$(jq -r .error_code <<<"$E")" "config_invalid"
E=$($AM summon --template codex-cli-execjson --name bad2 --command 'codex exec --ephemeral' --json)
check "--ephemeral rejected"        "$(jq -r .error_code <<<"$E")" "config_invalid"
E=$($AM summon --template codex-cli-execjson --name bad3 --command 'codex exec --json' --json)
check "--json rejected"             "$(jq -r .error_code <<<"$E")" "config_invalid"
E=$($AM summon --template codex-cli-execjson --name bad4 --command 'codex --model x' --json)
check "missing exec rejected"       "$(jq -r .error_code <<<"$E")" "config_invalid"
E=$($AM summon --template codex-cli-execjson --name bad5 --command 'codex exec | tee x' --json)
check "pipe rejected"               "$(jq -r .error_code <<<"$E")" "config_invalid"

# ---------------------------------------------------------------- 3. turn 0
step "3. turn 0: prompt -> wait -> capture"
P=$($AM prompt smoke --text "Reply with exactly one word: alpha" --json)
check "prompt marks busy" "$(jq -r .status <<<"$P")" "busy"
BUSY=$($AM list --json | jq -r '.data.instances[0].process_id // 0')
[ "$BUSY" -gt 0 ] && ok "busy instance exposes a live pid ($BUSY)" || bad "busy pid" "$BUSY"

step "3b. prompt while busy is rejected, nothing sent"
B=$($AM prompt smoke --text "should not be sent" --json); RC=$?
check "error_code"  "$(jq -r .error_code <<<"$B")" "execjson_instance_busy"
check "exit code 1" "$RC" "1"

W=$($AM wait smoke --timeout 240s --json)
check "wait -> idle"        "$(jq -r .status <<<"$W")" "idle"
check "stable_for_ms is 0"  "$(jq -r .data.stable_for_ms <<<"$W")" "0"

C=$($AM capture smoke --json)
check "content is alpha"      "$(jq -r .data.content <<<"$C" | tr -d '[:space:]' | tr 'A-Z' 'a-z')" "alpha"
check "turn_state completed"  "$(jq -r .data.turn_state <<<"$C")" "completed"
check "last_error empty"      "$(jq -r .data.last_error <<<"$C")" ""
check "turns == 1"            "$(jq -r .data.turns <<<"$C")" "1"
check "no cost field lying"   "$(jq -r .data.usage.total_cost_usd <<<"$C")" "0"
IN=$(jq -r .data.usage.input_tokens <<<"$C"); [ "$IN" -gt 0 ] && ok "usage.input_tokens > 0 ($IN)" || bad "input_tokens" "$IN"
TID=$(jq -r .data.thread_id <<<"$C")
[ -n "$TID" ] && [ "$TID" != "null" ] && ok "thread_id recorded ($TID)" || bad "thread_id" "$TID"

step "3c. between turns: idle, no process, still listed"
L=$($AM list --json)
check "instance survives list" "$(jq -r '.data.instances | map(select(.name=="smoke")) | length' <<<"$L")" "1"
check "status idle"            "$(jq -r '.data.instances[] | select(.name=="smoke") | .status' <<<"$L")" "idle"
check "process_id back to 0"   "$(jq -r '.data.instances[] | select(.name=="smoke") | .process_id // 0' <<<"$L")" "0"
for i in 1 2 3; do $AM list --json >/dev/null; done
check "repeated list does not prune" "$($AM list --json | jq -r '.data.instances | length')" "1"

# ---------------------------------------------------------------- 4. resume
step "4. turn 1 resumes the thread (cross-process memory)"
$AM prompt smoke --text "What word did you just say? Reply with that one word only." --json >/dev/null
$AM wait smoke --timeout 240s --json >/dev/null
C=$($AM capture smoke --json)
check "recalls turn 0 answer" "$(jq -r .data.content <<<"$C" | tr -d '[:space:]' | tr 'A-Z' 'a-z')" "alpha"
check "turns == 2"            "$(jq -r .data.turns <<<"$C")" "2"
check "same thread_id"        "$(jq -r .data.thread_id <<<"$C")" "$TID"

D=$(jq -r '.data.instances[] | select(.name=="smoke") | .transport_dir' <<<"$($AM list --json)")
grep -q "resume '$TID'" "$D/turns/001.run.sh" && ok "turn 1 run.sh uses resume" || bad "run.sh resume" "$(cat "$D/turns/001.run.sh")"
grep -q "resume" "$D/turns/000.run.sh" && bad "turn 0 must not resume" "$(cat "$D/turns/000.run.sh")" || ok "turn 0 run.sh has no resume"
check "two thread.started, one id" "$(jq -r 'select(.type=="thread.started")|.thread_id' "$D/output.jsonl" | sort -u | wc -l)" "1"
check "thread.started seen twice"  "$(jq -c 'select(.type=="thread.started")' "$D/output.jsonl" | wc -l | tr -d ' ')" "2"
check "exit code 0 for turn 0" "$(cat "$D/turns/000.exit")" "0"
check "exit code 0 for turn 1" "$(cat "$D/turns/001.exit")" "0"
grep -q "Reading additional input from stdin" "$D/stderr.log" && bad "stdin dangled" "$(grep -c . "$D/stderr.log")" || ok "stdin redirected (no stdin-read warning)"

# ------------------------------------------------------- 5. real tool work
step "5. sandbox workspace-write: agent actually does work"
$AM prompt smoke --text "Create a file named proof.txt in the current directory whose only content is the word verified. Then stop." --json >/dev/null
$AM wait smoke --timeout 240s --json >/dev/null
if [ -f "$WORK/proof.txt" ]; then
  ok "agent wrote proof.txt ($(tr -d '[:space:]' < "$WORK/proof.txt"))"
else
  bad "proof.txt missing" "$(ls "$WORK")"
fi
C=$($AM capture smoke --json)
TOOLS=$(jq -r '[.data.messages[] | select(.type=="tool_use")] | length' <<<"$C")
[ "$TOOLS" -gt 0 ] && ok "capture surfaces tool_use messages ($TOOLS)" || bad "tool_use messages" "$TOOLS"

# ------------------------------------------------- 6. failure stays usable
step "6. failed turn keeps the instance usable"
$AM summon --template codex-cli-execjson --name failer --command 'codex exec --sandbox read-only --skip-git-repo-check --model definitely-not-a-real-model-xyz' --json >/dev/null
$AM prompt failer --text "hi" --json >/dev/null
W=$($AM wait failer --timeout 120s --json)
check "wait still succeeds on a failed turn" "$(jq -r .ok <<<"$W")" "true"
C=$($AM capture failer --json)
check "turn_state failed" "$(jq -r .data.turn_state <<<"$C")" "failed"
LE=$(jq -r .data.last_error <<<"$C")
[ -n "$LE" ] && [ "$LE" != "null" ] && ok "last_error populated" || bad "last_error" "$LE"
check "instance still idle" "$(jq -r '.data.instances[] | select(.name=="failer") | .status' <<<"$($AM list --json)")" "idle"
P=$($AM prompt failer --text "retry" --json)
check "still promptable after failure" "$(jq -r .ok <<<"$P")" "true"
$AM halt failer --immediately --json >/dev/null

# ------------------------------------------------------------ 7. interrupt
step "7. C-c cancels a running turn"
$AM prompt smoke --text "Count from 1 to 500 slowly, printing each number on its own line with a short comment about it." --json >/dev/null
sleep 3
PID=$($AM list --json | jq -r '.data.instances[] | select(.name=="smoke") | .process_id // 0')
[ "$PID" -gt 0 ] && ok "turn running (pid $PID)" || bad "expected a running turn" "$PID"
K=$($AM prompt smoke --key C-c --json)
check "interrupt returns idle" "$(jq -r .status <<<"$K")" "idle"
kill -0 "$PID" 2>/dev/null && bad "turn process still alive" "$PID" || ok "turn process gone"
LAST=$(jq -r '.turns[-1].state' "$D/state.json")
check "last turn cancelled" "$LAST" "cancelled"
check "instance idle after interrupt" "$(jq -r '.data.instances[] | select(.name=="smoke") | .status' <<<"$($AM list --json)")" "idle"
P=$($AM prompt smoke --text "ok" --json)
check "promptable after interrupt" "$(jq -r .ok <<<"$P")" "true"
$AM wait smoke --timeout 240s --json >/dev/null

# ----------------------------------------------------------------- 8. halt
step "8. halt"
H=$($AM halt smoke --json)
check "halt reports exited" "$(jq -r .status <<<"$H")" "exited"
check "registry emptied"    "$($AM list --json | jq -r '.data.instances | length')" "0"
check "state marked exited" "$(jq -r .status "$D/state.json")" "exited"
check "audit logs kept"     "$([ -s "$D/output.jsonl" ] && echo yes)" "yes"
pgrep -f "codex exec" >/dev/null && bad "orphan codex processes" "$(pgrep -fa 'codex exec' | head -3)" || ok "no orphan process group"

printf '\n\033[1m==== %d passed, %d failed ====\033[0m\nstate: %s\n' "$PASS" "$FAIL" "$ROOT"
exit $((FAIL > 0))
