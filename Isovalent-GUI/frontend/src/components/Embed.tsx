"use client";

import { useEffect, useRef, useState } from "react";
import { apiUrl } from "@/lib/api";
import { EmptyState } from "@/components/ui/EmptyState";
import Link from "next/link";

/**
 * Frames an upstream console (Hubble UI, Grafana) through the backend proxy.
 *
 * Two things make this work where a naive iframe does not: the proxy strips
 * X-Frame-Options and frame-ancestors, and it serves the upstream from this
 * origin, so there is no CORS story to get wrong. What the proxy cannot do is
 * make an unreachable service reachable, so the failure path points at
 * Diagnostics rather than showing an empty rectangle.
 */
export function Embed({
  path,
  title,
  hint,
}: {
  path: string;
  title: string;
  hint: string;
}) {
  const [state, setState] = useState<"loading" | "ready" | "failed">("loading");
  const ref = useRef<HTMLIFrameElement>(null);

  useEffect(() => {
    // An iframe fires `load` even for the proxy's own error page, so a slow
    // upstream and a dead one look identical for a while. A timeout is the
    // honest way to tell the user we are still waiting.
    const t = setTimeout(() => setState((s) => (s === "loading" ? "failed" : s)), 15000);
    return () => clearTimeout(t);
  }, [path]);

  return (
    <div className="flex h-[calc(100vh-9rem)] flex-col">
      {state === "failed" && (
        <div className="mb-3">
          <EmptyState
            tone="warn"
            title={`${title} did not load`}
            body={
              <>
                <p>{hint}</p>
                <p className="mt-2">
                  The console proxies it at <code className="mono">{path}</code>. Diagnostics
                  probes the same address and reports what it gets back.
                </p>
              </>
            }
            action={
              <div className="flex gap-2">
                <Link href="/diagnostics" className="btn btn-secondary">
                  Open Diagnostics
                </Link>
                <a href={apiUrl(path)} target="_blank" rel="noreferrer" className="btn btn-ghost">
                  Open directly ↗
                </a>
              </div>
            }
          />
        </div>
      )}
      <div className="panel relative flex-1 overflow-hidden">
        {state === "loading" && (
          <div className="absolute inset-0 flex items-center justify-center text-[13px] text-[color:var(--text-tertiary)]">
            Loading {title}…
          </div>
        )}
        <iframe
          ref={ref}
          src={apiUrl(path)}
          title={title}
          className="h-full w-full"
          style={{ border: 0, opacity: state === "ready" ? 1 : 0, transition: "opacity var(--dur) var(--ease-out)" }}
          onLoad={() => setState("ready")}
        />
      </div>
    </div>
  );
}
