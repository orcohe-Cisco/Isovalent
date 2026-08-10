"use client";

/**
 * Tiny in-browser log so the GUI can show what it's actually doing —
 * every API call, WebSocket transition, and error is recorded here and
 * surfaced in the status bar / Diagnostics panel.
 */

export type LogLevel = "info" | "warn" | "error";

export interface LogEntry {
  time: string;
  level: LogLevel;
  source: string; // "api" | "ws" | "app"
  message: string;
  detail?: string;
}

const MAX = 300;
let entries: LogEntry[] = [];
const listeners = new Set<(e: LogEntry[]) => void>();

export function log(level: LogLevel, source: string, message: string, detail?: string) {
  entries = [
    { time: new Date().toISOString(), level, source, message, detail },
    ...entries,
  ].slice(0, MAX);
  listeners.forEach((l) => l(entries));
  if (level === "error") console.error(`[${source}] ${message}`, detail ?? "");
}

export function getLog(): LogEntry[] {
  return entries;
}

export function subscribeLog(fn: (e: LogEntry[]) => void): () => void {
  listeners.add(fn);
  fn(entries);
  return () => listeners.delete(fn);
}

export function clearLog() {
  entries = [];
  listeners.forEach((l) => l(entries));
}

/** Connection health, tracked globally so the status bar can show it. */
export interface Health {
  apiOk: boolean | null; // null = not checked yet
  apiUrl: string;
  apiError?: string;
  wsFlows: boolean;
  wsEvents: boolean;
  wsAlerts: boolean;
  mode?: string;
  cluster?: string;
}

let health: Health = { apiOk: null, apiUrl: "", wsFlows: false, wsEvents: false, wsAlerts: false };
const healthListeners = new Set<(h: Health) => void>();

export function setHealth(patch: Partial<Health>) {
  health = { ...health, ...patch };
  healthListeners.forEach((l) => l(health));
}

export function subscribeHealth(fn: (h: Health) => void): () => void {
  healthListeners.add(fn);
  fn(health);
  return () => healthListeners.delete(fn);
}
