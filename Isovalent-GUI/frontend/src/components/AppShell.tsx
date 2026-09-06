"use client";

import { useCallback, useEffect, useState } from "react";
import { apiGet, log } from "@/lib/api";
import { ConfigContext, FALLBACK_CONFIG, type UIConfig } from "@/lib/config";

/**
 * Loads the backend's self-description once and hands it to the whole tree.
 *
 * The menu depends on it: a page whose integration is not configured is hidden
 * rather than offered, because a menu entry that can only ever show "not
 * reachable" trains people to ignore the menu.
 */
export function AppShell({ children }: { children: React.ReactNode }) {
  const [config, setConfig] = useState<UIConfig>(FALLBACK_CONFIG);
  const [error, setError] = useState<string | null>(null);
  const [loaded, setLoaded] = useState(false);

  const load = useCallback(async () => {
    try {
      setConfig(await apiGet<UIConfig>("/api/v1/config"));
      setError(null);
    } catch (e) {
      setError(String(e instanceof Error ? e.message : e));
    } finally {
      setLoaded(true);
    }
  }, []);

  useEffect(() => {
    log.install();
    void load();
    const t = setInterval(load, 60000);
    return () => clearInterval(t);
  }, [load]);

  return (
    <ConfigContext.Provider value={{ config, error, loaded }}>
      {children}
    </ConfigContext.Provider>
  );
}
