"use client";

import { useState } from "react";
import { PolicyEditor } from "@/components/PolicyEditor";

type Family = "cilium" | "tetragon";

const TABS: { id: Family; label: string; blurb: string }[] = [
  {
    id: "cilium",
    label: "Cilium — Network Policies",
    blurb:
      "CiliumNetworkPolicy and CiliumClusterwideNetworkPolicy — L3/L4/L7 network rules. Edit selectors, sources, destinations and ports as a form or as YAML, dry-run against live flows, then apply.",
  },
  {
    id: "tetragon",
    label: "Tetragon — Tracing Policies",
    blurb:
      "TracingPolicy and TracingPolicyNamespaced — eBPF runtime-security rules. Edit here, or use Runtime Policies for the quick Monitor/Kill toggles.",
  },
];

export default function PoliciesPage() {
  const [family, setFamily] = useState<Family>("cilium");
  const active = TABS.find((t) => t.id === family)!;

  return (
    <div className="space-y-4">
      <header>
        <h1 className="text-lg font-semibold">Policy Management</h1>
        <p className="text-sm text-neutral-400">{active.blurb}</p>
      </header>

      <div className="flex gap-1 border-b border-neutral-800">
        {TABS.map((t) => (
          <button
            key={t.id}
            onClick={() => setFamily(t.id)}
            className={`px-4 py-2 text-sm ${
              family === t.id
                ? "border-b-2 border-series-blue text-white"
                : "text-neutral-400 hover:text-neutral-200"
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>

      <PolicyEditor key={family} family={family} />
    </div>
  );
}
