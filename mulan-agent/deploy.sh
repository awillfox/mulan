#!/usr/bin/env bash
# Deploy mulan-agent to the Windows POS terminal.
#
# Prereq (one-time):
#   - SSH key auth working: `ssh coffee@100.115.144.52 whoami` returns without prompt.
#   - MulanAgent service already registered via NSSM on remote.
#
# Usage:
#   ./deploy.sh                # build + ship binary + restart service
#   ./deploy.sh --templates    # also re-sync templates/ (HTML/CSS changes)
#   ./deploy.sh --env          # also push local build/.env (CAREFUL: overwrites remote)
#   ./deploy.sh --tail         # tail logs after restart (Ctrl-C to stop)
#   ./deploy.sh --all          # templates + env + binary + tail
#
# Flags can be combined: `./deploy.sh --templates --tail`.

set -euo pipefail

# ── Config ────────────────────────────────────────────────────────────────
REMOTE="coffee@100.115.144.52"
REMOTE_DIR="mulan-agent"                 # relative to %USERPROFILE% on Windows
SERVICE="MulanAgent"
LOCAL_DIR="$(cd "$(dirname "$0")" && pwd)"
BUILD_DIR="$LOCAL_DIR/build"

# ── Flag parsing ──────────────────────────────────────────────────────────
SHIP_TEMPLATES=0
SHIP_ENV=0
TAIL=0
for arg in "$@"; do
    case "$arg" in
        --templates) SHIP_TEMPLATES=1 ;;
        --env)       SHIP_ENV=1 ;;
        --tail)      TAIL=1 ;;
        --all)       SHIP_TEMPLATES=1; SHIP_ENV=1; TAIL=1 ;;
        -h|--help)
            sed -n '/^# Deploy/,/^# Flags can be combined/p' "$0" | sed 's/^# \?//'
            exit 0 ;;
        *)
            echo "unknown flag: $arg" >&2
            exit 2 ;;
    esac
done

# ── Colours (off when not a TTY) ──────────────────────────────────────────
if [[ -t 1 ]]; then
    C_HDR=$'\033[1;36m'; C_OK=$'\033[1;32m'; C_WARN=$'\033[1;33m'; C_ERR=$'\033[1;31m'; C_OFF=$'\033[0m'
else
    C_HDR=''; C_OK=''; C_WARN=''; C_ERR=''; C_OFF=''
fi
step() { echo "${C_HDR}▶ $*${C_OFF}"; }
ok()   { echo "${C_OK}✓ $*${C_OFF}"; }
warn() { echo "${C_WARN}! $*${C_OFF}"; }
die()  { echo "${C_ERR}✗ $*${C_OFF}" >&2; exit 1; }

# ── 1. Reachability check (key auth + remote alive) ───────────────────────
step "Probing $REMOTE"
ssh -o BatchMode=yes -o ConnectTimeout=5 "$REMOTE" "hostname" \
    > /dev/null 2>&1 || die "ssh to $REMOTE failed — is key auth still working?"
ok "remote reachable"

# ── 2. Cross-compile ──────────────────────────────────────────────────────
step "Building mulan-agent.exe (windows/amd64)"
mkdir -p "$BUILD_DIR"
cd "$LOCAL_DIR"
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath -ldflags="-s -w" -o "$BUILD_DIR/mulan-agent.exe" .
SIZE=$(du -h "$BUILD_DIR/mulan-agent.exe" | cut -f1)
ok "built ($SIZE)"

# ── 3. Stop service before swapping the binary ────────────────────────────
# NSSM holds an open handle on mulan-agent.exe; scp will fail with "file in
# use" if we skip this step.
step "Stopping $SERVICE"
ssh "$REMOTE" "C:\\Users\\Coffee\\mulan-agent\\nssm.exe stop $SERVICE 2>nul" > /dev/null || true
# Wait for the process to actually exit (NSSM returns before the child
# releases its file handle — scp will fail with "Failure" if we don't).
for i in 1 2 3 4 5 6 7 8 9 10; do
    sleep 1
    if ssh "$REMOTE" "powershell -Command \"if (Get-Process mulan-agent -ErrorAction SilentlyContinue) { exit 1 } else { exit 0 }\"" 2>/dev/null; then
        break
    fi
    if [[ $i -eq 5 ]]; then
        warn "process still running after 5s — sending kill"
        ssh "$REMOTE" "powershell -Command \"Stop-Process -Name mulan-agent -Force -ErrorAction SilentlyContinue\"" > /dev/null || true
    fi
    if [[ $i -eq 10 ]]; then
        die "process still alive after 10s — abort"
    fi
done
ok "service stopped"

# ── 4. Ship files ─────────────────────────────────────────────────────────
step "Uploading binary"
scp -q "$BUILD_DIR/mulan-agent.exe" "$REMOTE:$REMOTE_DIR/mulan-agent.exe"
ok "binary uploaded"

if [[ $SHIP_TEMPLATES -eq 1 ]]; then
    step "Syncing templates/"
    # scp -r overwrites — delete remote first so removed templates don't linger.
    ssh "$REMOTE" "powershell -Command \"Remove-Item -Recurse -Force '$REMOTE_DIR\\templates' -ErrorAction SilentlyContinue; New-Item -ItemType Directory -Force -Path '$REMOTE_DIR\\templates' | Out-Null\"" > /dev/null
    scp -qr "$LOCAL_DIR/templates/." "$REMOTE:$REMOTE_DIR/templates/"
    ok "templates synced"
fi

if [[ $SHIP_ENV -eq 1 ]]; then
    [[ -f "$BUILD_DIR/.env" ]] || die "build/.env not found — create it first"
    step "Pushing build/.env (overwrites remote)"
    scp -q "$BUILD_DIR/.env" "$REMOTE:$REMOTE_DIR/.env"
    ok ".env uploaded"
fi

# ── 5. Start service + verify ─────────────────────────────────────────────
step "Starting $SERVICE"
ssh "$REMOTE" "C:\\Users\\Coffee\\mulan-agent\\nssm.exe start $SERVICE" > /dev/null

# Give it 2s to bind the port, then poll status + health endpoint.
sleep 2
STATUS=$(ssh "$REMOTE" "powershell -Command \"(Get-Service $SERVICE).Status\"" 2>/dev/null | tr -d '\r\n ')
if [[ "$STATUS" == "Running" ]]; then
    ok "service Running"
else
    warn "service status: $STATUS — check logs"
fi

# Hit /pos to confirm port is actually serving.
if HTTP=$(curl -sS -m 5 -o /dev/null -w '%{http_code}' "http://100.115.144.52:8090/pos" 2>/dev/null) && [[ "$HTTP" == "200" ]]; then
    ok "http://100.115.144.52:8090/pos → 200"
else
    warn "/pos probe returned: ${HTTP:-no response}"
fi

# Last 5 lines of stderr — early errors usually surface here (DB unreachable,
# COM port busy, DLL not found, etc).
echo
echo "${C_HDR}── recent stderr ──${C_OFF}"
ssh "$REMOTE" "powershell -Command \"if (Test-Path '$REMOTE_DIR\\logs\\stderr.log') { Get-Content '$REMOTE_DIR\\logs\\stderr.log' -Tail 5 } else { 'no stderr.log yet' }\"" 2>/dev/null

# ── 6. Optional log tail ──────────────────────────────────────────────────
if [[ $TAIL -eq 1 ]]; then
    echo
    step "Tailing stderr.log (Ctrl-C to stop)"
    ssh "$REMOTE" "powershell -Command \"Get-Content '$REMOTE_DIR\\logs\\stderr.log' -Wait -Tail 20\""
fi
