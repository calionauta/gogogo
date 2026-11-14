#!/usr/bin/env bash
# verify-local — boot the REAL binary on a private data dir and hand the operator
# a URL plus a 60-second checklist of what to click.
#
# Why this exists: the gates (ci-local-fast, ci-local, PR CI) prove what a
# machine can assert. They cannot tell you the flow feels wrong, the label is
# ambiguous, or the feature is not what was asked. That judgment is the operator's
# and it is the only one no gate replaces — so it belongs at the moment a
# rejection is cheapest: after the cheap gate is green and BEFORE `ci-local`
# (~6 min) and a PR round trip.
#
# The division of labour, stated once:
#   1. the AGENT decides the rule fired (see docs/local-ci.md, "When the operator
#      must click") and ASKS the operator whether to run this;
#   2. THIS script builds, seeds an account, prints the URL and the routes the
#      diff touched, and waits;
#   3. the operator clicks, answers one word, and the script tears everything down.
#
# Nothing here touches the operator's data: a fresh mktemp dir is the database,
# removed on exit. Never point it at ~/ data or a live instance.
#
# Usage:
#   scripts/verify-local.sh                # build, boot :8098, print URL + checklist
#   scripts/verify-local.sh --port 9100
#   scripts/verify-local.sh --share        # also expose over the tailnet (Tailscale)
#   scripts/verify-local.sh --ttl 15       # auto-stop after 15 minutes
#   scripts/verify-local.sh --yes          # skip the confirmation prompt (agents)
set -euo pipefail

port=8098
share=0
ttl_min=30
assume_yes=0
email="verify@local.dev"
password="Verify-local-99!"

while [ $# -gt 0 ]; do
	case "$1" in
	--port) port="${2:?--port needs a number}"; shift 2 ;;
	--ttl) ttl_min="${2:?--ttl needs minutes}"; shift 2 ;;
	--share) share=1; shift ;;
	--yes | -y) assume_yes=1; shift ;;
	-h | --help) sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
	*) echo "verify-local: unknown argument $1" >&2; exit 2 ;;
	esac
done

root=$(git rev-parse --show-toplevel 2>/dev/null || true)
if [ -z "$root" ] || [ ! -d "$root/cmd/web" ]; then
	echo "verify-local: run this inside a gogogo checkout (no cmd/web found)" >&2
	exit 2
fi
cd "$root"

if [ "$assume_yes" -ne 1 ] && [ -t 0 ]; then
	cat <<EOF
verify-local will:
  - build ./cmd/web into a temp dir,
  - boot it on 127.0.0.1:$port with a FRESH database,
  - create the account $email / $password,
  - print the URL and the routes your change touched, then wait for you.

Your own data is never touched. Continue? [Y/n]
EOF
	read -r answer
	case "$answer" in [nN]*) echo "verify-local: cancelled"; exit 0 ;; esac
fi

work=$(mktemp -d "${TMPDIR:-/tmp}/gogogo-verify.XXXXXX")
bin="$work/app"
shared_url=""

cleanup() {
	if [ -n "${server_pid:-}" ] && kill -0 "$server_pid" 2>/dev/null; then
		kill "$server_pid" 2>/dev/null || true
		wait "$server_pid" 2>/dev/null || true
	fi
	if [ -n "$shared_url" ]; then
		tailscale serve reset >/dev/null 2>&1 || true
	fi
	rm -rf "$work"
}
trap cleanup EXIT INT TERM

echo "→ building ./cmd/web (this is the binary the operator clicks)…"
go build -o "$bin" ./cmd/web

# Env: the smoke suite's fixture set, minus the parts that would hide what the
# operator is testing. NATS stays at its default (embedded, on) because realtime
# is exactly the behaviour a local click is for; OFFLINE_SYNC and SIGNUP are on
# so the flow runs on a machine whose secrets file closes them.
env_args=(
	"HOST=127.0.0.1"
	"PORT=$port"
	"DATA_DIR=$work/data"
	"DATABASE_PATH=$work/data/app.db"
	"SIGNUP_ENABLED=true"
	"OFFLINE_SYNC_ENABLED=true"
	"GOGOGO_NO_BROWSER=1"
)
if [ -z "${GOAI_API_KEY:-}" ]; then
	env_args+=("SIMULATE_LLM=true")
fi

echo "→ booting on 127.0.0.1:$port with a fresh data dir…"
( env "${env_args[@]}" "$bin" >"$work/server.log" 2>&1 ) &
server_pid=$!

ready=0
for _ in $(seq 1 60); do
	if curl -fsS "http://127.0.0.1:$port/health" >/dev/null 2>&1; then
		ready=1
		break
	fi
	if ! kill -0 "$server_pid" 2>/dev/null; then break; fi
	sleep 0.5
done
if [ "$ready" -ne 1 ]; then
	echo "verify-local: the server did not answer /health; last log lines:" >&2
	tail -20 "$work/server.log" >&2 || true
	exit 1
fi

curl -fsS -X POST "http://127.0.0.1:$port/api/auth/signup/credential" \
	-H 'Content-Type: application/json' \
	-d "{\"email\":\"$email\",\"password\":\"$password\",\"name\":\"Verify Local\"}" \
	>/dev/null 2>&1 || true

# The checklist: a route list beats "test it". Receipts whose FILES the diff
# touched are the recipes that changed, and their declared routes are the pages
# to click — the declaration is already the inventory, so nothing is guessed.
changed=$( { git diff --name-only "${BASE:-HEAD}"; git status --porcelain | awk '{print $2}'; } | sort -u)
touched_ids=()
while IFS= read -r receipt; do
	[ -n "$receipt" ] || continue
	id=$(basename "$receipt" .yml)
	while IFS= read -r path; do
		[ -n "$path" ] || continue
		if grep -q "^  ${path}: " "$receipt"; then
			touched_ids+=("$id")
			break
		fi
	done <<<"$changed"
done < <(ls .recipes/*.yml 2>/dev/null || true)

if [ "$share" -eq 1 ] && command -v tailscale >/dev/null 2>&1; then
	tailscale serve --bg "$port" >/dev/null 2>&1 || true
	shared_url=$(tailscale serve status 2>/dev/null | grep -Eo 'https://[^ ]+' | head -1 || true)
fi

echo
echo "────────────────────────────────────────────────────────────"
echo "  URL       http://127.0.0.1:$port"
if [ -n "$shared_url" ]; then
	echo "  tailnet   $shared_url   (private to your tailnet)"
fi
echo "  account   $email"
echo "  password  $password"
echo "  data      fresh dir, deleted when this exits"
echo "────────────────────────────────────────────────────────────"
if [ "${#touched_ids[@]}" -gt 0 ]; then
	echo "  what to click (recipes your diff touched):"
	for id in "${touched_ids[@]}"; do
		# Declaration fields arrive Go-cased (the type carries yaml tags, and the
		# report's own fields carry json ones), so read both spellings rather
		# than assume: a silent empty list here would read as "nothing to click".
		routes=$(go run ./cmd/gogogo recipe check "$id" --dir "$root" --format json 2>/dev/null |
			node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>{try{const j=JSON.parse(s);const d=(Array.isArray(j)?j[0]:j)?.declaration||{};const r=d.Routes||d.routes||[];const g=r.filter(x=>(x.Method||x.method)==="GET").map(x=>x.Pattern||x.pattern).filter(p=>!p.startsWith("/api/"));console.log([...new Set(g)].join("  "))}catch{console.log("")}})' || true)
		echo "    $id: ${routes:-/  (declares no GET route)}"
	done
else
	echo "  what to click: no recipe declaration changed — open the page you edited:"
	echo "$changed" | grep -E '^(features|web|internal/ui)/' | head -8 | sed 's/^/    /' || true
fi
echo
echo "  Script (60s, not \"test it\"): 1) load each URL  2) do the one action the"
echo "  change added  3) say PASS or FAIL with one sentence of what you saw."
echo "────────────────────────────────────────────────────────────"

if [ -t 0 ]; then
	# The verdict is the point of the exercise, so it is captured here and
	# written as the stamp `make signoff` reads. Leaving it as "press Enter"
	# is what let a look be skipped while the gate stayed green.
	printf 'Verdict — PASS / FAIL / later (Enter = later): '
	read -r verdict || true
	case "${verdict:-}" in
	[Pp]ass | [Pp])
		bash scripts/operator-look.sh pass "operator PASS via verify-local"
		;;
	[Ff]ail | [Ff])
		bash scripts/operator-look.sh fail "operator FAIL via verify-local"
		;;
	*)
		echo "  no verdict recorded — run make operator-look-pass when the operator answers"
		;;
	esac
else
	echo "  (no TTY: stopping automatically after ${ttl_min} min, or Ctrl-C)"
	deadline=$((SECONDS + ttl_min * 60))
	while [ "$SECONDS" -lt "$deadline" ]; do
		kill -0 "$server_pid" 2>/dev/null || break
		sleep 5
	done
fi

echo
echo "→ server stopped, data dir removed."
echo "  Next: the operator's verdict decides — PASS → make ci-local / signoff;"
echo "  FAIL → fix now, while a rejection costs one edit instead of a CI round trip."
