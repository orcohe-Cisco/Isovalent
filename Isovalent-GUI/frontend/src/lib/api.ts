const API_BASE = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8081";

/** Where a bearer token is kept for the session. */
let authToken: string | null = null;

export function setAuthToken(token: string | null) {
  authToken = token;
}

export function apiUrl(path: string): string {
  return `${API_BASE}${path}`;
}

export function wsUrl(path: string): string {
  const base = `${API_BASE.replace(/^http/, "ws")}${path}`;
  return authToken ? `${base}?access_token=${encodeURIComponent(authToken)}` : base;
}

function headers(extra?: Record<string, string>): Record<string, string> {
  const h: Record<string, string> = { ...extra };
  if (authToken) h.Authorization = `Bearer ${authToken}`;
  return h;
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const started = performance.now();
  try {
    const res = await fetch(apiUrl(path), {
      method,
      cache: "no-store",
      headers: headers(body === undefined ? undefined : { "Content-Type": "application/json" }),
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    if (!res.ok && res.status !== 204) {
      const msg = await errText(res);
      log.error(`${method} ${path} → ${res.status}`, { error: msg });
      throw new ApiError(msg, res.status);
    }
    log.debug(`${method} ${path} → ${res.status}`, {
      ms: Math.round(performance.now() - started).toString(),
    });
    if (res.status === 204) return undefined as T;
    return (await res.json()) as T;
  } catch (e) {
    if (e instanceof ApiError) throw e;
    // A network-level failure is the one the user most needs explained: the
    // backend is unreachable, not "something went wrong".
    log.error(`${method} ${path} failed before a response`, { error: String(e) });
    throw new ApiError(
      `Cannot reach the API at ${API_BASE}. Check the backend port-forward, then look at Diagnostics.`,
      0,
    );
  }
}

/** ApiError carries the HTTP status so callers can distinguish 403 from 502. */
export class ApiError extends Error {
  status: number;
  constructor(message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

export const apiGet = <T,>(path: string) => request<T>("GET", path);
export const apiPut = <T,>(path: string, body: unknown) => request<T>("PUT", path, body);
export const apiPost = <T,>(path: string, body?: unknown) => request<T>("POST", path, body ?? {});
export const apiDelete = (path: string) => request<void>("DELETE", path);

/** Fetch a text document (the OpenAPI spec) rather than JSON. */
export async function apiText(path: string): Promise<string> {
  const res = await fetch(apiUrl(path), { cache: "no-store", headers: headers() });
  if (!res.ok) throw new ApiError(await errText(res), res.status);
  return res.text();
}

async function errText(res: Response): Promise<string> {
  try {
    const data = await res.json();
    return data.error ?? `HTTP ${res.status}`;
  } catch {
    return `HTTP ${res.status}`;
  }
}

/* ------------------------------------------------------------- logging --- */

type Level = "DEBUG" | "INFO" | "WARN" | "ERROR";

interface ClientLine {
  time: string;
  level: Level;
  message: string;
  fields?: Record<string, string>;
}

/**
 * Browser logging that ends up in the same place as the backend's.
 *
 * Half of every "the UI is broken" report lives in a devtools console nobody
 * has open. Shipping these lines to the backend puts both halves of the story
 * on one page, in one timeline, which is what the Diagnostics view reads.
 *
 * Lines are batched — a failing poll can produce a lot of them, and turning
 * one broken request into two is not an improvement.
 */
class Logger {
  private queue: ClientLine[] = [];
  private timer: ReturnType<typeof setTimeout> | null = null;
  private installed = false;
  /** Kept locally too, so the console works even if the backend is the thing that is down. */
  readonly recent: ClientLine[] = [];

  private push(level: Level, message: string, fields?: Record<string, string>) {
    const line: ClientLine = { time: new Date().toISOString(), level, message, fields };
    this.recent.push(line);
    if (this.recent.length > 500) this.recent.shift();
    // Debug lines stay in the browser; shipping every request would drown
    // the shared log in noise.
    if (level !== "DEBUG") {
      this.queue.push(line);
      this.schedule();
    }
  }

  private schedule() {
    if (this.timer || typeof window === "undefined") return;
    this.timer = setTimeout(() => {
      this.timer = null;
      void this.flush();
    }, 2000);
  }

  async flush() {
    if (!this.queue.length) return;
    const lines = this.queue.splice(0, this.queue.length);
    try {
      await fetch(apiUrl("/api/v1/logs/client"), {
        method: "POST",
        headers: headers({ "Content-Type": "application/json" }),
        body: JSON.stringify({ lines }),
        keepalive: true,
      });
    } catch {
      // Deliberately silent: if the backend is unreachable we already logged
      // that once, and retrying here would loop.
    }
  }

  debug = (m: string, f?: Record<string, string>) => this.push("DEBUG", m, f);
  info = (m: string, f?: Record<string, string>) => this.push("INFO", m, f);
  warn = (m: string, f?: Record<string, string>) => this.push("WARN", m, f);
  error = (m: string, f?: Record<string, string>) => this.push("ERROR", m, f);

  /** Capture uncaught errors and rejections once, at app start. */
  install() {
    if (this.installed || typeof window === "undefined") return;
    this.installed = true;
    window.addEventListener("error", (e) =>
      this.error(e.message, { source: `${e.filename}:${e.lineno}` }),
    );
    window.addEventListener("unhandledrejection", (e) =>
      this.error("unhandled promise rejection", { reason: String(e.reason) }),
    );
    window.addEventListener("beforeunload", () => void this.flush());
  }
}

export const log = new Logger();
