"use client";

import { useEffect, useState } from "react";
import { apiGet } from "@/lib/api";
import type { AppConfig } from "@/lib/types";

export default function NetworkPage() {
  const [cfg, setCfg] = useState<AppConfig | null>(null);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    apiGet<AppConfig>("/api/v1/config")
      .then(setCfg)
      .catch(() => {})
      .finally(() => setLoaded(true));
  }, []);

  const hubble = cfg?.hubbleUiUrl;

  return (
    <div className="space-y-4">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-lg font-semibold">Service Map</h1>
          <p className="text-sm text-neutral-400">
            The official Hubble UI — full L3–L7 service map with Hubble&apos;s own
            filtering, flow inspection, and drill-down.
          </p>
        </div>
        {hubble && (
          <a
            href={hubble}
            target="_blank"
            rel="noreferrer"
            className="rounded border border-neutral-700 bg-neutral-900 px-3 py-1.5 text-xs hover:bg-neutral-800"
          >
            Open in new tab ↗
          </a>
        )}
      </header>

      {hubble ? (
        <div className="panel h-[calc(100vh-10rem)] overflow-hidden">
          <iframe src={hubble} title="Hubble UI" className="h-full w-full border-0 bg-white" />
        </div>
      ) : loaded ? (
        <div className="panel space-y-3 p-6 text-sm text-neutral-300">
          <p className="text-neutral-400">
            The Hubble UI isn&apos;t reachable yet. Enable it and point the app at it:
          </p>
          <pre className="mono overflow-x-auto rounded bg-[color:var(--surface-0)] p-3 text-xs text-neutral-300">{`cilium hubble enable --ui

kubectl -n kube-system port-forward svc/hubble-ui 12000:80 &

kubectl -n isovalent-control set env deploy/isovalent-control-backend \\
  IC_HUBBLE_UI_URL=http://localhost:12000`}</pre>
          <p className="text-neutral-400">
            <code className="mono">./install.sh --install-stack</code> does all of this
            automatically and starts the port-forward for you.
          </p>
        </div>
      ) : (
        <div className="panel p-6 text-sm text-neutral-500">loading…</div>
      )}
    </div>
  );
}
