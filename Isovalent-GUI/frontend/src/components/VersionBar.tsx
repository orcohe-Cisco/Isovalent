"use client";

import { useCallback, useEffect, useState } from "react";
import { apiGet } from "@/lib/api";
import { Chip } from "@/components/ui/Chip";
import { Sheet } from "@/components/ui/Sheet";

export interface ComponentVersion {
  id: string;
  name: string;
  version?: string;
  /** How we learned it — grpc is authoritative, image is only what was requested. */
  source: "build" | "grpc" | "image" | "apiserver" | "unavailable";
  detail?: string;
  healthy: boolean;
}

export interface VersionInfo {
  cluster: string;
  mode: string;
  checkedAt: string;
  components: ComponentVersion[];
}

const SOURCE_LABEL: Record<ComponentVersion["source"], string> = {
  build: "this console's own build",
  grpc: "reported by the agent over gRPC",
  image: "read from the DaemonSet image tag",
  apiserver: "reported by the kube-apiserver",
  unavailable: "could not be determined",
};

export function VersionBar() {
  const [info, setInfo] = useState<VersionInfo | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async (refresh = false) => {
    setBusy(true);
    try {
      setInfo(
        await apiGet<VersionInfo>(
          `/api/v1/versions${refresh ? "?refresh=true" : ""}`,
        ),
      );
      setError(null);
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }, []);

  useEffect(() => {
    load();
    // The agents rarely change version mid-session; a slow poll is plenty.
    const t = setInterval(() => load(), 120000);
    return () => clearInterval(t);
  }, [load]);

  const byId = (id: string) => info?.components.find((c) => c.id === id);
  const cilium = byId("cilium");
  const tetragon = byId("tetragon");
  const self = byId("isovalent-control");

  return (
    <>
      <button
        onClick={() => setOpen(true)}
        className="w-full px-5 py-3 text-left transition-colors duration-200 hover:bg-[color:var(--surface-2)]"
        title="Component versions"
      >
        <div className="flex items-center gap-1.5">
          <VersionPill label="Cilium" c={cilium} />
          <VersionPill label="Tetragon" c={tetragon} />
        </div>
        <div className="mt-1.5 text-[10px] text-[color:var(--text-tertiary)]">
          {error
            ? "versions unavailable"
            : info
              ? `v${self?.version ?? "?"} · ${info.cluster} · ${info.mode} mode`
              : "checking…"}
        </div>
      </button>

      <Sheet
        open={open}
        onClose={() => setOpen(false)}
        title="Cluster & component versions"
        subtitle={
          info
            ? `${info.cluster} · ${info.mode} mode · checked ${new Date(info.checkedAt).toLocaleTimeString()}`
            : undefined
        }
        footer={
          <div className="flex items-center justify-between">
            <span className="text-[11px] text-[color:var(--text-tertiary)]">
              Cached for 60 seconds
            </span>
            <button
              className="btn btn-secondary"
              disabled={busy}
              onClick={() => load(true)}
            >
              {busy ? "Checking…" : "Re-check now"}
            </button>
          </div>
        }
      >
        {error && (
          <div className="rounded-lg px-3 py-2 text-xs" style={{ background: "rgba(230,103,103,0.12)", color: "#f0a3a3" }}>
            {error}
          </div>
        )}
        <ul className="space-y-2">
          {(info?.components ?? []).map((c) => (
            <li key={c.id} className="panel px-4 py-3">
              <div className="flex items-center justify-between gap-3">
                <div className="flex items-center gap-2">
                  <span
                    aria-hidden
                    className="inline-block h-2 w-2 rounded-full"
                    style={{
                      background: c.healthy ? "#199e70" : "#e66767",
                      boxShadow: c.healthy
                        ? "0 0 8px 1px rgba(25,158,112,0.5)"
                        : "0 0 8px 1px rgba(230,103,103,0.5)",
                    }}
                  />
                  <span className="text-[13px] font-medium">{c.name}</span>
                </div>
                <span className="mono text-[13px]">
                  {c.version ?? "unknown"}
                </span>
              </div>
              <div className="mt-1.5 flex items-center gap-2">
                <Chip tone={c.source === "grpc" || c.source === "apiserver" || c.source === "build" ? "ok" : c.source === "unavailable" ? "danger" : "neutral"}>
                  {c.source}
                </Chip>
                <span className="min-w-0 flex-1 truncate text-[11px] text-[color:var(--text-tertiary)]">
                  {SOURCE_LABEL[c.source]}
                </span>
              </div>
              {c.detail && (
                <p className="mono mt-1.5 break-all text-[10px] leading-relaxed text-[color:var(--text-tertiary)]">
                  {c.detail}
                </p>
              )}
            </li>
          ))}
        </ul>
        <p className="mt-4 text-[11px] leading-relaxed text-[color:var(--text-tertiary)]">
          Versions reported over gRPC come from the running agent itself. A
          version read from a DaemonSet image tag tells you what was requested,
          which is not always what is running — if a node failed to pull, the
          image tag will still look correct.
        </p>
      </Sheet>
    </>
  );
}

function VersionPill({ label, c }: { label: string; c?: ComponentVersion }) {
  const healthy = c?.healthy ?? false;
  return (
    <span
      className="inline-flex items-center gap-1 rounded-md border px-1.5 py-0.5 text-[10px] font-medium"
      style={{
        background: "rgba(255,255,255,0.05)",
        borderColor: "var(--hairline)",
        color: healthy ? "var(--text-secondary)" : "var(--text-tertiary)",
      }}
    >
      <span
        aria-hidden
        className="inline-block h-1.5 w-1.5 rounded-full"
        style={{ background: healthy ? "#199e70" : "#6f6e68" }}
      />
      {label}
      <span className="mono">{c?.version ?? "—"}</span>
    </span>
  );
}
