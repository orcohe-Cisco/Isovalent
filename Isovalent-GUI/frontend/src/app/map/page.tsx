"use client";

import { Embed } from "@/components/Embed";
import { PageHeader } from "@/components/ui/PageHeader";
import { EmptyState } from "@/components/ui/EmptyState";
import { useConfig } from "@/lib/config";
import Link from "next/link";

/**
 * The service map is the real Hubble UI, embedded — not a reimplementation.
 *
 * Drawing our own graph would produce a worse version of a tool people already
 * know, and it would drift the moment upstream changed. The console's job here
 * is to put it one click from the policy that governs what it shows.
 */
export default function MapPage() {
  const { config, loaded } = useConfig();

  return (
    <>
      <PageHeader
        title="Service Map"
        subtitle="Hubble UI, served through this console. Same tool, same behaviour — one origin, no second login."
        actions={
          <Link href="/create" className="btn btn-secondary">
            Write a policy for this
          </Link>
        }
      />
      {loaded && !config.features.hubbleUI ? (
        <EmptyState
          tone="warn"
          title="Hubble UI is not configured"
          body={
            <>
              <p>
                Install it with <code className="mono">cilium hubble enable --ui</code>, then point{" "}
                <code className="mono">IC_HUBBLE_UI_URL</code> at the service (
                <code className="mono">http://hubble-ui.kube-system.svc.cluster.local:80</code>).
              </p>
              <p className="mt-2">
                The bundled <code className="mono">run.sh</code> does both for you.
              </p>
            </>
          }
          action={
            <Link href="/deploy" className="btn btn-primary">
              Deployment commands
            </Link>
          }
        />
      ) : (
        <Embed
          path={config.embeds.hubbleUI}
          title="Hubble UI"
          hint="Your browser reaches Hubble UI only through this console's proxy, so the break is between the backend and the hubble-ui service."
        />
      )}
    </>
  );
}
