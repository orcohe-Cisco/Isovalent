# Observability bundle (Prometheus + Grafana)

Full-stack visibility for Cilium, Hubble, Tetragon, and isovalent-control's own
golden signals. The `rebuild-aks.sh` script installs and wires all of this
automatically (`WITH_MONITORING=true`, the default); this directory documents
the pieces and lets you apply them to an existing cluster.

## What gets installed

- **kube-prometheus-stack** (Prometheus + Grafana + Alertmanager) in the
  `monitoring` namespace.
- **Metrics enabled** on the data plane during install:
  - Cilium/Hubble: `hubble.metrics.enabled={dns,drop,tcp,flow,icmp,http}` plus
    Cilium + operator Prometheus endpoints and their ServiceMonitors.
  - Tetragon: `tetragon.prometheus.enabled=true` (metrics on `:2112`).
- **One ServiceMonitor** (`isovalent-control-scrape`, created by
  `hack/enable-metrics.sh`) that scrapes every metrics Service carrying the
  label `isovalent-control.io/scrape: "true"` — Cilium, the Cilium operator,
  Hubble, Tetragon, and this platform's own `/metrics`. Selecting our own label
  rather than each chart's scheme keeps it working across chart versions.
- **Grafana dashboard** (`dashboards/isovalent-control.json`) auto-provisioned
  via a ConfigMap labeled `grafana_dashboard: "1"` (the Grafana sidecar loads it).
- **Official community dashboards** for Cilium, Cilium Operator, and Hubble,
  fetched from grafana.com by ID at Grafana startup into a
  "Cilium / Hubble / Tetragon" folder (see `monitoring-values.yaml`).
- **Embedding enabled** (`allow_embedding`, anonymous Viewer) so the app's
  Dashboards tab can iframe Grafana — set `IC_GRAFANA_URL` on the backend.

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
./hack/enable-metrics.sh
kubectl -n monitoring create configmap ic-dashboard \
  --from-file=isovalent-control.json=deploy/observability/dashboards/isovalent-control.json
kubectl -n monitoring label configmap ic-dashboard grafana_dashboard=1
```

## "No data" in every panel

Three independent causes, all handled by `./hack/enable-metrics.sh`:

1. **Cilium ships Hubble metrics off.** If Cilium was installed before this
   platform, or by `cilium install` without the metrics flags, the
   `cilium-agent` and `hubble-metrics` Services do not exist at all. The script
   fixes this with `helm upgrade --reuse-values`, pinned to the chart version
   already installed so the CNI is not upgraded underneath a running cluster.
2. **Nothing tells Prometheus to scrape Cilium/Tetragon.** Fixed by labelling
   whichever metrics Services exist and applying the ServiceMonitor above.
3. **A ServiceMonitor selects Services by label, not pods.** The backend Service
   originally carried no labels, so the scrape config matched nothing while the
   backend served `/metrics` perfectly. The Service is labelled in the manifest
   now.

Verify without changing anything:

```bash
./hack/enable-metrics.sh --verify
```

It port-forwards Prometheus and reports the series count for each of the three
metric families, plus any targets currently down.

## Making "Isovalent Control" the default dashboard

Two independent places decide what you land on:

- **Grafana's home page** — `grafana.ini` sets
  `dashboards.default_home_dashboard_path` to the sidecar-provisioned file, so
  every user (including the anonymous viewer the embed uses) opens straight
  onto it.
- **The app's Dashboards tab** — the backend serves a
  `grafanaDashboardUid` (env `IC_GRAFANA_DASHBOARD_UID`, default
  `isovalent-control`) and the tab embeds `/d/<uid>?kiosk`, which also hides
  Grafana's own sidebar so there aren't two nested navigations. "Show Grafana
  nav" restores it for browsing the community dashboards inline.

Point either at a different dashboard:

```bash
kubectl -n isovalent-control set env deploy/isovalent-control-backend \
  IC_GRAFANA_DASHBOARD_UID=<uid-from-the-dashboard-url>
```
