"use client";

import { useEffect, useState } from "react";
import { apiGet } from "@/lib/api";
import type { AppConfig } from "@/lib/types";
import { Badge } from "@/components/StatCard";

export default function DashboardsPage() {
  const [cfg, setCfg] = useState<AppConfig | null>(null);

  useEffect(() => {
    apiGet<AppConfig>("/api/v1/config").then(setCfg).catch(() => setCfg({ cluster: "", mode: "" }));
  }, []);

  const [chrome, setChrome] = useState(false);
  const grafana = cfg?.grafanaUrl;
  const uid = cfg?.grafanaDashboardUid;

  // Land straight on our dashboard rather than Grafana's home page, and hide
  // Grafana's own navigation inside the iframe — the app already has a sidebar,
  // and two nested navs read as a bug. "Show Grafana nav" puts it back for
  // anyone who wants to browse the community dashboards inline.
  const embed = grafana
    ? uid
      ? `${grafana}/d/${uid}?${chrome ? "" : "kiosk&"}refresh=10s`
      : grafana
    : undefined;

  return (
    <div className="space-y-4">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-lg font-semibold">Dashboards</h1>
          <p className="text-sm text-neutral-400">
            Manager-friendly Grafana visibility across Cilium, Hubble, Tetragon,
            and isovalent-control — the golden signals, drops, enforcement, and
            L7 protocols in one place.
          </p>
        </div>
        {grafana && (
          <div className="flex items-center gap-2">
            {uid && (
              <button
                onClick={() => setChrome((v) => !v)}
                className="rounded border border-neutral-700 bg-neutral-900 px-3 py-1.5 text-xs hover:bg-neutral-800"
              >
                {chrome ? "Hide Grafana nav" : "Show Grafana nav"}
              </button>
            )}
            <a href={grafana} target="_blank" rel="noreferrer" className="rounded border border-neutral-700 bg-neutral-900 px-3 py-1.5 text-xs hover:bg-neutral-800">
              Open Grafana ↗
            </a>
          </div>
        )}
      </header>

      {embed ? (
        <div className="panel h-[calc(100vh-11rem)] overflow-hidden">
          <iframe key={embed} src={embed} title="Grafana" className="h-full w-full border-0 bg-white" />
        </div>
      ) : (
        <div className="panel space-y-3 p-6 text-sm text-neutral-300">
          <div><Badge tone="muted">not configured</Badge></div>
          <p className="text-neutral-400">
            Grafana isn&apos;t wired in yet. Install the monitoring stack and point the
            app at it:
          </p>
          <pre className="mono overflow-x-auto rounded bg-[color:var(--surface-0)] p-3 text-xs text-neutral-300">{`# 1. install Prometheus + Grafana + dashboards
./install.sh --with-monitoring

# 2. expose Grafana and tell the app where it is
kubectl -n monitoring port-forward svc/kube-prometheus-stack-grafana 3001:80 &
kubectl -n isovalent-control set env deploy/isovalent-control-backend \\
  IC_GRAFANA_URL=http://localhost:3001`}</pre>
          <p className="text-neutral-400">
            The installer auto-provisions the <span className="mono">Isovalent Control</span> dashboard plus
            the official community dashboards for Cilium, Hubble, and Tetragon.
          </p>
        </div>
      )}
    </div>
  );
}
