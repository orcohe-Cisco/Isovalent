"use client";

import { createContext, useContext } from "react";

/** What the backend reports about itself and its integrations. */
export interface UIConfig {
  cluster: string;
  mode: string;
  version: string;
  commit: string;
  built: string;
  uptimeSeconds: number;
  features: {
    hubbleUI: boolean;
    grafana: boolean;
    gitops: boolean;
    history: boolean;
    auth: boolean;
    database: boolean;
    ai: { enabled: boolean; provider?: string; model?: string; reason?: string };
  };
  embeds: { hubbleUI: string; grafana: string };
  grafanaDashboardUid: string;
  protectedNamespaces: string[];
  selfNamespace: string;
  policyKinds: string[];
  alertSinks: SinkSpec[];
  windows: string[];
}

export interface SinkSpec {
  type: string;
  label: string;
  urlLabel: string;
  urlHint: string;
  tokenLabel?: string;
  targetLabel?: string;
  targetHint?: string;
  formatOptions?: string[];
  notes: string;
}

export const FALLBACK_CONFIG: UIConfig = {
  cluster: "unknown",
  mode: "live",
  version: "",
  commit: "",
  built: "",
  uptimeSeconds: 0,
  features: {
    hubbleUI: false,
    grafana: false,
    gitops: false,
    history: false,
    auth: false,
    database: false,
    ai: { enabled: false },
  },
  embeds: { hubbleUI: "/hubble-ui/", grafana: "/grafana/" },
  grafanaDashboardUid: "isovalent-control",
  protectedNamespaces: [],
  selfNamespace: "isovalent-control",
  policyKinds: [],
  alertSinks: [],
  windows: ["5m", "15m", "1h", "6h", "1d", "7d", "30d"],
};

export const ConfigContext = createContext<{
  config: UIConfig;
  error: string | null;
  loaded: boolean;
}>({ config: FALLBACK_CONFIG, error: null, loaded: false });

export function useConfig() {
  return useContext(ConfigContext);
}
