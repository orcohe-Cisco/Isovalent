#!/usr/bin/env bash
# enable-metrics.sh — make the Grafana dashboards actually have data to draw.
#
# The dashboards query three metric families:
#
#   isovalent_control_*   from this platform's backend  (/metrics)
#   hubble_*              from cilium-agent             (Hubble metrics)
#   tetragon_events_*     from the tetragon DaemonSet
#
# "No data" everywhere means Prometheus is not scraping them. There are three
# independent reasons that happens, and this script fixes all three, idempotently,
# against a cluster that is already running — no reinstall needed.
#
#   1. Cilium ships with Hubble metrics OFF. If Cilium was installed before this
#      platform (or by `cilium install` without the metrics flags), the
#      cilium-agent and hubble-metrics Services do not even exist. Fixed with a
#      helm upgrade --reuse-values, so nothing else about the install changes.
#   2. Nothing tells Prometheus to scrape Cilium/Hubble/Tetragon. Fixed by
#      labelling whichever metrics Services exist and applying one ServiceMonitor
#      that selects that label — no guessing at each chart's own label scheme,
#      which differs between versions.
#   3. A ServiceMonitor selects SERVICES by label, and our backend Service had no
#      labels, so the existing ServiceMonitor matched nothing. Fixed here by
#      labelling it, and in deploy/manifests/isovalent-control.yaml for new installs.
#
# Usage:  ./hack/enable-metrics.sh            fix + verify
#         ./hack/enable-metrics.sh --verify   verify only, change nothing
set -u

NS="${IC_NAMESPACE:-isovalent-control}"
CILIUM_NS="${CILIUM_NS:-kube-system}"
MON_NS="${MON_NS:-monitoring}"
SCRAPE_LABEL="isovalent-control.io/scrape"
VERIFY_ONLY=false
[ "${1:-}" = "--verify" ] && VERIFY_ONLY=true

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

command -v kubectl >/dev/null 2>&1 || { bad "kubectl not found"; exit 1; }
kubectl cluster-info >/dev/null 2>&1 || { bad "cannot reach the cluster"; exit 1; }

have_crd() { kubectl get crd servicemonitors.monitoring.coreos.com >/dev/null 2>&1; }

# Only x.y.z (optionally with a suffix like -rc.1) counts as a usable version.
is_semver() {
  case "${1:-}" in
    [0-9]*.[0-9]*.[0-9]*) return 0 ;;
    *) return 1 ;;
  esac
}

# ------------------------------------------------------------------ 1. cilium
enable_cilium_metrics() {
  command -v helm >/dev/null 2>&1 || { warn "helm not found — skipping Cilium metrics"; return 0; }
  helm -n "$CILIUM_NS" status cilium >/dev/null 2>&1 || { warn "no Cilium helm release in $CILIUM_NS — skipping"; return 0; }

  if kubectl -n "$CILIUM_NS" get svc hubble-metrics >/dev/null 2>&1 &&
     kubectl -n "$CILIUM_NS" get svc cilium-agent   >/dev/null 2>&1; then
    ok "Cilium + Hubble metrics already exposed"
    return 0
  fi

  c "Enabling Cilium + Hubble metrics (helm upgrade --reuse-values)"
  helm repo add cilium https://helm.cilium.io >/dev/null 2>&1 || true
  helm repo update >/dev/null 2>&1 || true

  # Pin to the version already installed so --reuse-values cannot silently
  # upgrade the CNI underneath a running cluster. Three sources, most reliable
  # first; each result is validated as a real x.y.z before it is trusted,
  # because a half-parsed string like "19" produces a confusing
  # "no chart version found" failure rather than an honest one.
  ver=""
  # 1. The running image tag. Cilium's chart version tracks the release exactly.
  img=$(kubectl -n "$CILIUM_NS" get ds cilium -o jsonpath='{.spec.template.spec.containers[0].image}' 2>/dev/null)
  cand=$(printf '%s' "$img" | sed -n 's/.*:v\{0,1\}\([0-9][0-9]*\.[0-9][0-9]*\.[0-9][^@ ]*\).*/\1/p')
  is_semver "$cand" && ver="$cand"
  # 2. helm get metadata (Helm >= 3.13).
  if [ -z "$ver" ]; then
    cand=$(helm -n "$CILIUM_NS" get metadata cilium -o json 2>/dev/null | tr ',{}' '\n' | sed -n 's/^"version":"\([^"]*\)"$/\1/p' | head -1)
    is_semver "$cand" && ver="$cand"
  fi
  # 3. helm list.
  if [ -z "$ver" ]; then
    cand=$(helm -n "$CILIUM_NS" list --filter '^cilium$' -o json 2>/dev/null | tr ',{}' '\n' | sed -n 's/^"chart":"cilium-\(.*\)"$/\1/p' | head -1)
    is_semver "$cand" && ver="$cand"
  fi

  if [ -n "$ver" ]; then
    dim "  pinning to the installed Cilium version $ver"
  else
    warn "could not determine the installed Cilium version — not touching the CNI."
    dim "  Find it and run the upgrade yourself:"
    dim "    kubectl -n $CILIUM_NS get ds cilium -o jsonpath='{.spec.template.spec.containers[0].image}'"
    dim "    helm -n $CILIUM_NS upgrade cilium cilium/cilium --version <that version> --reuse-values \\"
    dim "      --set prometheus.enabled=true --set operator.prometheus.enabled=true \\"
    dim "      --set hubble.enabled=true --set hubble.metrics.enableOpenMetrics=true \\"
    dim "      --set hubble.metrics.enabled='{dns,drop,tcp,flow,port-distribution,icmp,httpV2}'"
    dim "  Then re-run this script to label the new Services and scrape them."
    return 0
  fi

  # shellcheck disable=SC2086
  helm -n "$CILIUM_NS" upgrade cilium cilium/cilium ${ver:+--version "$ver"} --reuse-values \
    --set prometheus.enabled=true \
    --set operator.prometheus.enabled=true \
    --set hubble.enabled=true \
    --set hubble.metrics.enableOpenMetrics=true \
    --set hubble.metrics.enabled="{dns,drop,tcp,flow,port-distribution,icmp,httpV2}" \
    --wait --timeout 5m \
    && ok "Cilium metrics enabled" \
    || warn "cilium helm upgrade failed — Hubble panels will stay empty"

  kubectl -n "$CILIUM_NS" rollout status ds/cilium --timeout=180s >/dev/null 2>&1 || true
}

enable_tetragon_metrics() {
  command -v helm >/dev/null 2>&1 || return 0
  helm -n "$CILIUM_NS" status tetragon >/dev/null 2>&1 || { warn "no Tetragon helm release — skipping"; return 0; }
  if kubectl -n "$CILIUM_NS" get svc tetragon >/dev/null 2>&1; then
    ok "Tetragon metrics service present"
    return 0
  fi
  c "Enabling Tetragon metrics"
  helm -n "$CILIUM_NS" upgrade tetragon cilium/tetragon --reuse-values \
    --set tetragon.prometheus.enabled=true >/dev/null 2>&1 \
    && ok "Tetragon metrics enabled" || warn "tetragon helm upgrade failed"
}

# ------------------------------------------------------------------ 2 + 3. scrape
label_services() {
  c "Labelling metrics Services for scraping"
  n=0
  for pair in \
    "$CILIUM_NS/cilium-agent" \
    "$CILIUM_NS/cilium-operator" \
    "$CILIUM_NS/hubble-metrics" \
    "$CILIUM_NS/hubble-relay-metrics" \
    "$CILIUM_NS/tetragon" \
    "$CILIUM_NS/tetragon-operator-metrics" \
    "$NS/isovalent-control-backend"
  do
    ns="${pair%%/*}"; svc="${pair##*/}"
    if kubectl -n "$ns" get svc "$svc" >/dev/null 2>&1; then
      kubectl -n "$ns" label svc "$svc" "$SCRAPE_LABEL=true" --overwrite >/dev/null 2>&1 && {
        dim "  labelled $ns/$svc"; n=$((n + 1)); }
    fi
  done
  ok "$n metrics Services labelled"
}

apply_servicemonitor() {
  if ! have_crd; then
    warn "ServiceMonitor CRD missing — install monitoring first (./install.sh --with-monitoring)"
    return 1
  fi
  c "Applying the ServiceMonitor"
  # A ServiceMonitor endpoint references a port by NAME, and the charts do not
  # agree on names (metrics / hubble-metrics / http / prometheus …), so rather
  # than hardcode a guess, read the names off the Services we just labelled.
  names=$(kubectl get svc -A -l "$SCRAPE_LABEL=true" \
    -o jsonpath='{range .items[*]}{range .spec.ports[*]}{.name}{"\n"}{end}{end}' 2>/dev/null \
    | grep -v '^$' | sort -u)
  [ -z "$names" ] && names=$(printf 'metrics\nhubble-metrics\nhttp\n')
  dim "  port names discovered: $(printf '%s' "$names" | tr '\n' ' ')"
  endpoints=""
  for pn in $names; do
    endpoints="$endpoints
    - { port: $pn, path: /metrics, interval: 15s }"
  done

  kubectl apply -f - >/dev/null <<YAML
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: isovalent-control-scrape
  namespace: $MON_NS
  labels:
    release: kube-prometheus-stack
spec:
  namespaceSelector: { any: true }
  selector:
    matchLabels:
      $SCRAPE_LABEL: "true"
  endpoints:$endpoints
YAML
  ok "ServiceMonitor isovalent-control-scrape applied in $MON_NS"
}

# ------------------------------------------------------------------ verify
port_busy() { (exec 3<>"/dev/tcp/127.0.0.1/$1") >/dev/null 2>&1 && exec 3<&- 2>/dev/null; }

verify() {
  c "Verifying Prometheus actually has the metrics"
  kubectl -n "$MON_NS" get svc kube-prometheus-stack-prometheus >/dev/null 2>&1 || {
    warn "Prometheus service not found in $MON_NS — is monitoring installed?"; return 1; }

  p=9464; while port_busy "$p"; do p=$((p + 1)); done
  kubectl -n "$MON_NS" port-forward svc/kube-prometheus-stack-prometheus "$p:9090" >/dev/null 2>&1 &
  pf=$!
  trap 'kill "$pf" 2>/dev/null' RETURN 2>/dev/null || true
  i=0; while [ "$i" -lt 15 ]; do curl -sf -o /dev/null "http://127.0.0.1:$p/-/ready" && break; sleep 1; i=$((i + 1)); done

  has_series() {
    curl -sf --max-time 5 "http://127.0.0.1:$p/api/v1/query?query=$1" 2>/dev/null \
      | tr '{' '\n' | grep -q '"value"'
  }

  # A freshly applied ServiceMonitor is not instant: the operator has to
  # regenerate the config, Prometheus has to reload it, and only then does the
  # first scrape happen. Poll rather than declaring failure after one look.
  c1='count(isovalent_control_flows_total)'
  c2='count(hubble_flows_processed_total)'
  c3='count(tetragon_events_total)'
  i=0
  while [ "$i" -lt 18 ]; do
    a=false; b=false; d=false
    has_series "$c1" && a=true
    has_series "$c2" && b=true
    has_series "$c3" && d=true
    { [ "$a" = true ] && [ "$b" = true ] && [ "$d" = true ]; } && break
    [ "$i" = 0 ] && dim "  waiting for the first scrape (up to 3 min)…"
    sleep 10
    i=$((i + 1))
  done

  rc=0
  [ "$a" = true ] && ok "isovalent_control_*  (this platform)" || { bad "isovalent_control_*  (this platform)"; rc=1; }
  [ "$b" = true ] && ok "hubble_*             (Cilium/Hubble)" || { bad "hubble_*             (Cilium/Hubble)"; rc=1; }
  [ "$d" = true ] && ok "tetragon_events_*    (Tetragon)"      || { bad "tetragon_events_*    (Tetragon)";      rc=1; }

  # Target-level truth: is the scrape even configured, and if so is it failing?
  # "Not configured" and "configured but erroring" need completely different
  # fixes, and a metric count alone cannot tell them apart.
  tgt=$(curl -sf --max-time 8 "http://127.0.0.1:$p/api/v1/targets?state=any" 2>/dev/null)
  if [ -n "$tgt" ]; then
    echo
    dim "Scrape targets from the isovalent-control-scrape ServiceMonitor:"
    printf '%s' "$tgt" | tr '}' '\n' | grep 'isovalent-control-scrape' \
      | sed -n 's/.*"scrapeUrl":"\([^"]*\)".*/  \1/p' | sort -u | head -12 | sed 's/^/  /'
    n=$(printf '%s' "$tgt" | grep -c 'isovalent-control-scrape' || true)
    [ "${n:-0}" -eq 0 ] && warn "Prometheus has NO targets from our ServiceMonitor — it isn't selecting it."
    errs=$(printf '%s' "$tgt" | tr '}' '\n' | grep 'isovalent-control-scrape' \
      | sed -n 's/.*"lastError":"\([^"]\{1,\}\)".*/  error: \1/p' | sort -u | head -5)
    [ -n "$errs" ] && { warn "scrape errors:"; printf '%s\n' "$errs" | sed 's/^/     /'; }
  fi

  kill "$pf" 2>/dev/null
  return $rc
}

# ------------------------------------------------------------------ main
if [ "$VERIFY_ONLY" = false ]; then
  enable_cilium_metrics
  enable_tetragon_metrics
  label_services
  apply_servicemonitor || exit 1
  c "Waiting for the Prometheus operator to reload its config"
  sleep 20
fi

if verify; then
  echo
  ok "All three metric families are being scraped — the dashboards will fill in."
  dim "Panels use rate() over 1m, so give them a minute of traffic before judging."
else
  echo
  warn "Some families are still missing. Most common reasons:"
  dim "  hubble_*   — Cilium needs a real restart to export metrics:"
  dim "               kubectl -n $CILIUM_NS rollout restart ds/cilium"
  dim "  tetragon_* — no Tetragon events yet; generate some:"
  dim "               kubectl run t --rm -it --image=busybox --restart=Never -- sh -c 'cat /etc/shadow'"
  dim "  isovalent_control_* — backend must be in live mode:"
  dim "               kubectl -n $NS get deploy isovalent-control-backend -o jsonpath='{..env}'"
fi
