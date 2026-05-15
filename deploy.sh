#!/usr/bin/env bash
# Deploy mulan (backend) to the Linux server.
#
# Prereq (one-time):
#   - SSH key auth working: `ssh coffee@192.168.1.47 whoami` returns without prompt.
#   - mulan.service registered as a systemd unit on remote.
#   - Passwordless sudo for systemctl (or run sudo password agent on remote).
#
# Schema migrations are NOT handled here — run `task migrate-prod` separately
# from your dev machine (Atlas needs a tunnel to remote Postgres).
#
# Usage:
#   ./deploy.sh                # build + ship binary + restart service
#   ./deploy.sh --assets       # also re-sync templates/ and elements/
#   ./deploy.sh --env          # also push local .env (CAREFUL: overwrites remote, contains DB password)
#   ./deploy.sh --tail         # follow journal after restart (Ctrl-C to stop)
#   ./deploy.sh --all          # assets + tail (env NOT included on purpose)
#
# Flags combine: `./deploy.sh --assets --tail`.

set -euo pipefail

# ── Config ────────────────────────────────────────────────────────────────
REMOTE="coffee@192.168.1.47"
REMOTE_DIR="mulan"                       # under $HOME on remote
SERVICE="mulan"
LOCAL_DIR="$(cd "$(dirname "$0")" && pwd)"
BUILD_DIR="$LOCAL_DIR/build"
BIN_NAME="mulan-linux-amd64"
HEALTH_URL="http://192.168.1.47:8085/api/menus"

# ── Flag parsing ──────────────────────────────────────────────────────────
SHIP_ASSETS=0
SHIP_ENV=0
TAIL=0
for arg in "$@"; do
    case "$arg" in
        --assets) SHIP_ASSETS=1 ;;
        --env)    SHIP_ENV=1 ;;
        --tail)   TAIL=1 ;;
        --all)    SHIP_ASSETS=1; TAIL=1 ;;   # intentionally omit --env
        -h|--help)
            sed -n '/^# Deploy/,/^# Flags combine/p' "$0" | sed 's/^# \?//'
            exit 0 ;;
        *)
            echo "unknown flag: $arg" >&2
            exit 2 ;;
    esac
done

# ── Colours ───────────────────────────────────────────────────────────────
if [[ -t 1 ]]; then
    C_HDR=$'\033[1;36m'; C_OK=$'\033[1;32m'; C_WARN=$'\033[1;33m'; C_ERR=$'\033[1;31m'; C_OFF=$'\033[0m'
else
    C_HDR=''; C_OK=''; C_WARN=''; C_ERR=''; C_OFF=''
fi
step() { echo "${C_HDR}▶ $*${C_OFF}"; }
ok()   { echo "${C_OK}✓ $*${C_OFF}"; }
warn() { echo "${C_WARN}! $*${C_OFF}"; }
die()  { echo "${C_ERR}✗ $*${C_OFF}" >&2; exit 1; }

# ── 1. SSH reachability ───────────────────────────────────────────────────
step "Probing $REMOTE"
ssh -o BatchMode=yes -o ConnectTimeout=5 "$REMOTE" "hostname" \
    > /dev/null 2>&1 || die "ssh to $REMOTE failed — key auth broken?"
ok "remote reachable"

# ── 2. Cross-compile ──────────────────────────────────────────────────────
step "Building $BIN_NAME (linux/amd64)"
mkdir -p "$BUILD_DIR"
cd "$LOCAL_DIR"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath -ldflags="-s -w" -o "$BUILD_DIR/$BIN_NAME" .
SIZE=$(du -h "$BUILD_DIR/$BIN_NAME" | cut -f1)
ok "built ($SIZE)"

# ── 3. Stop service ───────────────────────────────────────────────────────
# Linux ELF allows overwriting a running binary, but the kernel still
# executes the old image until restart. Stopping first keeps the new
# request flow consistent with the new binary right after upload.
step "Stopping $SERVICE"
ssh "$REMOTE" "sudo systemctl stop $SERVICE" > /dev/null
ok "service stopped"

# ── 4. Ship files ─────────────────────────────────────────────────────────
step "Uploading binary"
scp -q "$BUILD_DIR/$BIN_NAME" "$REMOTE:$REMOTE_DIR/$BIN_NAME"
ssh "$REMOTE" "chmod +x $REMOTE_DIR/$BIN_NAME"
ok "binary uploaded"

if [[ $SHIP_ASSETS -eq 1 ]]; then
    step "Syncing templates/"
    ssh "$REMOTE" "rm -rf ~/$REMOTE_DIR/templates && mkdir -p ~/$REMOTE_DIR/templates"
    scp -qr "$LOCAL_DIR/templates/." "$REMOTE:$REMOTE_DIR/templates/"
    ok "templates synced"

    step "Syncing elements/"
    ssh "$REMOTE" "rm -rf ~/$REMOTE_DIR/elements && mkdir -p ~/$REMOTE_DIR/elements"
    scp -qr "$LOCAL_DIR/elements/." "$REMOTE:$REMOTE_DIR/elements/"
    ok "elements synced"
fi

if [[ $SHIP_ENV -eq 1 ]]; then
    [[ -f "$LOCAL_DIR/.env" ]] || die ".env not found"
    warn "pushing .env — contains DB password"
    scp -q "$LOCAL_DIR/.env" "$REMOTE:$REMOTE_DIR/.env"
    ssh "$REMOTE" "chmod 600 $REMOTE_DIR/.env"
    ok ".env uploaded (chmod 600)"
fi

# ── 5. Start + health check ───────────────────────────────────────────────
step "Starting $SERVICE"
ssh "$REMOTE" "sudo systemctl start $SERVICE" > /dev/null

# Wait for systemd state to flip + port to bind.
sleep 2
STATUS=$(ssh "$REMOTE" "systemctl is-active $SERVICE" 2>/dev/null | tr -d '\r\n ')
if [[ "$STATUS" == "active" ]]; then
    ok "service active"
else
    warn "service status: $STATUS"
fi

if HTTP=$(curl -sS -m 5 -o /dev/null -w '%{http_code}' "$HEALTH_URL" 2>/dev/null) && [[ "$HTTP" == "200" ]]; then
    ok "$HEALTH_URL → 200"
else
    warn "health probe returned: ${HTTP:-no response}"
fi

# Last 5 journal lines — surfaces DB-down, port-busy, panic-on-boot.
echo
echo "${C_HDR}── recent journal ──${C_OFF}"
ssh "$REMOTE" "sudo journalctl -u $SERVICE -n 5 --no-pager" 2>/dev/null

# ── 6. Optional tail ──────────────────────────────────────────────────────
if [[ $TAIL -eq 1 ]]; then
    echo
    step "Tailing journal (Ctrl-C to stop)"
    ssh "$REMOTE" "sudo journalctl -u $SERVICE -f -n 20"
fi
