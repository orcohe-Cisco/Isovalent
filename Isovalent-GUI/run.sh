#!/usr/bin/env bash
#
# run.sh — stand the whole thing up, on any Kubernetes.
#
#   ./run.sh                     everything, against the current kube-context
#   ./run.sh --no-goat           skip Kubernetes GOAT
#   ./run.sh --no-grafana        skip kube-prometheus-stack
#   ./run.sh --no-agents         assume Cilium and Tetragon are already there
#   ./run.sh --registry ghcr.io/you
#   ./run.sh --skip-build        deploy an already-built --tag from --registry;
#                                 no local Docker needed
#   ./run.sh --local             spin up (or reuse) a local minikube cluster
#                                 first, then deploy onto it — no cloud needed
#   ./run.sh --forward-only      just (re)establish the port-forwards
#   ./run.sh --uninstall         remove what this script installed
#
# Written for bash 3.2, because that is what macOS ships. No associative
# arrays, no mapfile, no ${var,,}. `make check-scripts` enforces it.
set -euo pipefail

# ---------------------------------------------------------------- options ---
NAMESPACE="${NAMESPACE:-isovalent-control}"
REGISTRY="${REGISTRY:-}"
IMAGE_TAG="${IMAGE_TAG:-}"
WITH_GOAT=1
WITH_GRAFANA=1
WITH_AGENTS=1
SKIP_BUILD=0
LOCAL_MODE=0
MINIKUBE_PROFILE=""
FORWARD_ONLY=0
UNINSTALL=0
ASSUME_YES=0
CILIUM_VERSION="${CILIUM_VERSION:-1.16.5}"
GOAT_DIR="${GOAT_DIR:-$HOME/.cache/kubernetes-goat}"
# GOAT's own manifests are not namespace-parameterized — most scenarios
# hardcode "namespace: default" or omit a namespace entirely (falling back
# to whatever's current). Left alone, that means the console's own
# namespace pickers (Create Policy, Active Policies, History) show GOAT's
# deliberately-vulnerable workloads mixed in with "default" and whatever
# real things live there — confusing at best when writing an actual rule.
# GOAT_NAMESPACE is patched into those manifests before install (see the
# goat step below) so everything lands somewhere dedicated instead.
GOAT_NAMESPACE="${GOAT_NAMESPACE:-kubernetes-goat}"

# Ports. Each is checked before use; the script tells you what is holding one.
PORT_UI="${PORT_UI:-3000}"
PORT_API="${PORT_API:-8081}"       # not configurable in practice: see note below
PORT_HUBBLE_UI="${PORT_HUBBLE_UI:-12000}"
PORT_GRAFANA="${PORT_GRAFANA:-3001}"
PORT_GOAT="${PORT_GOAT:-1234}"

while [ $# -gt 0 ]; do
  case "$1" in
    --no-goat)      WITH_GOAT=0 ;;
    --no-grafana)   WITH_GRAFANA=0 ;;
    --no-agents)    WITH_AGENTS=0 ;;
    --skip-build)   SKIP_BUILD=1 ;;
    --local)        LOCAL_MODE=1 ;;
    --forward-only) FORWARD_ONLY=1 ;;
    --uninstall)    UNINSTALL=1 ;;
    --yes|-y)       ASSUME_YES=1 ;;
    --registry)     REGISTRY="${2:-}"; shift ;;
    --namespace)    NAMESPACE="${2:-}"; shift ;;
    --tag)          IMAGE_TAG="${2:-}"; shift ;;
    -h|--help)      sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown option: $1 (try --help)" >&2; exit 2 ;;
  esac
  shift
done

cd "$(dirname "$0")"
[ -n "$IMAGE_TAG" ] || IMAGE_TAG="$(cat VERSION)"

# ----------------------------------------------------------------- output ---
if [ -t 1 ]; then
  B="$(printf '\033[1m')"; D="$(printf '\033[2m')"; R="$(printf '\033[0m')"
  GREEN="$(printf '\033[32m')"; YELLOW="$(printf '\033[33m')"; RED="$(printf '\033[31m')"
else
  B=""; D=""; R=""; GREEN=""; YELLOW=""; RED=""
fi
step() { printf '\n%s==>%s %s%s\n' "$GREEN" "$R" "$B" "$1$R"; }
info() { printf '    %s\n' "$1"; }
warn() { printf '    %s!%s %s\n' "$YELLOW" "$R" "$1"; }
die()  { printf '\n%serror:%s %s\n' "$RED" "$R" "$1" >&2; exit 1; }

confirm() {
  # $1 = question. Non-interactive or --yes answers yes.
  #
  # The prompt goes to stderr, not stdout: this is called from inside a
  # command substitution in discover_registry, and a prompt on stdout would be
  # captured as part of the answer.
  [ "$ASSUME_YES" = "1" ] && return 0
  [ -t 0 ] || return 0
  printf '    %s [Y/n] ' "$1" >&2
  read -r reply || true
  case "$reply" in n|N|no|NO) return 1 ;; *) return 0 ;; esac
}

ensure_namespace_ready() {
  # $1 = namespace. A previous --uninstall (or a crashed run) can leave the
  # namespace stuck in `Terminating` — usually because a resource inside it
  # has a finalizer that never cleared, which is exactly what happens when
  # nodes were NotReady while kubelet-owned finalizers (e.g. on pods) were
  # supposed to run. `kubectl apply` on the Namespace object itself is a
  # harmless no-op in that state, but every child resource inside it is
  # rejected with "Forbidden: ... because it is being terminated" — which is
  # a confusing way to fail on what looks like a normal deploy.
  local ns="$1" phase i
  phase="$(kubectl get namespace "$ns" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
  [ "$phase" = "Terminating" ] || return 0

  warn "namespace '$ns' is still finishing a previous delete (Terminating); waiting for it"
  i=0
  while [ "$phase" = "Terminating" ] && [ "$i" -lt 18 ]; do
    sleep 10
    phase="$(kubectl get namespace "$ns" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
    i=$((i + 1))
  done
  if [ "$phase" != "Terminating" ]; then
    info "namespace '$ns' finished deleting; continuing"
    return 0
  fi

  warn "namespace '$ns' is still Terminating after 3 minutes"
  info "this usually means a resource inside it has a finalizer that cannot"
  info "clear on its own — often because nodes were NotReady when the delete"
  info "started. The fix is to clear the namespace's own finalizer list so"
  info "Kubernetes can finish removing it; nothing you built is lost, the"
  info "next step recreates it fresh."
  if confirm "Force-clear its finalizers so it can finish deleting?"; then
    kubectl get namespace "$ns" -o json \
      | sed 's/"finalizers"[[:space:]]*:[[:space:]]*\[[^]]*\]/"finalizers":[]/' \
      | kubectl replace --raw "/api/v1/namespaces/$ns/finalize" -f - >/dev/null 2>&1 \
      || warn "could not force-finalize via the API; try by hand: kubectl get ns $ns -o json | sed \"s/\\\"finalizers\\\":.*/\\\"finalizers\\\":[]}}/\" | kubectl replace --raw /api/v1/namespaces/$ns/finalize -f -"
    i=0
    while kubectl get namespace "$ns" >/dev/null 2>&1 && [ "$i" -lt 6 ]; do
      sleep 5
      i=$((i + 1))
    done
  fi
  if kubectl get namespace "$ns" >/dev/null 2>&1; then
    die "namespace '$ns' is still present. Wait for it to clear (kubectl get ns $ns -w) and re-run, or remove it by hand."
  fi
  info "namespace '$ns' cleared"
}

need() {
  command -v "$1" >/dev/null 2>&1 || die "$1 is required but not on PATH. $2"
}

need_docker() {
  need docker "Install Docker, or pass --registry and build the images yourself."
  # `docker` on PATH does not mean the daemon is up — Docker Desktop being
  # installed-but-not-launched is the single most common reason this step
  # fails, and "no such file or directory" on docker.sock is a useless error
  # to hand someone cold.
  if ! docker info >/dev/null 2>&1; then
    echo
    echo "Docker is installed but its daemon is not reachable — this almost"
    echo "always means Docker Desktop (or your Docker engine) is not running."
    echo
    echo "  macOS / Windows   open Docker Desktop and wait for it to say \"Running\""
    echo "  Linux             sudo systemctl start docker"
    echo
    die "docker daemon not reachable"
  fi
}

# ------------------------------------------------------------- preflight ---
step "Preflight"
need kubectl "Install it from https://kubernetes.io/docs/tasks/tools/"
need helm    "Install it from https://helm.sh/docs/intro/install/"

if [ "$LOCAL_MODE" = "1" ]; then
  step "Local cluster (minikube)"
  need minikube "Install it: https://minikube.sigs.k8s.io/docs/start/ (brew install minikube on macOS)"
  need_docker
  MINIKUBE_PROFILE="isovalent-control"
  if minikube status -p "$MINIKUBE_PROFILE" >/dev/null 2>&1; then
    info "minikube profile '$MINIKUBE_PROFILE' is already running"
  else
    info "starting a local minikube cluster (first run can take a few minutes)"
    # --cni=false: Cilium is the CNI here, so the default one must stay off,
    # same reasoning as deploy/local/kind.yaml. Driver is docker because
    # that's the one thing --local assumes you already have — everything
    # else (Cilium/Tetragon/Grafana/GOAT/the console) is unmodified from a
    # normal run.
    if ! minikube start -p "$MINIKUBE_PROFILE" --driver=docker --cni=false \
         --cpus=4 --memory=6g >/tmp/ic-minikube-start.log 2>&1; then
      sed 's/^/      /' /tmp/ic-minikube-start.log | tail -15
      die "minikube start failed — see the output above"
    fi
    info "cluster up"
  fi
  kubectl config use-context "$MINIKUBE_PROFILE" >/dev/null 2>&1 \
    || die "minikube started but its kubectl context ('$MINIKUBE_PROFILE') isn't there — try 'minikube update-context -p $MINIKUBE_PROFILE'"
fi

if ! kubectl config current-context >/dev/null 2>&1; then
  echo
  echo "No kubectl context is selected. This script does not create clusters —"
  echo "point kubectl at the one you want and run it again. For example:"
  echo
  echo "  AKS    az aks get-credentials --resource-group <rg> --name <cluster>"
  echo "  EKS    aws eks update-kubeconfig --region <region> --name <cluster>"
  echo "  GKE    gcloud container clusters get-credentials <cluster> --region <region>"
  echo "  local  kind create cluster --config deploy/local/kind.yaml"
  echo
  die "no kube-context"
fi

CONTEXT="$(kubectl config current-context)"
info "context: $CONTEXT"
kubectl cluster-info >/dev/null 2>&1 || die "cannot reach the cluster for context '$CONTEXT'."

# A cluster with NotReady nodes will fail in ways that look like this script's
# fault (image pulls hang, pods stay Pending, rollouts time out) when the real
# problem is upstream. Catch it here instead of three confusing steps later.
count_not_ready() {
  # Not `grep -c .`: under `set -e`, grep exits 1 when it counts zero matches
  # (i.e. exactly when every node is Ready), which would kill the whole
  # script right at the moment the fix worked. wc -l always exits 0.
  kubectl get nodes --no-headers 2>/dev/null | awk '$2 != "Ready"' | wc -l | tr -d ' '
}

# A NotReady node whose kubelet reports the container runtime itself is down
# ("containerd service is not running") has a crashed containerd on that one
# VM — no amount of node-pool scaling or operation-abort touches that, it
# needs that specific VM reimaged. The providerID Kubernetes already has for
# every node spells out exactly which resource group, VMSS, and instance:
#   azure:///subscriptions/S/resourceGroups/RG/providers/Microsoft.Compute/
#     virtualMachineScaleSets/VMSS/virtualMachines/N
# so this needs nothing from `az aks` at all — just parse it and reimage.
try_fix_aks_containerd() {
  command -v az >/dev/null 2>&1 || return 1
  TARGET_IDS=""
  VMSS_RG=""
  VMSS_NAME=""
  for node in $(kubectl get nodes --no-headers 2>/dev/null | awk '$2 != "Ready" {print $1}'); do
    MSG="$(kubectl get node "$node" -o jsonpath='{range .status.conditions[?(@.type=="Ready")]}{.message}{end}' 2>/dev/null || true)"
    case "$MSG" in
      *"container runtime is down"*|*"container runtime"*down*)
        PID="$(kubectl get node "$node" -o jsonpath='{.spec.providerID}' 2>/dev/null || true)"
        case "$PID" in
          azure://*virtualMachineScaleSets/*/virtualMachines/*)
            rg="$(printf '%s' "$PID" | sed -n 's#.*/resourceGroups/\([^/]*\)/.*#\1#p')"
            vmss="$(printf '%s' "$PID" | sed -n 's#.*/virtualMachineScaleSets/\([^/]*\)/.*#\1#p')"
            id="$(printf '%s' "$PID" | sed -n 's#.*/virtualMachines/\([^/]*\)$#\1#p')"
            [ -n "$rg" ] && [ -n "$vmss" ] && [ -n "$id" ] || continue
            VMSS_RG="$rg"
            VMSS_NAME="$vmss"
            TARGET_IDS="$TARGET_IDS $id"
            ;;
        esac
        ;;
    esac
  done
  [ -n "$TARGET_IDS" ] || return 1

  info "node(s) with a crashed container runtime found — VM instance(s):$TARGET_IDS"
  confirm "Reimage just those VM instance(s) in '$VMSS_NAME'? (reboots them from a clean image; the rest of the pool is untouched)" || return 1
  # shellcheck disable=SC2086
  if az vmss reimage -g "$VMSS_RG" -n "$VMSS_NAME" --instance-ids $TARGET_IDS >/dev/null 2>&1; then
    info "reimage requested"
  else
    warn "vmss reimage failed; do it by hand:"
    echo "        az vmss reimage -g $VMSS_RG -n $VMSS_NAME --instance-ids$TARGET_IDS"
  fi
  return 0
}

# On AKS, "some nodes NotReady" is very often one specific, well-known failure:
# a stuck node-pool operation, or a node that never finished bootstrapping
# (frequently an IMDS timeout) — and the fix is always the same two az calls:
# abort the stuck operation, then cycle the pool so it provisions fresh VMs.
# This is exactly what a human does by hand for this; automate it rather than
# just describing it, but never do it without asking.
try_fix_aks_nodes() {
  command -v az >/dev/null 2>&1 || return 1
  PROVIDER_ID="$(kubectl get nodes -o jsonpath='{.items[0].spec.providerID}' 2>/dev/null || true)"
  case "$PROVIDER_ID" in azure://*) ;; *) return 1 ;; esac

  MC_RG="$(kubectl get nodes -o jsonpath='{.items[0].metadata.labels.kubernetes\.azure\.com/cluster}' 2>/dev/null || true)"
  [ -n "$MC_RG" ] || return 1
  # NB: `[?filter] | [0].[a,b]` projects the *selected object* into a 2-element
  # array and hands -o tsv a bare list — tsv then prints one element per line
  # instead of one tab-separated row, so cut -f2 silently returns the whole
  # blob. Filtering to rows first (`[?filter].[a,b]`) keeps each match as its
  # own row throughout, so tsv actually tab-separates within a row; `head -1`
  # then takes the first match instead of the JMESPath pipe doing it.
  AKS_INFO="$(az aks list --query "[?nodeResourceGroup=='$MC_RG'].[name,resourceGroup]" -o tsv 2>/dev/null | head -1 || true)"
  AKS_NAME="$(printf '%s' "$AKS_INFO" | cut -f1)"
  AKS_RG="$(printf '%s' "$AKS_INFO" | cut -f2)"
  [ -n "$AKS_NAME" ] && [ -n "$AKS_RG" ] || return 1

  info "this looks like AKS cluster '$AKS_NAME' in resource group '$AKS_RG'"
  confirm "Try the usual fix — abort any stuck node pool operation, then cycle the affected pool to force fresh VMs?" || return 1

  POOLS="$(kubectl get nodes -o jsonpath='{range .items[*]}{.metadata.labels.kubernetes\.azure\.com/agentpool}{"\n"}{end}' 2>/dev/null | sort -u)"
  for pool in $POOLS; do
    [ -n "$pool" ] || continue
    STATE="$(az aks nodepool show -g "$AKS_RG" --cluster-name "$AKS_NAME" -n "$pool" --query provisioningState -o tsv 2>/dev/null || true)"

    # Only "Failed" means genuinely stuck. "Scaling"/"Updating"/"Creating"/
    # "Upgrading" are normal in-progress states — very possibly this exact
    # fix, from a run of this script that got interrupted (Ctrl-C/Ctrl-Z) —
    # and operation-abort on a healthy in-progress operation is how you turn
    # that into an actually broken node pool. Only abort a real failure.
    if [ "$STATE" = "Failed" ]; then
      info "node pool '$pool' has a stuck operation ($STATE) — aborting it"
      az aks nodepool operation-abort -g "$AKS_RG" --cluster-name "$AKS_NAME" -n "$pool" >/dev/null 2>&1 || true
      STATE="$(az aks nodepool show -g "$AKS_RG" --cluster-name "$AKS_NAME" -n "$pool" --query provisioningState -o tsv 2>/dev/null || true)"
    fi

    case "$STATE" in
      ''|Succeeded)
        COUNT="$(az aks nodepool show -g "$AKS_RG" --cluster-name "$AKS_NAME" -n "$pool" --query count -o tsv 2>/dev/null || true)"
        case "$COUNT" in
          ''|*[!0-9]*) continue ;;
        esac
        if [ "$COUNT" -gt 1 ]; then
          info "cycling node pool '$pool' ($COUNT nodes) — this can take a few minutes"
          az aks nodepool scale -g "$AKS_RG" --cluster-name "$AKS_NAME" -n "$pool" --node-count $((COUNT - 1)) >/dev/null 2>&1 || true
          az aks nodepool scale -g "$AKS_RG" --cluster-name "$AKS_NAME" -n "$pool" --node-count "$COUNT" >/dev/null 2>&1 || true
        else
          warn "node pool '$pool' has only 1 node; scale it up yourself first to cycle it without downtime"
        fi
        ;;
      *)
        info "node pool '$pool' already has an operation in progress ($STATE) — waiting for it instead of starting another"
        ;;
    esac
  done
  return 0
}

NOT_READY_COUNT="$(count_not_ready)"
if [ "$NOT_READY_COUNT" != "0" ]; then
  warn "$NOT_READY_COUNT node(s) are not Ready:"
  kubectl get nodes --no-headers 2>/dev/null | awk '$2 != "Ready" {print "      " $1 "  " $2}'
  # Try the targeted fix first — it's faster and only touches the specific
  # broken VM(s). Only fall back to the pool-wide cycle if nothing matched
  # that specific failure mode, so a crashed-containerd node doesn't also get
  # its whole pool cycled unnecessarily.
  FIXED=0
  if try_fix_aks_containerd; then
    FIXED=1
  elif try_fix_aks_nodes; then
    FIXED=1
  fi
  if [ "$FIXED" = "1" ]; then
    info "waiting up to 2 minutes for nodes to come back Ready"
    i=0
    while [ "$i" -lt 12 ]; do
      NOT_READY_COUNT="$(count_not_ready)"
      [ "$NOT_READY_COUNT" = "0" ] && break
      sleep 10
      i=$((i + 1))
    done
  fi
  if [ "$NOT_READY_COUNT" != "0" ]; then
    warn "$NOT_READY_COUNT node(s) still not Ready."
    warn "on a managed cluster (AKS/EKS/GKE) a stuck node bootstrap is usually"
    warn "cleared by scaling that node pool down and back up from your cloud"
    warn "console or CLI. On AKS specifically, also check for a stuck node pool"
    warn "operation: az aks nodepool operation-abort."
    confirm "Continue anyway?" || die "aborted — fix node health first"
  else
    info "all nodes Ready"
  fi
fi

CLUSTER_NAME="${CLUSTER_NAME:-$CONTEXT}"

# Platform detection drives only how images get to the nodes. Everything else
# is identical, which is the point.
if [ -n "$MINIKUBE_PROFILE" ]; then
  # We just started this ourselves with a profile name that will never match
  # the `minikube*` context-name guess below (minikube's default profile is
  # literally called "minikube"; a named profile's context is the profile
  # name instead) — so skip the guess and say so directly.
  PLATFORM="minikube"
else
  PLATFORM="generic"
  case "$CONTEXT" in
    kind-*)      PLATFORM="kind" ;;
    k3d-*)       PLATFORM="k3d" ;;
    minikube*)   PLATFORM="minikube" ;;
  esac
  if [ "$PLATFORM" = "generic" ]; then
    NODE_IMAGE="$(kubectl get nodes -o jsonpath='{.items[0].status.nodeInfo.osImage}' 2>/dev/null || true)"
    case "$NODE_IMAGE" in
      *"Docker Desktop"*) PLATFORM="docker-desktop" ;;
    esac
  fi
fi
info "platform: $PLATFORM"

# ------------------------------------------------------------- uninstall ---
if [ "$UNINSTALL" = "1" ]; then
  step "Removing isovalent-control"
  kubectl delete namespace "$NAMESPACE" --ignore-not-found --wait=false
  kubectl delete clusterrolebinding isovalent-control-policies --ignore-not-found
  kubectl delete clusterrole isovalent-control-policies --ignore-not-found
  if [ "$WITH_GRAFANA" = "1" ]; then
    helm uninstall kube-prometheus-stack -n monitoring >/dev/null 2>&1 || true
  fi
  info "Cilium, Tetragon and Kubernetes GOAT were left alone — remove them"
  info "yourself if you want to:"
  info "  helm uninstall cilium tetragon -n kube-system"
  info "  kubectl delete namespace $GOAT_NAMESPACE   # GOAT — one namespace, one command now"
  exit 0
fi

# -------------------------------------------------------------- helpers ----
have_release() {  # $1 release, $2 namespace
  helm status "$1" -n "$2" >/dev/null 2>&1
}

wait_rollout() {  # $1 kind/name, $2 namespace, $3 timeout
  kubectl -n "$2" rollout status "$1" --timeout="${3:-180s}" >/dev/null 2>&1 || {
    warn "$1 in $2 did not become ready in time; continuing"
    kubectl -n "$2" get pods -l "app.kubernetes.io/name=${1#*/}" 2>/dev/null || true
  }
}

svc_exists() {    # $1 service, $2 namespace
  kubectl -n "$2" get svc "$1" >/dev/null 2>&1
}

release_chart_version() {  # $1 release, $2 namespace -> "1.16.5", or empty
  # `helm get metadata` is the direct answer but only exists from Helm 3.13.
  v="$(helm get metadata "$1" -n "$2" 2>/dev/null | awk '/^VERSION:/ {print $2}')"
  if [ -z "$v" ]; then
    # Fall back to the table. CHART is the 9th field because UPDATED spans
    # four. Sanity-check the result rather than trusting the column index.
    v="$(helm list -n "$2" --filter "^$1\$" 2>/dev/null | awk 'NR==2 {print $9}' | sed 's/^.*-//')"
  fi
  case "$v" in
    [0-9]*.[0-9]*) echo "$v" ;;
    *) echo "" ;;
  esac
}

# Helm's --reuse-values reuses the *previous release's* values and does not
# merge in the new chart's defaults. Upgrading an old release with a newer
# chart therefore leaves any newly-introduced value key nil, and templates
# referencing it fail with "nil pointer evaluating interface {}". Helm 3.14
# added --reset-then-reuse-values, which merges properly; on older Helm we pin
# to the installed chart version instead, so there are no new keys to miss.
helm_merge_flag() {
  ver="$(helm version --template='{{.Version}}' 2>/dev/null | sed 's/^v//')"
  major="$(echo "$ver" | cut -d. -f1)"
  minor="$(echo "$ver" | cut -d. -f2)"
  [ -n "$major" ] || { echo "--reuse-values"; return; }
  if [ "$major" -gt 3 ] 2>/dev/null || { [ "$major" = "3" ] && [ "$minor" -ge 14 ] 2>/dev/null; }; then
    echo "--reset-then-reuse-values"
  else
    echo "--reuse-values"
  fi
}

# discover_registry asks, rather than assuming. It never prints or stores cloud
# credentials, and it does not guess: if there is more than one candidate you
# pick, and if there are none you are told what to pass.
discover_registry() {
  candidates=""
  if command -v az >/dev/null 2>&1; then
    candidates="$(az acr list --query '[].loginServer' -o tsv 2>/dev/null || true)"
  fi
  if [ -z "$candidates" ]; then
    if [ -t 0 ] && [ "$ASSUME_YES" != "1" ]; then
      printf '    %sNo registry given, and this cluster is remote.%s\n' "$YELLOW" "$R" >&2
      printf '    Registry to push to (blank to abort): ' >&2
      read -r answer || true
      echo "$answer"
      return 0
    fi
    echo ""
    return 0
  fi

  count="$(echo "$candidates" | grep -c . || true)"
  if [ "$count" = "1" ]; then
    reg="$(echo "$candidates" | sed -n '1p')"
    printf '    Found one container registry on this Azure subscription: %s\n' "$reg" >&2
    if confirm "Push the images there?"; then
      az acr login --name "${reg%%.*}" >/dev/null 2>&1 || \
        printf '    %s!%s az acr login failed; docker push may still work if you are logged in.\n' "$YELLOW" "$R" >&2
      echo "$reg"
      return 0
    fi
    echo ""
    return 0
  fi

  printf '    Registries available on this Azure subscription:\n' >&2
  i=1
  for reg in $candidates; do
    printf '      %d) %s\n' "$i" "$reg" >&2
    i=$((i + 1))
  done
  printf '    Pick a number (blank to abort): ' >&2
  read -r choice || true
  [ -n "$choice" ] || { echo ""; return 0; }
  reg="$(echo "$candidates" | sed -n "${choice}p")"
  [ -n "$reg" ] || { echo ""; return 0; }
  az acr login --name "${reg%%.*}" >/dev/null 2>&1 || true
  echo "$reg"
}

ensure_pull_secret() {  # $1 registry host, $2 namespace
  # A successful `docker push` proves you're authenticated to the registry
  # from this machine, but says nothing about whether the *cluster* can pull
  # from it. On a remote cluster (AKS/EKS/GKE/anything without a managed
  # identity already attached to the registry) that gap is exactly what
  # produces ErrImagePull right after everything else looked fine.
  local reg="$1" ns="$2" token=""

  # ACR specifically: `az acr login` (when authenticated via `az login`,
  # which is the common case) writes an `identitytoken` entry into
  # ~/.docker/config.json, not a plain `auth` (base64 user:pass) field. The
  # Docker CLI knows how to refresh an identitytoken, which is why a local
  # `docker push` works fine with it — but the kubelet's imagePullSecrets
  # mechanism only understands the plain `auth` field and silently ignores
  # identitytoken. Copying config.json into the cluster therefore *creates*
  # a secret without error, but pulls still fail: ImagePullBackOff on every
  # node, indistinguishable at a glance from a missing secret. Sidestep the
  # whole problem for ACR by minting a scoped access token that works as
  # plain basic auth (fixed username, the token as the password) instead of
  # reusing whatever docker happens to have on disk.
  case "$reg" in
    *.azurecr.io)
      if command -v az >/dev/null 2>&1; then
        token="$(az acr login --name "${reg%%.*}" --expose-token --output tsv --query accessToken 2>/dev/null || true)"
      fi
      ;;
  esac

  kubectl -n "$ns" delete secret ic-registry-pull --ignore-not-found >/dev/null 2>&1

  if [ -n "$token" ]; then
    if kubectl -n "$ns" create secret docker-registry ic-registry-pull \
         --docker-server="$reg" \
         --docker-username="00000000-0000-0000-0000-000000000000" \
         --docker-password="$token" \
         --docker-email="isovalent-control@local" >/dev/null 2>&1; then
      attach_pull_secret_to_sas "$ns"
      info "wired up an image pull secret for $reg (ACR access token — good for a few hours)"
      return 0
    fi
    warn "ACR token minted but the secret create failed; falling back to the docker config"
  fi

  # Non-ACR registries (GHCR, ECR via a docker-credential helper, Docker
  # Hub, ...) generally do write a real `auth` field, so reusing docker's
  # own config is fine there.
  DOCKER_CFG="${DOCKER_CONFIG:-$HOME/.docker}/config.json"
  if [ ! -f "$DOCKER_CFG" ]; then
    warn "no docker config at $DOCKER_CFG; skipping the image pull secret"
    warn "if pods show ErrImagePull, create one yourself:"
    echo "        kubectl create secret docker-registry ic-registry-pull -n $ns \\"
    echo "          --docker-server=$reg --docker-username=... --docker-password=..."
    return 0
  fi
  if kubectl -n "$ns" create secret generic ic-registry-pull \
       --type=kubernetes.io/dockerconfigjson \
       --from-file=.dockerconfigjson="$DOCKER_CFG" >/dev/null 2>&1; then
    attach_pull_secret_to_sas "$ns"
    info "wired up an image pull secret for $reg from your local docker login"
    if [ -f "$DOCKER_CFG" ] && grep -q '"identitytoken"' "$DOCKER_CFG" 2>/dev/null; then
      warn "this docker config uses an identitytoken, which the kubelet cannot use to pull;"
      warn "if pods show ImagePullBackOff, that secret will not fix it — see QUICKSTART.md"
    fi
  else
    warn "could not create the image pull secret; if pods show ErrImagePull, create one yourself:"
    echo "        kubectl create secret docker-registry ic-registry-pull -n $ns \\"
    echo "          --docker-server=$reg --docker-username=... --docker-password=..."
  fi
}

attach_pull_secret_to_sas() {  # $1 namespace
  # A patch that silently fails (wrong SA name racing the manifest apply,
  # a transient API error, whatever) used to be swallowed by `|| true` —
  # the secret would exist, look correct, and simply never be attached to
  # anything, producing ImagePullBackOff with no clue why. Verify it stuck,
  # retry once, and say so loudly if it still didn't — better than a script
  # that reports success while the pods stay broken.
  local ns="$1" sa attached tries
  for sa in default isovalent-control-backend; do
    kubectl -n "$ns" get serviceaccount "$sa" >/dev/null 2>&1 || continue
    tries=0
    attached=""
    while [ "$tries" -lt 2 ]; do
      kubectl -n "$ns" patch serviceaccount "$sa" \
        -p '{"imagePullSecrets":[{"name":"ic-registry-pull"}]}' >/dev/null 2>&1
      attached="$(kubectl -n "$ns" get serviceaccount "$sa" -o jsonpath='{.imagePullSecrets[*].name}' 2>/dev/null)"
      case " $attached " in
        *" ic-registry-pull "*) break ;;
      esac
      tries=$((tries + 1))
    done
    case " $attached " in
      *" ic-registry-pull "*) ;;
      *)
        warn "could not attach ic-registry-pull to serviceaccount/$sa in $ns — pods using it will hit ImagePullBackOff"
        echo "        kubectl -n $ns patch serviceaccount $sa -p '{\"imagePullSecrets\":[{\"name\":\"ic-registry-pull\"}]}'"
        ;;
    esac
  done
}

ensure_registry_login() {  # $1 registry host
  # discover_registry() only runs — and only logs you in — when --registry
  # was left blank and it had to go find one. Pass --registry explicitly
  # (the common case once you know your registry) and that login never
  # happens, so a stale or missing ACR token surfaces as a confusing
  # mid-push "authentication required" instead of being caught up front.
  case "$1" in
    *.azurecr.io)
      command -v az >/dev/null 2>&1 || return 0
      az acr login --name "${1%%.*}" >/dev/null 2>&1 \
        || warn "az acr login for $1 failed; the push below may fail too. Run 'az acr login --name ${1%%.*}' yourself first."
      ;;
  esac
}

port_free() {     # $1 port
  if command -v lsof >/dev/null 2>&1; then
    ! lsof -iTCP:"$1" -sTCP:LISTEN >/dev/null 2>&1
  elif command -v ss >/dev/null 2>&1; then
    ! ss -ltn "sport = :$1" 2>/dev/null | grep -q LISTEN
  else
    return 0
  fi
}

# ---------------------------------------------------------------- agents ---
if [ "$FORWARD_ONLY" = "0" ] && [ "$WITH_AGENTS" = "1" ]; then
  step "Cilium + Hubble"
  helm repo add cilium https://helm.cilium.io >/dev/null 2>&1 || true
  helm repo update >/dev/null 2>&1 || true

  HUBBLE_METRICS='{dns,drop,tcp,flow,port-distribution,icmp,httpV2:exemplars=true}'

  if have_release cilium kube-system; then
    # Two independent things can be missing, and this checks both. Hubble
    # Relay/UI not being enabled is the older check. The newer one: even with
    # Hubble fully enabled, its own /metrics endpoint (and the agent's and
    # operator's, neither on by default) were never actually scraped —
    # nothing in this script ever created a ServiceMonitor for any of the
    # three, so Prometheus never saw them, and the Cilium Agent / Cilium
    # Operator / Hubble tabs on Dashboards had nothing to render even though
    # Hubble Flows and the Service Map worked fine (those go through Hubble
    # Relay directly, not Prometheus). See deploy/observability/cilium-metrics-values.yaml.
    CILIUM_VALUES="$(helm get values cilium -n kube-system -o yaml 2>/dev/null)"
    CILIUM_SM_COUNT="$(printf '%s\n' "$CILIUM_VALUES" \
      | awk '/^[[:space:]]*serviceMonitor:/{getline; if ($0 ~ /^[[:space:]]*enabled:[[:space:]]*true/) c++} END{print c+0}')"
    if svc_exists hubble-relay kube-system && svc_exists hubble-ui kube-system && [ "$CILIUM_SM_COUNT" -ge 3 ]; then
      info "already installed, with Hubble Relay/UI and metrics ServiceMonitors — nothing to do"
    else
      if svc_exists hubble-relay kube-system && svc_exists hubble-ui kube-system; then
        warn "Cilium/Hubble are enabled but their Prometheus ServiceMonitors are not —"
        warn "the Cilium Agent, Cilium Operator, and Hubble dashboards will have no data."
      else
        warn "Cilium is installed but Hubble Relay and/or UI are not enabled."
      fi
      if confirm "Fix it? (helm upgrade against the installed chart version)"; then
        INSTALLED="$(release_chart_version cilium kube-system)"
        MERGE="$(helm_merge_flag)"
        if [ -n "$INSTALLED" ]; then
          info "installed chart: cilium-$INSTALLED (pinning the upgrade to it)"
          VERSION_ARG="--version $INSTALLED"
        else
          VERSION_ARG=""
        fi
        # Never fatal: failing to enable Hubble is a reason to carry on with a
        # warning, not to abandon the whole install. Upgrading somebody's CNI
        # is the riskiest thing this script does.
        # shellcheck disable=SC2086
        if helm upgrade cilium cilium/cilium --namespace kube-system $VERSION_ARG "$MERGE" \
             -f deploy/observability/cilium-metrics-values.yaml \
             --set hubble.enabled=true \
             --set hubble.relay.enabled=true \
             --set hubble.ui.enabled=true \
             --set hubble.metrics.enableOpenMetrics=true \
             --set "hubble.metrics.enabled=$HUBBLE_METRICS" \
             >/dev/null 2>/tmp/ic-cilium-upgrade.log; then
          info "Hubble and metrics ServiceMonitors enabled"
        else
          warn "the Cilium upgrade failed. Your CNI is untouched — Helm rolls back on failure."
          sed 's/^/      /' /tmp/ic-cilium-upgrade.log | head -12
          echo
          warn "Enable Hubble by hand, then re-run this script with --no-agents:"
          echo "        cilium hubble enable --ui"
          echo "      or, with Helm:"
          echo "        helm upgrade cilium cilium/cilium -n kube-system --version <installed> \\"
          echo "          --reset-then-reuse-values --set hubble.enabled=true \\"
          echo "          --set hubble.relay.enabled=true --set hubble.ui.enabled=true"
        fi
      else
        warn "skipping — Flows, the Service Map, and/or the metrics dashboards will be empty"
      fi
    fi
  elif kubectl -n kube-system get ds cilium >/dev/null 2>&1; then
    warn "Cilium is present but was not installed by Helm; leaving it alone."
    warn "Enable Hubble yourself: cilium hubble enable --ui"
  else
    if confirm "Cilium is not installed. Install it now (version $CILIUM_VERSION)?"; then
      helm install cilium cilium/cilium --version "$CILIUM_VERSION" --namespace kube-system \
        -f deploy/observability/cilium-metrics-values.yaml \
        --set hubble.enabled=true \
        --set hubble.relay.enabled=true \
        --set hubble.ui.enabled=true \
        --set hubble.metrics.enableOpenMetrics=true \
        --set "hubble.metrics.enabled=$HUBBLE_METRICS" \
        >/dev/null 2>/tmp/ic-cilium-install.log && info "installed" || {
          warn "the Cilium install failed; continuing without it"
          sed 's/^/      /' /tmp/ic-cilium-install.log 2>/dev/null | head -8
        }
    else
      warn "skipping — the console will start but Flows and the Service Map will be empty"
    fi
  fi
  # The CNI itself, not just Hubble Relay on top of it. A fresh install (or
  # a fresh cluster, which is the common case for --local) needs the agent
  # rolled out to every node before pod networking actually works — deploy
  # anything before that finishes and it can sit stuck ContainerCreating (or
  # Running-but-never-Ready) for reasons that have nothing to do with the
  # workload itself. This was previously the single most common reason the
  # console's own pods never became ready on a brand-new cluster.
  if kubectl -n kube-system get ds cilium >/dev/null 2>&1; then
    wait_rollout ds/cilium kube-system 300s
  fi
  if kubectl -n kube-system get ds cilium-envoy >/dev/null 2>&1; then
    wait_rollout ds/cilium-envoy kube-system 300s
  fi
  if svc_exists hubble-relay kube-system; then
    wait_rollout deploy/hubble-relay kube-system 240s
  fi

  step "Tetragon"
  # tetragon.grpc.address is easy to get wrong: some chart versions/values
  # restrict it to localhost inside the pod, which works fine for kubectl
  # exec but leaves every other pod in the cluster — including this console —
  # getting "connection refused". Binding all interfaces here is what makes
  # gRPC reachable from outside the pod at all.
  TETRAGON_GRPC_ADDR="0.0.0.0:54321"
  if have_release tetragon kube-system; then
    info "already installed"
    CURRENT_ADDR="$(helm get values tetragon -n kube-system -o yaml 2>/dev/null \
      | awk '/^[[:space:]]*address:/ {print $2; exit}')"
    case "$CURRENT_ADDR" in
      *127.0.0.1*|*localhost*)
        warn "Tetragon's gRPC listener is bound to localhost — other pods (including"
        warn "this console) cannot reach it. That is the #1 cause of an empty Runtime tab."
        if confirm "Fix it? (helm upgrade against the installed chart version)"; then
          INSTALLED="$(release_chart_version tetragon kube-system)"
          MERGE="$(helm_merge_flag)"
          VERSION_ARG=""
          [ -n "$INSTALLED" ] && VERSION_ARG="--version $INSTALLED"
          # shellcheck disable=SC2086
          if helm upgrade tetragon cilium/tetragon --namespace kube-system $VERSION_ARG "$MERGE" \
               --set "tetragon.grpc.address=$TETRAGON_GRPC_ADDR" \
               >/dev/null 2>/tmp/ic-tetragon-upgrade.log; then
            info "gRPC now listening on $TETRAGON_GRPC_ADDR"
          else
            warn "the Tetragon upgrade failed; leaving it as-is"
            sed 's/^/      /' /tmp/ic-tetragon-upgrade.log | head -12
          fi
        fi
        ;;
    esac
  else
    if confirm "Install Tetragon?"; then
      # enableProcessCred is what makes the user column populated rather than
      # empty, and the Exclusions tab is much less useful without it.
      helm install tetragon cilium/tetragon --namespace kube-system \
        --set tetragon.grpc.enabled=true \
        --set "tetragon.grpc.address=$TETRAGON_GRPC_ADDR" \
        --set tetragon.enableProcessCred=true \
        --set tetragon.enableProcessNs=true \
        --set tetragon.exportAllowList="" \
        >/dev/null 2>/tmp/ic-tetragon-install.log && info "installed" || {
          warn "the Tetragon install failed; Runtime and Exclusions will be empty"
          sed 's/^/      /' /tmp/ic-tetragon-install.log 2>/dev/null | head -8
        }
    else
      warn "skipping — Runtime and Exclusions will be empty"
    fi
  fi
  wait_rollout ds/tetragon kube-system 240s

  # Do not trust the chart's default Service name/shape to stay stable across
  # versions — build the one this console actually depends on (a stable DNS
  # name for the DaemonSet's own gRPC port) directly from the DaemonSet's own
  # pod selector, so a chart bump can't silently break connectivity again.
  TETRAGON_SEL="$(kubectl -n kube-system get ds tetragon -o jsonpath='{.spec.selector.matchLabels}' 2>/dev/null || true)"
  if [ -n "$TETRAGON_SEL" ]; then
    cat <<EOF | kubectl apply -f - >/dev/null 2>&1 \
      || warn "could not create/update the tetragon gRPC service; IC_TETRAGON_ADDR may not resolve"
apiVersion: v1
kind: Service
metadata:
  name: tetragon
  namespace: kube-system
  labels: { app.kubernetes.io/managed-by: isovalent-control-run.sh }
spec:
  selector: $TETRAGON_SEL
  ports:
    - { name: grpc, port: 54321, targetPort: 54321 }
EOF
  else
    warn "tetragon DaemonSet not found; cannot verify its gRPC service exists"
  fi
fi

# --------------------------------------------------------------- grafana ---
if [ "$FORWARD_ONLY" = "0" ] && [ "$WITH_GRAFANA" = "1" ]; then
  step "Prometheus + Grafana"
  if have_release kube-prometheus-stack monitoring; then
    info "already installed"
    # All of Grafana's overrides live in one YAML file, not scattered across
    # --set flags, and that is not a style choice — it fixed a real bug.
    # grafana.ini's own keys are ini section names, and one of them,
    # auth.anonymous, contains a literal dot. --set's path syntax also uses
    # a dot as its separator, so getting that one key right meant escaping
    # it correctly in every flag that touched it — and getting it subtly
    # wrong produces no error at all: the section just silently never makes
    # it into the rendered grafana.ini, and Grafana falls back to requiring
    # a real login with no sign of why. That happened here. A YAML file has
    # no such ambiguity, so read deploy/observability/grafana-values.yaml
    # for what's actually being set and why, rather than duplicating it here.
    #
    # `helm get values` returns the stored value verbatim — if root_url was
    # ever set from Grafana's own "%(protocol)s://%(domain)s/..." template
    # (the very first default), that literal template string is what comes
    # back here, not whatever it resolves to inside Grafana. Compare against
    # the one set of values this script now ever sets, rather than trying to
    # enumerate every way any of them could be wrong.
    CURRENT_VALUES="$(helm get values kube-prometheus-stack -n monitoring -o yaml 2>/dev/null)"
    CURRENT_ROOT_URL="$(printf '%s\n' "$CURRENT_VALUES" | awk '/^[[:space:]]*root_url:/ {print $2; exit}')"
    CURRENT_SUB_PATH="$(printf '%s\n' "$CURRENT_VALUES" | awk '/^[[:space:]]*serve_from_sub_path:/ {print $2; exit}')"
    CURRENT_ANON="$(printf '%s\n' "$CURRENT_VALUES" | awk '/^[[:space:]]*auth\.anonymous:/{f=1;next} f&&/^[[:space:]]*enabled:/{print $2; exit} f&&/^[[:space:]]*[a-zA-Z]+:/&&!/enabled:/{exit}')"
    case "$CURRENT_ROOT_URL,$CURRENT_SUB_PATH,$CURRENT_ANON" in
      *"localhost:8081/grafana/"*,false,true)
        : # already correct
        ;;
      *)
        warn "Grafana's embed settings (root_url / serve_from_sub_path / anonymous"
        warn "access) aren't what this console needs — Dashboards will 404,"
        warn "redirect-loop, ask for a login, or misreport as down."
        if confirm "Fix it? (helm upgrade against the installed chart version)"; then
          INSTALLED="$(release_chart_version kube-prometheus-stack monitoring)"
          MERGE="$(helm_merge_flag)"
          VERSION_ARG=""
          [ -n "$INSTALLED" ] && VERSION_ARG="--version $INSTALLED"
          # shellcheck disable=SC2086
          if helm upgrade kube-prometheus-stack prometheus-community/kube-prometheus-stack \
               --namespace monitoring $VERSION_ARG "$MERGE" \
               -f deploy/observability/grafana-values.yaml \
               >/dev/null 2>/tmp/ic-grafana-upgrade.log; then
            info "Grafana's embed settings fixed (root_url, sub-path, anonymous access)"
            kubectl -n monitoring rollout restart deploy -l app.kubernetes.io/name=grafana >/dev/null 2>&1 || true
          else
            warn "the Grafana upgrade failed; leaving it as-is"
            sed 's/^/      /' /tmp/ic-grafana-upgrade.log | head -12
          fi
        fi
        ;;
    esac
  else
    if confirm "Install kube-prometheus-stack into namespace 'monitoring'?"; then
      helm repo add prometheus-community https://prometheus-community.github.io/helm-charts >/dev/null 2>&1 || true
      helm repo update >/dev/null 2>&1 || true
      # See deploy/observability/grafana-values.yaml for what's set and why:
      # anonymous read-only access (no second login — a lab default; put an
      # auth proxy in front of Grafana before doing this anywhere real), a
      # root_url that matches where the browser actually reaches the
      # embedded Grafana (through this console's backend, not the frontend),
      # and serve_from_sub_path matching how this proxy actually forwards.
      helm install kube-prometheus-stack prometheus-community/kube-prometheus-stack \
        --namespace monitoring --create-namespace \
        -f deploy/observability/grafana-values.yaml \
        >/dev/null
    else
      WITH_GRAFANA=0
    fi
  fi

  if [ "$WITH_GRAFANA" = "1" ]; then
    info "loading the shipped dashboards"
    for f in deploy/observability/dashboards/*.json; do
      [ -f "$f" ] || continue
      name="$(basename "$f" .json)"
      kubectl -n monitoring create configmap "ic-dashboard-$name" \
        --from-file="$(basename "$f")=$f" --dry-run=client -o yaml \
        | kubectl label -f - --local -o yaml grafana_dashboard=1 \
        | kubectl apply -f - >/dev/null
    done
    wait_rollout deploy/kube-prometheus-stack-grafana monitoring 300s
  fi
fi

# ------------------------------------------------------------------ goat ---
if [ "$FORWARD_ONLY" = "0" ] && [ "$WITH_GOAT" = "1" ]; then
  step "Kubernetes GOAT"
  if kubectl get ns "$GOAT_NAMESPACE" >/dev/null 2>&1; then
    info "already installed"
    # A cluster that ran an older version of this script may still have
    # GOAT's "default"-namespace scenarios sitting there from before this
    # isolation existed. Flag it rather than silently leaving duplicates or
    # trying to migrate live workloads automatically.
    STRAY="$(kubectl get deploy -n default \
      -l 'app in (build-code,internal-proxy,kubernetes-goat-home,poor-registry,system-monitor,health-check)' \
      -o name 2>/dev/null)"
    if [ -n "$STRAY" ]; then
      warn "GOAT workloads also exist in 'default' (from before namespace isolation)."
      warn "Remove them once you've confirmed nothing else needs them:"
      echo "        kubectl delete deploy,svc -n default -l 'app in (build-code,internal-proxy,kubernetes-goat-home,poor-registry,system-monitor,health-check)'"
    fi
  elif confirm "Install Kubernetes GOAT (deliberately vulnerable workloads)?"; then
    if ! command -v git >/dev/null 2>&1; then
      warn "git is not on PATH; skipping GOAT"
      WITH_GOAT=0
    else
      if [ ! -d "$GOAT_DIR/.git" ]; then
        mkdir -p "$(dirname "$GOAT_DIR")"
        git clone --depth 1 https://github.com/madhuakula/kubernetes-goat.git "$GOAT_DIR" >/dev/null 2>&1 \
          || die "could not clone Kubernetes GOAT. Clone it yourself and set GOAT_DIR."
      fi

      # Isolate GOAT into its own namespace instead of "default". Two of its
      # scenarios (hunger-check, cache-store) already ship their own
      # dedicated namespace and are left alone; insecure-rbac deliberately
      # targets kube-system — that IS the scenario, also left alone. Every
      # other scenario either hardcodes "namespace: default" (which
      # `kubectl apply -n ...` cannot override — a mismatch between an
      # object's own namespace and -n is a hard error, not a merge) or omits
      # a namespace entirely (in which case kubectl falls back to whatever
      # is current). Patch both cases directly, once, before handing off to
      # GOAT's own install script — this only touches the cached clone, and
      # only re-runs when GOAT_NAMESPACE doesn't already exist.
      kubectl create namespace "$GOAT_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f - >/dev/null

      for f in build-code internal-proxy kubernetes-goat-home poor-registry system-monitor; do
        ff="$GOAT_DIR/scenarios/$f/deployment.yaml"
        [ -f "$ff" ] || continue
        sed "s/namespace: default/namespace: $GOAT_NAMESPACE/g" "$ff" > "$ff.ic-tmp" && mv "$ff.ic-tmp" "$ff"
      done
      for ff in "$GOAT_DIR/scenarios/batch-check/job.yaml" \
                "$GOAT_DIR/scenarios/health-check/deployment.yaml" \
                "$GOAT_DIR/scenarios/hidden-in-layers/deployment.yaml"; do
        [ -f "$ff" ] || continue
        # These ship with no namespace field at all — insert one right after
        # every top-level (column-0) `metadata:`, not the indented ones
        # nested under pod templates further down.
        awk -v ns="$GOAT_NAMESPACE" '{print} /^metadata:$/{print "  namespace: " ns}' "$ff" > "$ff.ic-tmp" && mv "$ff.ic-tmp" "$ff"
      done

      warn "GOAT deploys intentionally vulnerable workloads. Never on a shared cluster."
      ( cd "$GOAT_DIR" && bash setup-kubernetes-goat.sh ) || warn "GOAT setup reported errors; continuing"
    fi
  else
    WITH_GOAT=0
  fi
fi

# ---------------------------------------------------------------- images ---
if [ "$FORWARD_ONLY" = "0" ]; then
  BACKEND_IMAGE="isovalent-control-backend:$IMAGE_TAG"
  FRONTEND_IMAGE="isovalent-control-frontend:$IMAGE_TAG"
  PREFIX=""
  if [ -n "$REGISTRY" ]; then
    PREFIX="${REGISTRY%/}/"
    BACKEND_IMAGE="$PREFIX$BACKEND_IMAGE"
    FRONTEND_IMAGE="$PREFIX$FRONTEND_IMAGE"
  fi

  if [ "$SKIP_BUILD" = "1" ]; then
    # Docker is only needed to *produce* the images. If they were already
    # built and pushed by hand or by CI for this tag, there is nothing here
    # for Docker to do — skip it entirely rather than demanding Docker
    # Desktop be running just to run `kubectl apply`.
    step "Using existing images ($IMAGE_TAG)"
    case "$PLATFORM" in
      kind|k3d|minikube|docker-desktop)
        warn "--skip-build on $PLATFORM assumes the images are already there"
        warn "(kind load / k3d image import / minikube image load, or — for"
        warn "Docker Desktop — already present in the shared daemon)."
        ;;
      *)
        [ -n "$REGISTRY" ] || die "--skip-build on a remote cluster needs --registry, so the manifest knows where to pull from."
        ensure_registry_login "$REGISTRY"
        info "assuming $BACKEND_IMAGE and $FRONTEND_IMAGE already exist in $REGISTRY"
        ;;
    esac
  else
    step "Building images ($IMAGE_TAG)"
    need_docker
    COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
    BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

    docker build --quiet \
      --build-arg "VERSION=$IMAGE_TAG" --build-arg "COMMIT=$COMMIT" --build-arg "BUILD_DATE=$BUILD_DATE" \
      -t "$BACKEND_IMAGE" backend/ >/dev/null
    docker build --quiet -t "$FRONTEND_IMAGE" frontend/ >/dev/null
    info "built $BACKEND_IMAGE"
    info "built $FRONTEND_IMAGE"

    step "Getting the images to the nodes"
    case "$PLATFORM" in
      kind)
        CLUSTER="${CONTEXT#kind-}"
        kind load docker-image "$BACKEND_IMAGE" "$FRONTEND_IMAGE" --name "$CLUSTER" >/dev/null
        info "loaded into the kind cluster"
        ;;
      k3d)
        CLUSTER="${CONTEXT#k3d-}"
        k3d image import "$BACKEND_IMAGE" "$FRONTEND_IMAGE" -c "$CLUSTER" >/dev/null
        info "imported into the k3d cluster"
        ;;
      minikube)
        MINIKUBE_FLAG=""
        [ -n "$MINIKUBE_PROFILE" ] && MINIKUBE_FLAG="-p $MINIKUBE_PROFILE"
        # shellcheck disable=SC2086
        minikube image load "$BACKEND_IMAGE" $MINIKUBE_FLAG >/dev/null
        # shellcheck disable=SC2086
        minikube image load "$FRONTEND_IMAGE" $MINIKUBE_FLAG >/dev/null
        info "loaded into minikube"
        ;;
      docker-desktop)
        info "Docker Desktop shares the daemon with the cluster; nothing to push"
        ;;
      *)
        if [ -z "$REGISTRY" ]; then
          REGISTRY="$(discover_registry)"
          if [ -n "$REGISTRY" ]; then
            PREFIX="${REGISTRY%/}/"
            docker tag "$BACKEND_IMAGE" "$PREFIX$BACKEND_IMAGE"
            docker tag "$FRONTEND_IMAGE" "$PREFIX$FRONTEND_IMAGE"
            BACKEND_IMAGE="$PREFIX$BACKEND_IMAGE"
            FRONTEND_IMAGE="$PREFIX$FRONTEND_IMAGE"
          fi
        fi
        if [ -z "$REGISTRY" ]; then
          echo
          echo "This is a remote cluster, so its nodes cannot see images built on"
          echo "this machine. Re-run with a registry they can pull from:"
          echo
          echo "    ./run.sh --registry ghcr.io/your-org"
          echo "    ./run.sh --registry <name>.azurecr.io"
          echo "    ./run.sh --registry <account>.dkr.ecr.<region>.amazonaws.com"
          echo
          echo "Log in to it first (docker login / az acr login / aws ecr get-login-password)."
          die "no registry for a remote cluster"
        fi
        ensure_registry_login "$REGISTRY"
        if ! docker push "$BACKEND_IMAGE" >/dev/null 2>/tmp/ic-push.log; then
          sed 's/^/      /' /tmp/ic-push.log | head -5
          die "docker push failed. Log in to $REGISTRY first, then re-run."
        fi
        docker push "$FRONTEND_IMAGE" >/dev/null
        info "pushed to $REGISTRY"
        ;;
    esac
  fi

  # ------------------------------------------------------------- deploy ---
  step "Deploying the console"
  ensure_namespace_ready "$NAMESPACE"
  HUBBLE_RELAY="hubble-relay.kube-system.svc.cluster.local:80"
  TETRAGON_ADDR="tetragon.kube-system.svc.cluster.local:54321"
  HUBBLE_UI_URL="http://hubble-ui.kube-system.svc.cluster.local:80"
  GRAFANA_URL="http://kube-prometheus-stack-grafana.monitoring.svc.cluster.local:80"
  svc_exists hubble-relay kube-system || warn "hubble-relay service not found; Flows will be empty"
  svc_exists hubble-ui kube-system    || warn "hubble-ui service not found; the Service Map will be empty"

  # Always is correct for a real registry (a reused tag must not silently
  # serve a stale cached layer) but wrong for a locally-loaded image: on
  # kind/k3d/minikube/Docker Desktop nothing was ever pushed anywhere, so
  # Always makes the kubelet ignore the image that was just loaded and try
  # a real registry pull instead — ImagePullBackOff on every node, every
  # time, no matter how correctly everything else went.
  case "$PLATFORM" in
    kind|k3d|minikube|docker-desktop) IC_IMAGE_PULL_POLICY="Never" ;;
    *)                                IC_IMAGE_PULL_POLICY="Always" ;;
  esac

  export IC_IMAGE_PREFIX="$PREFIX" IMAGE_TAG NAMESPACE CLUSTER_NAME IC_IMAGE_PULL_POLICY
  export HUBBLE_RELAY TETRAGON_ADDR HUBBLE_UI_URL GRAFANA_URL

  if command -v envsubst >/dev/null 2>&1; then
    envsubst < deploy/kubernetes/isovalent-control.yaml | kubectl apply -f - >/dev/null
  else
    # envsubst lives in gettext, which macOS does not ship. Rather than make
    # that a hard dependency, fall back to sed for the handful of variables
    # this manifest actually uses.
    sed -e "s|\${IC_IMAGE_PREFIX}|$PREFIX|g" \
        -e "s|\${IMAGE_TAG}|$IMAGE_TAG|g" \
        -e "s|\${NAMESPACE}|$NAMESPACE|g" \
        -e "s|\${CLUSTER_NAME}|$CLUSTER_NAME|g" \
        -e "s|\${HUBBLE_RELAY}|$HUBBLE_RELAY|g" \
        -e "s|\${TETRAGON_ADDR}|$TETRAGON_ADDR|g" \
        -e "s|\${HUBBLE_UI_URL}|$HUBBLE_UI_URL|g" \
        -e "s|\${GRAFANA_URL}|$GRAFANA_URL|g" \
        -e "s|\${IC_IMAGE_PULL_POLICY}|$IC_IMAGE_PULL_POLICY|g" \
        deploy/kubernetes/isovalent-control.yaml | kubectl apply -f - >/dev/null
  fi

  if [ -n "$REGISTRY" ]; then
    ensure_pull_secret "$REGISTRY" "$NAMESPACE"
  fi

  # Reused tags are the single most common "the new build is broken" report;
  # a restart defeats the node's image cache.
  kubectl -n "$NAMESPACE" rollout restart deploy/isovalent-control-backend deploy/isovalent-control-frontend >/dev/null 2>&1 || true
  wait_rollout deploy/isovalent-control-backend "$NAMESPACE" 240s
  wait_rollout deploy/isovalent-control-frontend "$NAMESPACE" 240s
fi

# --------------------------------------------------------- port-forwards ---
step "Port-forwards"

# The frontend's JavaScript runs in your browser and calls http://localhost:8081,
# baked in at build time. So the backend has to be on 8081 specifically — this
# one is not a preference.
PF_STATE_DIR="${PF_STATE_DIR:-$HOME/.cache/isovalent-control}"
PF_PIDFILE="$PF_STATE_DIR/portforward.pids"
mkdir -p "$PF_STATE_DIR" 2>/dev/null || true

# A closed terminal that didn't run the trap (killed window, machine sleep,
# `kill -9`) leaves the old port-forwards holding the ports forever — every
# re-run then reports "already in use" and does nothing, which is exactly the
# babysitting this script is supposed to remove. Reap our own previous
# forwards before deciding a port is unavailable.
reap_stale_forwards() {
  [ -f "$PF_PIDFILE" ] || return 0
  while read -r pid; do
    [ -n "$pid" ] || continue
    if kill -0 "$pid" 2>/dev/null; then
      case "$(ps -o command= -p "$pid" 2>/dev/null || true)" in
        *kubectl*port-forward*) kill "$pid" >/dev/null 2>&1 || true ;;
      esac
    fi
  done < "$PF_PIDFILE"
  rm -f "$PF_PIDFILE"
  sleep 1
}
reap_stale_forwards

PF_PIDS=""
forward() {  # $1 namespace, $2 svc, $3 local, $4 remote, $5 label
  if ! svc_exists "$2" "$1"; then
    warn "$5: service $2 not found in $1 — skipped"
    return 0
  fi
  if ! port_free "$3"; then
    warn "$5: port $3 is already in use by something else — skipped"
    warn "  find it with: lsof -iTCP:$3 -sTCP:LISTEN"
    return 0
  fi
  kubectl -n "$1" port-forward "svc/$2" "$3:$4" >/dev/null 2>&1 &
  pid="$!"
  PF_PIDS="$PF_PIDS $pid"
  echo "$pid" >> "$PF_PIDFILE"
  info "$5 → http://localhost:$3"
}

cleanup() {
  for pid in $PF_PIDS; do
    kill "$pid" >/dev/null 2>&1 || true
  done
  rm -f "$PF_PIDFILE"
}
trap cleanup EXIT INT TERM

forward "$NAMESPACE" isovalent-control-backend  "$PORT_API"       8081 "Console API"
forward "$NAMESPACE" isovalent-control-frontend "$PORT_UI"        3000 "Console"
forward kube-system  hubble-ui                  "$PORT_HUBBLE_UI" 80   "Hubble UI (direct)"
if [ "$WITH_GRAFANA" = "1" ]; then
  forward monitoring kube-prometheus-stack-grafana "$PORT_GRAFANA" 80 "Grafana (direct)"
fi
if [ "$WITH_GOAT" = "1" ] && kubectl get ns "$GOAT_NAMESPACE" >/dev/null 2>&1; then
  # The Service GOAT's own manifest actually creates is named
  # "health-check-service", not "health-check" — this was pointed at the
  # wrong name before, so the port-forward silently never established
  # (forward() warns and skips rather than failing loudly) and GOAT looked
  # empty regardless of anything about its namespace.
  forward "$GOAT_NAMESPACE" health-check-service "$PORT_GOAT" 80 "Kubernetes GOAT"
fi

sleep 3

# ----------------------------------------------------------------- ready ---
step "Ready"
HEALTH="$(curl -fsS "http://localhost:$PORT_API/healthz" 2>/dev/null || true)"
if [ -n "$HEALTH" ]; then
  info "backend: $HEALTH"
else
  warn "the backend did not answer /healthz yet — give it a few seconds, then reload"
fi

echo
printf '    %sConsole%s        http://localhost:%s\n' "$B" "$R" "$PORT_UI"
printf '    %sAPI%s            http://localhost:%s/healthz\n' "$D" "$R" "$PORT_API"
printf '    %sAPI docs%s       http://localhost:%s/api/openapi.yaml\n' "$D" "$R" "$PORT_API"
[ "$WITH_GOAT" = "1" ] && printf '    %sGOAT%s           http://localhost:%s\n' "$D" "$R" "$PORT_GOAT"
echo
echo "    The Service Map and Dashboards are embedded in the console itself —"
echo "    you do not need the direct Hubble UI or Grafana links, they are there"
echo "    for when you want to check the proxy is the problem and not the app."
echo
echo "    Leave this terminal open; the port-forwards die with it."
echo "    Re-establish them later with: ./run.sh --forward-only"
echo

# Wait on the forwards so Ctrl-C cleans them up.
wait
