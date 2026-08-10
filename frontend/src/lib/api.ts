import { log, setHealth } from "./log";

/**
 * API base.
 *
 * Empty string = same origin: the browser calls whatever host:port served the
 * page, and the frontend container's proxy (see frontend/proxy.js) forwards
 * /api and /ws to the backend Service inside the cluster. That is what makes
 * `port-forward svc/isovalent-control-frontend <anyport>:3000` sufficient —
 * there is no build-time port for the user to get wrong.
 *
 * In `npm run dev` there is no proxy in front, so fall back to the local
 * backend. Setting NEXT_PUBLIC_API_URL still overrides both (for an ingress
 * deployment that terminates the API on a different host).
 */
const API_BASE =
  process.env.NEXT_PUBLIC_API_URL ??
  (process.env.NODE_ENV === "production" ? "" : "http://localhost:8081");

export function apiUrl(path: string): string {
  return `${API_BASE}${path}`;
}

export function wsUrl(path: string): string {
  if (API_BASE) return `${API_BASE.replace(/^http/, "ws")}${path}`;
  if (typeof window === "undefined") return path;
  const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${proto}//${window.location.host}${path}`;
}

/** Human-readable base, for the status bar and logs. */
export function apiBase(): string {
  if (API_BASE) return API_BASE;
  if (typeof window === "undefined") return "same origin";
  return `${window.location.origin} (same origin)`;
}

/** Wraps fetch so every call is logged and connection health is tracked. */
async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const started = Date.now();
  try {
    const res = await fetch(apiUrl(path), {
      method,
      cache: "no-store",
      headers: body === undefined ? undefined : { "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const ms = Date.now() - started;
    if (!res.ok) {
      const msg = await errText(res);
      log("error", "api", `${method} ${path} → ${res.status} (${ms}ms)`, msg);
      if (res.status >= 500) setHealth({ apiOk: false, apiError: msg });
      throw new Error(msg);
    }
    log("info", "api", `${method} ${path} → ${res.status} (${ms}ms)`);
    setHealth({ apiOk: true, apiUrl: apiBase(), apiError: undefined });
    if (res.status === 204) return undefined as T;
    return (await res.json()) as T;
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e);
    // A network-level failure (backend down / port-forward dead) lands here.
    if (msg.includes("fetch") || msg.includes("NetworkError") || msg.includes("Load failed")) {
      setHealth({
        apiOk: false,
        apiUrl: apiBase(),
        apiError: `Cannot reach the API at ${apiBase()}. The app's own port-forward may be down, or the frontend pod cannot reach the backend Service.`,
      });
      log("error", "api", `${method} ${path} — connection failed`, `${apiBase()} unreachable`);
    }
    throw e;
  }
}

export const apiGet = <T,>(path: string) => request<T>("GET", path);
export const apiPut = <T,>(path: string, body: unknown) => request<T>("PUT", path, body);
export const apiPost = <T,>(path: string, body: unknown) => request<T>("POST", path, body);
export const apiDelete = (path: string) => request<void>("DELETE", path);

async function errText(res: Response): Promise<string> {
  try {
    const data = await res.json();
    return data.error ?? `HTTP ${res.status}`;
  } catch {
    return `HTTP ${res.status}`;
  }
}
