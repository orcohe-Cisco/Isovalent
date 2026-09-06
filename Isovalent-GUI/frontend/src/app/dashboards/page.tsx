"use client";

import { useState } from "react";
import Link from "next/link";
import { Embed } from "@/components/Embed";
import { PageHeader } from "@/components/ui/PageHeader";
import { EmptyState } from "@/components/ui/EmptyState";
import { useConfig } from "@/lib/config";

/** The dashboards worth pinning, in the order you would look at them. */
const VIEWS = [
  { id: "console", label: "Isovalent Control", path: (uid: string) => `/grafana/d/${uid}` },
  { id: "cilium", label: "Cilium Agent", path: () => "/grafana/d/cilium-agent" },
  { id: "operator", label: "Cilium Operator", path: () => "/grafana/d/cilium-operator" },
  { id: "hubble", label: "Hubble", path: () => "/grafana/d/hubble" },
  { id: "tetragon", label: "Tetragon", path: () => "/grafana/d/tetragon" },
  { id: "browse", label: "Browse all", path: () => "/grafana/dashboards" },
];

export default function DashboardsPage() {
  const { config, loaded } = useConfig();
  const [view, setView] = useState(VIEWS[0].id);
  const active = VIEWS.find((v) => v.id === view) ?? VIEWS[0];

  if (loaded && !config.features.grafana) {
    return (
      <>
        <PageHeader title="Dashboards" />
        <EmptyState
          tone="warn"
          title="Grafana is not configured"
          body={
            <p>
              Run <code className="mono">./run.sh</code> — it installs kube-prometheus-stack and
              loads the shipped dashboards — or point <code className="mono">IC_GRAFANA_URL</code>{" "}
              at an existing Grafana and import{" "}
              <code className="mono">deploy/observability/dashboards/</code>.
            </p>
          }
          action={
            <Link href="/deploy" className="btn btn-primary">
              Deployment commands
            </Link>
          }
        />
      </>
    );
  }

  return (
    <>
      <PageHeader
        title="Dashboards"
        subtitle="Grafana, embedded. The console ships the panel definitions; Grafana owns the rendering and the alerting."
        actions={
          <div className="flex flex-wrap gap-1">
            {VIEWS.map((v) => (
              <button
                key={v.id}
                onClick={() => setView(v.id)}
                className={`btn ${v.id === view ? "btn-secondary" : "btn-ghost"} !px-2.5 !py-1.5 !text-[12px]`}
              >
                {v.label}
              </button>
            ))}
          </div>
        }
      />
      <Embed
        key={active.id}
        path={`${active.path(config.grafanaDashboardUid)}?kiosk&theme=dark`}
        title="Grafana"
        hint="Grafana answered nothing, or that dashboard UID has not been imported into it yet."
      />
    </>
  );
}
