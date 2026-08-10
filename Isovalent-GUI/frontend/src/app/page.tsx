"use client";

import { useEffect, useState } from "react";
import {
  Area,
  AreaChart,
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { apiGet } from "@/lib/api";
import { useStream } from "@/lib/useStream";
import type { Alert, OverviewResponse } from "@/lib/types";
import { Badge, StatCard } from "@/components/StatCard";

const S1 = "#3987e5"; // flows (blue, slot 1)
const S8 = "#e66767"; // drops/errors (red, slot 8)
const S3 = "#199e70"; // aqua, slot 3

function fmtTime(t: number) {
  return new Date(t * 1000).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

const tooltipStyle = {
  backgroundColor: "#232322",
  border: "1px solid #3a3a38",
  borderRadius: 6,
  fontSize: 12,
};

export default function OverviewPage() {
  const [data, setData] = useState<OverviewResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const { items: liveAlerts } = useStream<Alert>("/ws/alerts", 40);

  useEffect(() => {
    let stop = false;
    const load = () =>
      apiGet<OverviewResponse>("/api/v1/overview")
        .then((d) => !stop && (setData(d), setError(null)))
        .catch((e) => !stop && setError(String(e)));
    load();
    const t = setInterval(load, 5000);
    return () => {
      stop = true;
      clearInterval(t);
    };
  }, []);

  const o = data?.overview;
  const series =
    o?.series.map((p) => ({
      ...p,
      time: fmtTime(p.t),
      errPct: p.httpReq > 0 ? (100 * p.httpErr) / p.httpReq : 0,
    })) ?? [];
  const alerts = [
    ...liveAlerts,
    ...(data?.alerts ? [...data.alerts].reverse() : []),
  ].slice(0, 30);

  return (
    <div className="space-y-6">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-lg font-semibold">Cluster Overview</h1>
          <p className="text-sm text-neutral-400">
            cluster <span className="mono">{data?.cluster ?? "…"}</span>
            {data?.mode === "mock" && (
              <span className="ml-2">
                <Badge tone="muted">demo data</Badge>
              </span>
            )}
          </p>
        </div>
        {error && <Badge tone="crit">API unreachable: {error}</Badge>}
      </header>

      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <StatCard
          label="Flow rate"
          value={o ? `${o.flowRate.toFixed(1)}/s` : "—"}
          sub={`${o?.totalFlows ?? 0} flows total`}
          accent="blue"
        />
        <StatCard
          label="Policy drops"
          value={o ? `${o.dropRate.toFixed(2)}/s` : "—"}
          sub={`${o?.totalDrops ?? 0} dropped total`}
          accent="red"
        />
        <StatCard
          label="HTTP 5xx (15m)"
          value={o ? `${o.httpErrPct.toFixed(1)}%` : "—"}
          sub={`${o?.dnsErrors ?? 0} DNS failures`}
          accent="orange"
        />
        <StatCard
          label="Runtime kills"
          value={o ? String(o.totalKills) : "—"}
          sub={`${o?.totalEvents ?? 0} Tetragon events`}
          accent="aqua"
        />
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <section className="panel p-4">
          <h2 className="mb-3 text-sm font-medium text-neutral-300">
            Traffic &amp; policy drops{" "}
            <span className="text-neutral-500">(per 10s, last 15m)</span>
          </h2>
          <div className="h-56">
            <ResponsiveContainer>
              <AreaChart data={series} margin={{ top: 4, right: 8 }}>
                <CartesianGrid stroke="#2a2a28" strokeDasharray="0" vertical={false} />
                <XAxis dataKey="time" tick={{ fontSize: 10, fill: "#8a8a85" }} minTickGap={40} tickLine={false} axisLine={{ stroke: "#3a3a38" }} />
                <YAxis tick={{ fontSize: 10, fill: "#8a8a85" }} width={36} tickLine={false} axisLine={false} />
                <Tooltip contentStyle={tooltipStyle} />
                <Legend wrapperStyle={{ fontSize: 12 }} />
                <Area type="monotone" dataKey="flows" name="Flows" stroke={S1} strokeWidth={2} fill={S1} fillOpacity={0.15} />
                <Area type="monotone" dataKey="drops" name="Drops" stroke={S8} strokeWidth={2} fill={S8} fillOpacity={0.25} />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </section>

        <section className="panel p-4">
          <h2 className="mb-3 text-sm font-medium text-neutral-300">
            Golden signals{" "}
            <span className="text-neutral-500">(HTTP 5xx %, enforcement kills)</span>
          </h2>
          <div className="h-56">
            <ResponsiveContainer>
              <LineChart data={series} margin={{ top: 4, right: 8 }}>
                <CartesianGrid stroke="#2a2a28" vertical={false} />
                <XAxis dataKey="time" tick={{ fontSize: 10, fill: "#8a8a85" }} minTickGap={40} tickLine={false} axisLine={{ stroke: "#3a3a38" }} />
                <YAxis tick={{ fontSize: 10, fill: "#8a8a85" }} width={36} tickLine={false} axisLine={false} />
                <Tooltip contentStyle={tooltipStyle} />
                <Legend wrapperStyle={{ fontSize: 12 }} />
                <Line type="monotone" dataKey="errPct" name="HTTP 5xx %" stroke={S8} strokeWidth={2} dot={false} />
                <Line type="monotone" dataKey="kills" name="Kills" stroke={S3} strokeWidth={2} dot={false} />
              </LineChart>
            </ResponsiveContainer>
          </div>
        </section>
      </div>

      <section className="panel">
        <div className="flex items-center justify-between border-b border-neutral-800 px-4 py-3">
          <h2 className="text-sm font-medium text-neutral-300">
            Active security violations
          </h2>
          <span className="text-xs text-neutral-500">
            network drops + Tetragon enforcement, live
          </span>
        </div>
        <ul className="max-h-80 divide-y divide-neutral-800/70 overflow-y-auto">
          {alerts.length === 0 && (
            <li className="px-4 py-6 text-sm text-neutral-500">
              No alerts yet.
            </li>
          )}
          {alerts.map((a, i) => (
            <li key={i} className="flex items-start gap-3 px-4 py-2.5">
              <Badge tone={a.severity === "critical" ? "crit" : "warn"}>
                {a.severity === "critical" ? "CRITICAL" : "WARN"}
              </Badge>
              <div className="min-w-0">
                <div className="truncate text-sm">{a.title}</div>
                <div className="mono truncate text-xs text-neutral-500">
                  {new Date(a.time).toLocaleTimeString()} · {a.detail}
                  {a.policy && ` · policy=${a.policy}`}
                </div>
              </div>
            </li>
          ))}
        </ul>
      </section>
    </div>
  );
}
