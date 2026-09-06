"use client";

import { useEffect, useRef, useState } from "react";
import { wsUrl } from "./api";

/**
 * Subscribes to a backend WebSocket topic and keeps the most recent `limit`
 * messages (newest first). Reconnects automatically with backoff.
 */
export function useStream<T>(path: string, limit = 100, paused = false) {
  const [items, setItems] = useState<T[]>([]);
  const [connected, setConnected] = useState(false);
  const pausedRef = useRef(paused);
  pausedRef.current = paused;

  useEffect(() => {
    let ws: WebSocket | null = null;
    let closed = false;
    let retry = 1000;
    let timer: ReturnType<typeof setTimeout>;

    const connect = () => {
      ws = new WebSocket(wsUrl(path));
      ws.onopen = () => {
        setConnected(true);
        retry = 1000;
      };
      ws.onmessage = (ev) => {
        if (pausedRef.current) return;
        try {
          const msg = JSON.parse(ev.data) as T;
          setItems((prev) => [msg, ...prev].slice(0, limit));
        } catch {
          /* ignore malformed frames */
        }
      };
      ws.onclose = () => {
        setConnected(false);
        if (!closed) {
          timer = setTimeout(connect, retry);
          retry = Math.min(retry * 2, 15000);
        }
      };
      ws.onerror = () => ws?.close();
    };
    connect();

    return () => {
      closed = true;
      clearTimeout(timer);
      ws?.close();
    };
  }, [path, limit]);

  return { items, connected, clear: () => setItems([]) };
}
