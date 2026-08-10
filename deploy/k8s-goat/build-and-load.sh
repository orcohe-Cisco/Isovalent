#!/usr/bin/env bash
# Build the two images and make them available to your cluster's nodes.
# Branches on cluster type (kind / minikube / aks). Run from repo root or
# from deploy/k8s-goat — it locates the repo automatically.
set -euo pipefail

TAG="${TAG:-0.1.0}"
BACKEND_IMG="isovalent-control-backend:${TAG}"
FRONTEND_IMG="isovalent-control-frontend:${TAG}"
REPO="$(cd "$(dirname "$0")/../.." && pwd)"
TYPE="${CLUSTER_TYPE:-$(cat /tmp/ic-cluster-type 2>/dev/null || echo unknown)}"

echo "Building images (tag=$TAG) …"
docker build -t "$BACKEND_IMG"  "$REPO/backend"
docker build -t "$FRONTEND_IMG" "$REPO/frontend"

echo "Cluster type: $TYPE"
case "$TYPE" in
  kind)
    KIND_NAME="${KIND_NAME:-kubernetes-goat}"   # override if your kind cluster differs
    kind load docker-image "$BACKEND_IMG"  --name "$KIND_NAME"
    kind load docker-image "$FRONTEND_IMG" --name "$KIND_NAME"
    echo "✓ Loaded into kind cluster '$KIND_NAME'. Manifests use imagePullPolicy: IfNotPresent."
    ;;
  minikube)
    minikube image load "$BACKEND_IMG"
    minikube image load "$FRONTEND_IMG"
    echo "✓ Loaded into minikube."
    ;;
  aks|unknown)
    cat <<EOF

AKS nodes cannot be side-loaded — you need a registry the cluster can pull from.
Cheapest path with Azure Container Registry (ACR):

  ACR=<youracrname>            # without .azurecr.io
  az acr login -n "\$ACR"
  # retag + push
  docker tag  $BACKEND_IMG  \$ACR.azurecr.io/$BACKEND_IMG
  docker tag  $FRONTEND_IMG \$ACR.azurecr.io/$FRONTEND_IMG
  docker push \$ACR.azurecr.io/$BACKEND_IMG
  docker push \$ACR.azurecr.io/$FRONTEND_IMG
  # let the cluster pull without a secret:
  az aks update -n <cluster> -g <resource-group> --attach-acr "\$ACR"

Then set the image refs before applying the manifest:

  export IC_IMAGE_PREFIX=\$ACR.azurecr.io/ IC_IMAGE_TAG=$TAG
  envsubst < deploy/manifests/isovalent-control.yaml | kubectl apply -f -

(No ACR? 'az acr create -n <name> -g <rg> --sku Basic' first — ~free tier.)
EOF
    ;;
esac
