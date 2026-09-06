"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { apiGet } from "@/lib/api";

/**
 * Polls a JSON endpoint, keeping the last good value while a refresh is in
 * flight. Blanking the table on every tick is what makes a live console feel
 * broken even when it is working.
 */
export function usePoll<T>(path: string | null, intervalMs = 10000, initial?: T) {
  const [data, setData] = useState<T | undefined>(initial);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const alive = useRef(true);

  const load = useCallback(async () => {
    if (!path) return;
    setBusy(true);
    try {
      const next = await apiGet<T>(path);
      if (!alive.current) return;
      setData(next);
      setError(null);
    } catch (e) {
      if (!alive.current) return;
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      if (alive.current) {
        setLoading(false);
        setBusy(false);
      }
    }
  }, [path]);

  useEffect(() => {
    alive.current = true;
    void load();
    if (!intervalMs || !path) return () => { alive.current = false; };
    const t = setInterval(load, intervalMs);
    return () => {
      alive.current = false;
      clearInterval(t);
    };
  }, [load, intervalMs, path]);

  return { data, error, loading, busy, reload: load };
}
