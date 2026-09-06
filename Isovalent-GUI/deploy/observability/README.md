# Observability bundle (Prometheus + Grafana)

Full-stack visibility for Cilium, Hubble, Tetragon, and isovalent-control's own
golden signals. `./run.sh` installs and wires all of this automatically (pass
`--no-grafana` to skip it); this directory documents the pieces and lets you
apply them to a cluster that already has its own Prometheus.

## What gets installed

- **kube-prometheus-stack** (Prometheus + Grafana + Alertmanager) in the
  `monitoring` namespace.
- **Metrics enabled** on the data plane during install:
  - Cilium/Hubble: `hubble.metrics.enabled={dns,drop,tcp,flow,icmp,http}` plus
    Cilium + operator Prometheus endpoints and their ServiceMonitors.
  - Tetragon: `tetragon.prometheus.enabled=true` (metrics on `:2112`).
- **`isovalent-control` ServiceMonitor** (`servicemonitor.yaml`) scraping the
  backend's `/metrics`.
- **Grafana dashboard** (`dashboards/isovalent-control.json`) auto-provisioned
  via a ConfigMap labeled `grafana_dashboard: "1"` (the Grafana sidecar loads it).

## Dashboard panels

Stat row (from isovalent-control's own metrics — always present): flow rate,
policy-drop rate, runtime-event rate, enforcement kills. Time series: traffic
vs drops, alert-router deliveries, Hubble flows by verdict, Hubble drops by
reason, Tetragon events by type, and service-map size. Colors use the
brand-neutral validated dataviz palette (blue/red/aqua/orange), CVD-safe in
both light and dark.

## Access Grafana

```bash
kubectl -n monitoring get secret kube-prometheus-stack-grafana \
  -o jsonpath='{.data.admin-password}' | base64 -d; echo   # admin password
kubectl -n monitoring port-forward svc/kube-prometheus-stack-grafana 3001:80
# open http://localhost:3001  (user: admin) → dashboard "Isovalent Control"
```

## Apply to an existing monitoring stack

```bash
kubectl apply -f deploy/observability/servicemonitor.yaml
kubectl -n monitoring create configmap ic-dashboard \
  --from-file=isovalent-control.json=deploy/observability/dashboards/isovalent-control.json
kubectl -n monitoring label configmap ic-dashboard grafana_dashboard=1
```

If Hubble/Tetragon panels show "No data", their metrics aren't enabled — the
isovalent-control panels work regardless since they scrape our own `/metrics`.
