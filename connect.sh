#!/usr/bin/env bash
# connect.sh — open every port-forward the platform needs, from any shell.
#
#   ./connect.sh            start (or restart) all forwards and verify them
#   ./connect.sh status     show what is currently reachable
#   ./connect.sh stop       stop everything this script started
#
# Ports are chosen automatically: it prefers the defaults below and steps to the
# next free port if one is taken. The app itself works on ANY port — the
# frontend container proxies /api and /ws to the backend Service, so nothing is
# baked into the browser bundle.
#
# Override a preference with env vars:
#   IC_FE_PORT=3000  IC_BE_PORT=8081  IC_HUBBLE_PORT=12000  IC_GRAFANA_PORT=3001
set -u

NS="${IC_NAMESPACE:-isovalent-control}"
FE_PREF="${IC_FE_PORT:-3000}"
BE_PREF="${IC_BE_PORT:-8081}"
HUBBLE_PREF="${IC_HUBBLE_PORT:-12000}"
GRAFANA_PREF="${IC_GRAFANA_PORT:-3001}"
PIDFILE="${TMPDIR:-/tmp}/isovalent-control.pids"
PORTFILE="${TMPDIR:-/tmp}/isovalent-control.ports"
LOGDIR="${TMPDIR:-/tmp}/isovalent-control-logs"
OPEN_BROWSER="${IC_OPEN_BROWSER:-true}"

if [ -t 1 ]; then
  B=$'\033[1m'; G=$'\033[32m'; Y=$'\033[33m'; R=$'\033[31m'; D=$'\033[2m'; N=$'\033[0m'
else
  B=; G=; Y=; R=; D=; N=
fi
c()    { printf '%s==>%s %s\n' "$B" "$N" "$*"; }
ok()   { printf '%s  ok%s  %s\n' "$G" "$N" "$*"; }
warn() { printf '%s  !!%s  %s\n' "$Y" "$N" "$*"; }
bad()  { printf '%s  xx%s  %s\n' "$R" "$N" "$*"; }
dim()  { printf '%s%s%s\n' "$D" "$*" "$N"; }

command -v kubectl >/dev/null 2>&1 || { bad "kubectl not found in PATH"; exit 1; }

# --------------------------------------------------------------- ports
# Dependency-free listener check (no lsof/nc/ss needed).
port_busy() { (exec 3<>"/dev/tcp/127.0.0.1/$1") >/dev/null 2>&1 && exec 3<&- 2>/dev/null; }

# Who is holding a port? Empty if we can't tell (lsof missing).
port_holder_pids() { command -v lsof >/dev/null 2>&1 && lsof -t -nP -iTCP:"$1" -sTCP:LISTEN 2>/dev/null; }
pid_cmd() { ps -o command= -p "$1" 2>/dev/null; }

# Reclaim a port held by a stray port-forward of OURS — typically one that was
# Ctrl-Z'd. A suspended process ignores SIGTERM until it is resumed, which is why
# this sends SIGCONT first and SIGKILL second. Anything that is not one of our
# own forwards is left strictly alone.
reclaim_port() {
  reclaimed=false
  for pid in $(port_holder_pids "$1"); do
    cmd=$(pid_cmd "$pid")
    case "$cmd" in
      *"port-forward"*"isovalent-control-"*|*"port-forward"*"hubble-ui"*|*"port-forward"*"kube-prometheus-stack-grafana"*)
        kill -CONT "$pid" 2>/dev/null
        kill -9 "$pid" 2>/dev/null
        reclaimed=true
        ;;
    esac
  done
  [ "$reclaimed" = true ] && sleep 1
  port_busy "$1" && return 1 || return 0
}

pick_port() {
  p="$1"; limit=$((p + 40))
  while [ "$p" -lt "$limit" ]; do
    if ! port_busy "$p"; then echo "$p"; return 0; fi
    if [ "$p" = "$1" ] && reclaim_port "$p"; then echo "$p"; return 0; fi
    p=$((p + 1))
  done
  echo "$1"   # nothing free nearby; caller will report the failure honestly
}

# Explain a port we had to move off, so it isn't a silent mystery.
explain_busy() {
  pids=$(port_holder_pids "$1")
  [ -z "$pids" ] && return 0
  for pid in $pids; do
    dim "        port $1 is held by pid $pid: $(pid_cmd "$pid" | cut -c1-90)"
  done
}

svc_exists() { kubectl -n "$1" get svc "$2" >/dev/null 2>&1; }

probe() {
  case "$1" in
    frontend) curl -sf -o /dev/null --max-time 3 "http://localhost:$FE_PORT" ;;
    api)      curl -sf -o /dev/null --max-time 3 "http://localhost:$FE_PORT/healthz" ;;
    backend)  curl -sf -o /dev/null --max-time 3 "http://localhost:$BE_PORT/healthz" ;;
    hubble)   curl -sf -o /dev/null --max-time 3 "http://localhost:$HUBBLE_PORT" ;;
    grafana)  curl -sf -o /dev/null --max-time 3 "http://localhost:$GRAFANA_PORT/api/health" ;;
  esac
}

load_ports() {
  FE_PORT="$FE_PREF"; BE_PORT="$BE_PREF"
  HUBBLE_PORT="$HUBBLE_PREF"; GRAFANA_PORT="$GRAFANA_PREF"
  # shellcheck disable=SC1090
  [ -f "$PORTFILE" ] && . "$PORTFILE"
  return 0
}

stop_all() {
  if [ -f "$PIDFILE" ]; then
    while read -r pid; do [ -n "$pid" ] && kill "$pid" >/dev/null 2>&1; done < "$PIDFILE"
    rm -f "$PIDFILE"
  fi
  pkill -f "port-forward svc/isovalent-control-" >/dev/null 2>&1
  pkill -f "port-forward svc/hubble-ui" >/dev/null 2>&1
  pkill -f "port-forward svc/kube-prometheus-stack-grafana" >/dev/null 2>&1
  return 0
}

# Fully detached supervisor: reconnects forever, survives Ctrl-Z, closing the
# tab, and the shell exiting. Nothing to accidentally suspend.
start_forward() {
  ns="$1"; svc="$2"; ports="$3"; label="$4"
  mkdir -p "$LOGDIR"
  nohup bash -c "
    while true; do
      kubectl -n '$ns' port-forward 'svc/$svc' '$ports' >>'$LOGDIR/$label.log' 2>&1
      echo '--- $label forward dropped, reconnecting' >>'$LOGDIR/$label.log'
      sleep 2
    done
  " </dev/null >/dev/null 2>&1 &
  echo "$!" >> "$PIDFILE"
  disown 2>/dev/null || true
}

report() {
  printf '\n'
  if probe frontend; then
    ok "app       ${B}http://localhost:$FE_PORT${N}   <- open this"
    if probe api; then
      ok "api       proxied through the app on the same port (no separate forward needed)"
    else
      bad "api       the app is up but /healthz did not answer"
      dim "          the frontend pod cannot reach the backend Service:"
      dim "          kubectl -n $NS get pods"
    fi
  else
    bad "app       http://localhost:$FE_PORT   NOT reachable"
    dim "          log: $LOGDIR/frontend.log"
  fi
  probe backend && dim "backend   http://localhost:$BE_PORT   (direct, for curl/debug only)"
  if svc_exists kube-system hubble-ui; then
    probe hubble && ok "hubble    http://localhost:$HUBBLE_PORT   (embedded in Service Map)" \
                 || warn "hubble    http://localhost:$HUBBLE_PORT   not answering yet"
  fi
  if svc_exists monitoring kube-prometheus-stack-grafana; then
    probe grafana && ok "grafana   http://localhost:$GRAFANA_PORT   (embedded in Dashboards)" \
                  || warn "grafana   http://localhost:$GRAFANA_PORT   not answering yet"
  fi
  printf '\n'
}

# If the embeds could not get their default ports, the backend has to be told,
# because it is what hands those URLs to the browser.
sync_embed_urls() {
  want_hubble="http://localhost:$HUBBLE_PORT"
  want_grafana="http://localhost:$GRAFANA_PORT"
  have=$(kubectl -n "$NS" get deploy isovalent-control-backend \
    -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="IC_HUBBLE_UI_URL")].value} {.spec.template.spec.containers[0].env[?(@.name=="IC_GRAFANA_URL")].value}' 2>/dev/null)
  if [ "$have" != "$want_hubble $want_grafana" ]; then
    c "Embed ports changed — updating the backend so the UI points at them"
    kubectl -n "$NS" set env deploy/isovalent-control-backend \
      IC_HUBBLE_UI_URL="$want_hubble" IC_GRAFANA_URL="$want_grafana" >/dev/null 2>&1
    kubectl -n "$NS" rollout status deploy/isovalent-control-backend --timeout=120s >/dev/null 2>&1
    ok "backend now serves $want_hubble and $want_grafana"
  fi
}

# --------------------------------------------------------------- commands
case "${1:-start}" in
  stop)
    c "Stopping port-forwards"; stop_all; rm -f "$PORTFILE"; ok "stopped"; exit 0 ;;
  status)
    load_ports; c "Port-forward status"; report; dim "logs: $LOGDIR/*.log"; exit 0 ;;
  start|restart|"") ;;
  *) echo "usage: $0 [start|status|stop]" >&2; exit 2 ;;
esac

# --------------------------------------------------------------- start
c "Checking cluster"
if ! kubectl cluster-info >/dev/null 2>&1; then
  bad "cannot reach the Kubernetes API — check your kubeconfig / VPN."
  dim "  kubectl config current-context"
  exit 1
fi
ok "API reachable — context $(kubectl config current-context 2>/dev/null)"

kubectl get ns "$NS" >/dev/null 2>&1 || { bad "namespace $NS not found — run ./install.sh first."; exit 1; }

c "Checking pods in $NS"
not_ready=$(kubectl -n "$NS" get pods --no-headers 2>/dev/null | awk '{split($2,a,"/"); if ($3 != "Running" || a[1] != a[2]) print}')
if [ -n "$not_ready" ]; then
  warn "not ready yet — waiting up to 90s:"
  printf '%s\n' "$not_ready" | sed 's/^/       /'
  kubectl -n "$NS" wait --for=condition=ready pod --all --timeout=90s >/dev/null 2>&1 || true
fi
kubectl -n "$NS" get pods --no-headers 2>/dev/null | sed 's/^/       /'

c "Clearing old forwards"
stop_all
sleep 1
: > "$PIDFILE"

c "Choosing ports"
FE_PORT=$(pick_port "$FE_PREF")
BE_PORT=$(pick_port "$BE_PREF")
HUBBLE_PORT=$(pick_port "$HUBBLE_PREF")
GRAFANA_PORT=$(pick_port "$GRAFANA_PREF")
if [ "$FE_PORT" != "$FE_PREF" ]; then
  warn "port $FE_PREF is taken — using $FE_PORT instead (the app works on any port)"
  explain_busy "$FE_PREF"
fi
[ "$BE_PORT" = "$BE_PREF" ] || dim "  backend debug port $BE_PREF taken — using $BE_PORT"
cat > "$PORTFILE" <<EOF
FE_PORT=$FE_PORT
BE_PORT=$BE_PORT
HUBBLE_PORT=$HUBBLE_PORT
GRAFANA_PORT=$GRAFANA_PORT
EOF

c "Starting port-forwards"
start_forward "$NS" isovalent-control-frontend "$FE_PORT:3000" frontend
start_forward "$NS" isovalent-control-backend  "$BE_PORT:8081" backend
svc_exists kube-system hubble-ui && \
  start_forward kube-system hubble-ui "$HUBBLE_PORT:80" hubble
svc_exists monitoring kube-prometheus-stack-grafana && \
  start_forward monitoring kube-prometheus-stack-grafana "$GRAFANA_PORT:80" grafana

i=0
while [ "$i" -lt 20 ]; do
  if probe frontend && probe api; then break; fi
  sleep 2
  i=$((i + 1))
done

if svc_exists kube-system hubble-ui || svc_exists monitoring kube-prometheus-stack-grafana; then
  sync_embed_urls
fi

report

if probe api; then
  cfg=$(curl -sf --max-time 3 "http://localhost:$FE_PORT/api/v1/config" 2>/dev/null)
  case "$cfg" in
    *retentionDays*) dim "backend config: $cfg" ;;
    *) warn "/api/v1/config returned nothing — the backend image is older than this checkout."
       dim "  rebuild and redeploy:  ./install.sh --acr <your-acr>   (or ./install.sh)" ;;
  esac
fi

dim "forwards are detached — Ctrl-Z, closing this tab, or exiting the shell will not kill them."
dim "  ./connect.sh status    ./connect.sh stop    logs: $LOGDIR/*.log"

if [ "$OPEN_BROWSER" = true ] && probe frontend; then
  ( command -v open >/dev/null && open "http://localhost:$FE_PORT" ) >/dev/null 2>&1 || \
  ( command -v xdg-open >/dev/null && xdg-open "http://localhost:$FE_PORT" ) >/dev/null 2>&1 || true
fi
