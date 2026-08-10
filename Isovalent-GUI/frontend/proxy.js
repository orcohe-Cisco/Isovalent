/**
 * Single-origin entrypoint for the frontend container.
 *
 * Why this exists
 * ---------------
 * Next.js inlines NEXT_PUBLIC_* into the browser bundle at BUILD time. Baking
 * `http://localhost:8081` in there meant the browser always called that exact
 * port, so the app only worked if you happened to port-forward the backend to
 * 8081 — forward it to 8085 and you got a perfectly rendered UI with zero data
 * and no obvious cause.
 *
 * Now the browser only ever talks to the origin it was served from. This
 * process listens on $PORT, runs the Next standalone server privately on
 * 127.0.0.1:$NEXT_PORT, and reverse-proxies:
 *
 *   /api/*    -> backend (HTTP)
 *   /ws/*     -> backend (WebSocket upgrade)
 *   /healthz  -> backend
 *   /_ic/*    -> handled here (container probes, proxy self-diagnostics)
 *   everything else -> Next
 *
 * Result: `kubectl port-forward svc/isovalent-control-frontend <anyport>:3000`
 * is the only forward the app needs, and any local port works.
 *
 * Zero dependencies — Node's http module only, so nothing is added to the
 * production image.
 */
"use strict";

const http = require("http");
const { spawn } = require("child_process");
const { URL } = require("url");

const PORT = parseInt(process.env.PORT || "3000", 10);
const HOST = process.env.HOSTNAME || "0.0.0.0";
const NEXT_PORT = parseInt(process.env.NEXT_PORT || "3100", 10);
const BACKEND = (process.env.IC_BACKEND_URL || "http://isovalent-control-backend:8081").replace(/\/+$/, "");

const backend = new URL(BACKEND);
const BACKEND_HOST = backend.hostname;
const BACKEND_PORT = parseInt(backend.port || "80", 10);

const API_PREFIXES = ["/api/", "/ws/", "/healthz", "/metrics"];
const isBackendPath = (p) => API_PREFIXES.some((x) => p === x || p.startsWith(x));

let backendUp = null;
let backendErr = null;

// ------------------------------------------------------------------ next.js
// Spawned privately on loopback. The public listener is this process.
const next = spawn(process.execPath, [require("path").join(__dirname, "server.js")], {
  env: { ...process.env, PORT: String(NEXT_PORT), HOSTNAME: "127.0.0.1" },
  stdio: "inherit",
});
next.on("exit", (code, signal) => {
  console.error(`[proxy] next server exited (code=${code} signal=${signal}) — exiting so Kubernetes restarts us`);
  process.exit(code === null ? 1 : code);
});

// ------------------------------------------------------------------ helpers
function proxy(req, res, host, port, label) {
  const opts = {
    host,
    port,
    method: req.method,
    path: req.url,
    headers: { ...req.headers, host: `${host}:${port}` },
  };
  const up = http.request(opts, (upRes) => {
    if (label === "backend") {
      backendUp = true;
      backendErr = null;
    }
    res.writeHead(upRes.statusCode || 502, upRes.headers);
    upRes.pipe(res);
  });
  up.on("error", (err) => {
    if (label === "backend") {
      backendUp = false;
      backendErr = err.message;
    }
    if (res.headersSent) {
      res.destroy();
      return;
    }
    res.writeHead(502, { "content-type": "application/json" });
    res.end(
      JSON.stringify({
        error: `cannot reach ${label} at ${host}:${port} — ${err.message}`,
        hint:
          label === "backend"
            ? "The frontend pod cannot reach the backend Service. Check: kubectl -n isovalent-control get pods"
            : undefined,
      })
    );
  });
  req.pipe(up);
}

// ------------------------------------------------------------------ server
const server = http.createServer((req, res) => {
  const path = (req.url || "/").split("?")[0];

  // Container probes + a self-diagnostic the UI can call. Deliberately does NOT
  // depend on the backend, so a backend outage doesn't get the frontend killed.
  if (path === "/_ic/healthz") {
    res.writeHead(200, { "content-type": "text/plain" });
    res.end("ok\n");
    return;
  }
  if (path === "/_ic/proxy") {
    res.writeHead(200, { "content-type": "application/json" });
    res.end(
      JSON.stringify({
        backend: BACKEND,
        backendReachable: backendUp,
        backendError: backendErr,
        listenPort: PORT,
        nextPort: NEXT_PORT,
      })
    );
    return;
  }

  if (isBackendPath(path)) proxy(req, res, BACKEND_HOST, BACKEND_PORT, "backend");
  else proxy(req, res, "127.0.0.1", NEXT_PORT, "next");
});

// WebSocket upgrades. Next rewrites cannot proxy these, which is the other half
// of why the browser used to need a direct route to the backend.
server.on("upgrade", (req, socket, head) => {
  const path = (req.url || "/").split("?")[0];
  const toBackend = isBackendPath(path);
  const host = toBackend ? BACKEND_HOST : "127.0.0.1";
  const port = toBackend ? BACKEND_PORT : NEXT_PORT;

  socket.on("error", () => socket.destroy());

  const up = http.request({
    host,
    port,
    method: req.method,
    path: req.url,
    headers: { ...req.headers, host: `${host}:${port}` },
  });
  up.on("error", (err) => {
    if (toBackend) {
      backendUp = false;
      backendErr = err.message;
    }
    socket.destroy();
  });
  up.on("upgrade", (upRes, upSocket, upHead) => {
    if (toBackend) {
      backendUp = true;
      backendErr = null;
    }
    const lines = [`HTTP/1.1 ${upRes.statusCode} ${upRes.statusMessage}`];
    for (let i = 0; i < upRes.rawHeaders.length; i += 2) {
      lines.push(`${upRes.rawHeaders[i]}: ${upRes.rawHeaders[i + 1]}`);
    }
    socket.write(lines.join("\r\n") + "\r\n\r\n");
    if (upHead && upHead.length) socket.unshift(upHead);
    upSocket.on("error", () => upSocket.destroy());
    upSocket.pipe(socket);
    socket.pipe(upSocket);
  });
  up.on("response", (upRes) => {
    // Upgrade refused — pass the status back rather than hanging the client.
    socket.write(`HTTP/1.1 ${upRes.statusCode} ${upRes.statusMessage}\r\n\r\n`);
    socket.destroy();
  });
  if (head && head.length) up.write(head);
  up.end();
});

server.listen(PORT, HOST, () => {
  console.log(`[proxy] listening on ${HOST}:${PORT}`);
  console.log(`[proxy] /api,/ws,/healthz -> ${BACKEND}`);
  console.log(`[proxy] everything else   -> next on 127.0.0.1:${NEXT_PORT}`);
});

for (const sig of ["SIGTERM", "SIGINT"]) {
  process.on(sig, () => {
    try {
      next.kill(sig);
    } catch {
      /* already gone */
    }
    server.close(() => process.exit(0));
    setTimeout(() => process.exit(0), 5000).unref();
  });
}
