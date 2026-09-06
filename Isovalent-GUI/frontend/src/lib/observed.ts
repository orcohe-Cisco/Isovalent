"use client";

import { useCallback, useEffect, useState } from "react";
import { apiGet } from "@/lib/api";

/**
 * The distinct values Hubble and Tetragon have actually seen, used to populate
 * exclusion pickers. Typing a namespace name from memory is how you write an
 * exclusion that silently matches nothing; picking from observed data is not.
 */
export interface ObservedValue {
  value: string;
  count: number;
  lastSeen: string;
  /** Namespace for a workload, workload for a binary, and so on. */
  context?: string;
}

export interface ObservedResponse {
  dimensions: string[];
  values: Record<string, ObservedValue[]>;
  counts: Record<string, number>;
  mode: string;
}

export const EMPTY_OBSERVED: ObservedResponse = {
  dimensions: [],
  values: {},
  counts: {},
  mode: "unknown",
};

export function useObserved(pollMs = 15000) {
  const [data, setData] = useState<ObservedResponse>(EMPTY_OBSERVED);
  const [error, setError] = useState<string | null>(null);
  const [loaded, setLoaded] = useState(false);

  const load = useCallback(async () => {
    try {
      setData(await apiGet<ObservedResponse>("/api/v1/observed?limit=200"));
      setError(null);
    } catch (e) {
      setError(String(e));
    } finally {
      setLoaded(true);
    }
  }, []);

  useEffect(() => {
    load();
    if (!pollMs) return;
    const t = setInterval(load, pollMs);
    return () => clearInterval(t);
  }, [load, pollMs]);

  return { data, error, loaded, reload: load };
}

/** "3 minutes ago" without pulling in a date library. */
export function sinceLabel(iso: string): string {
  const ms = Date.now() - new Date(iso).getTime();
  if (!Number.isFinite(ms) || ms < 0) return "";
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

/**
 * Kubernetes identities that routinely need an exemption. These are not
 * "observed" — admission usernames do not appear in flow or process data —
 * but they are the ones people actually exclude, so they are offered as
 * suggestions alongside service accounts derived from observed namespaces.
 */
export const WELL_KNOWN_ADMISSION_USERS = [
  "system:serviceaccount:kube-system:daemon-set-controller",
  "system:serviceaccount:kube-system:replicaset-controller",
  "system:serviceaccount:kube-system:statefulset-controller",
  "system:serviceaccount:kube-system:job-controller",
  "system:serviceaccount:kube-system:cronjob-controller",
  "system:serviceaccount:kube-system:deployment-controller",
  "system:kube-scheduler",
  "system:kube-controller-manager",
];

/** Service-account principals for every namespace the platform has seen. */
export function derivedServiceAccounts(namespaces: ObservedValue[]): ObservedValue[] {
  return namespaces.map((ns) => ({
    value: `system:serviceaccount:${ns.value}:default`,
    count: ns.count,
    lastSeen: ns.lastSeen,
    context: `derived from namespace ${ns.value}`,
  }));
}
