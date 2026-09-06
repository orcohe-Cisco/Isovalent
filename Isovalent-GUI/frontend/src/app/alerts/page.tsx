"use client";

import { useCallback, useEffect, useState } from "react";
import { apiGet, apiPost, apiPut } from "@/lib/api";
import { useConfig, type SinkSpec } from "@/lib/config";
import type { AlertRoute } from "@/lib/types";
import { PageHeader } from "@/components/ui/PageHeader";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorNote } from "@/components/ui/ErrorNote";
import { Chip } from "@/components/ui/Chip";

const KINDS = [
  { id: "network_drop", label: "Network drops" },
  { id: "runtime_enforcement", label: "Runtime enforcement" },
  { id: "http_error", label: "HTTP errors" },
];

function emptyRoute(type: string): AlertRoute {
  return {
    id: `r${Date.now().toString(36)}`,
    name: "New route",
    type: type as AlertRoute["type"],
    url: "",
    minSeverity: "warning",
    enabled: true,
  };
}

/**
 * Alert routing.
 *
 * Each destination has different words for the same three fields, so the form
 * is driven by the backend's sink catalogue rather than a switch statement
 * here: "token" is a routing key for PagerDuty, a bot token for Webex and an
 * HEC token for Splunk, and labelling all three "token" is how integrations
 * end up misconfigured.
 */
export default function AlertsPage() {
  const { config } = useConfig();
  const [routes, setRoutes] = useState<AlertRoute[]>([]);
  const [specs, setSpecs] = useState<SinkSpec[]>([]);
  const [status, setStatus] = useState<{ ok: boolean; msg: string } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [testing, setTesting] = useState<string | null>(null);
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    apiGet<AlertRoute[]>("/api/v1/alerts/routes")
      .then(setRoutes)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)));
    apiGet<SinkSpec[]>("/api/v1/alerts/sinks")
      .then(setSpecs)
      .catch(() => setSpecs(config.alertSinks));
  }, [config.alertSinks]);

  const specFor = useCallback(
    (type: string) => specs.find((s) => s.type === type),
    [specs],
  );

  const update = (i: number, patch: Partial<AlertRoute>) => {
    setDirty(true);
    setRoutes((prev) => prev.map((r, idx) => (idx === i ? { ...r, ...patch } : r)));
  };

  const save = async () => {
    try {
      setRoutes(await apiPut<AlertRoute[]>("/api/v1/alerts/routes", routes));
      setStatus({ ok: true, msg: "Routing saved" });
      setDirty(false);
    } catch (e) {
      setStatus({ ok: false, msg: e instanceof Error ? e.message : String(e) });
    }
  };

  const test = async (r: AlertRoute) => {
    setTesting(r.id);
    setStatus(null);
    try {
      await apiPost("/api/v1/alerts/routes/test", r);
      setStatus({ ok: true, msg: `Test alert delivered to ${r.name}` });
    } catch (e) {
      setStatus({ ok: false, msg: `Delivery failed: ${e instanceof Error ? e.message : String(e)}` });
    } finally {
      setTesting(null);
    }
  };

  return (
    <>
      <PageHeader
        title="Integrations"
        subtitle="Where security events go. Alerts are deduplicated per kind and title within a 60-second window before delivery, so one noisy policy cannot page you 400 times."
        actions={
          <>
            <select
              className="field !w-auto !py-1.5 !text-[12px]"
              value=""
              onChange={(e) => {
                if (!e.target.value) return;
                setRoutes((r) => [...r, emptyRoute(e.target.value)]);
                setDirty(true);
              }}
            >
              <option value="">＋ Add destination…</option>
              {specs.map((s) => (
                <option key={s.type} value={s.type}>
                  {s.label}
                </option>
              ))}
            </select>
            <button className="btn btn-primary" onClick={save} disabled={!dirty}>
              Save
            </button>
          </>
        }
      />

      <ErrorNote error={error} />

      {status && (
        <div
          className="mb-4 rounded-lg px-3.5 py-2.5 text-[13px]"
          style={
            status.ok
              ? { background: "rgba(25,158,112,0.12)", color: "#6dd3ab" }
              : { background: "rgba(230,103,103,0.12)", color: "#f0a3a3" }
          }
        >
          {status.msg}
        </div>
      )}

      {routes.length === 0 ? (
        <EmptyState
          title="No destinations configured"
          body={
            <p>
              Add one above. Slack and Teams take an incoming-webhook URL and nothing else; syslog
              and the SIEM sinks take a collector address and a format.
            </p>
          }
        />
      ) : (
        <div className="space-y-3">
          {routes.map((r, i) => {
            const spec = specFor(r.type);
            return (
              <section key={r.id} className="panel p-4">
                <div className="flex flex-wrap items-center gap-2">
                  <input
                    value={r.name}
                    onChange={(e) => update(i, { name: e.target.value })}
                    className="field !w-44 !py-1.5 font-medium"
                  />
                  <select
                    value={r.type}
                    onChange={(e) => update(i, { type: e.target.value as AlertRoute["type"] })}
                    className="field !w-auto !py-1.5 !text-[12px]"
                  >
                    {specs.map((s) => (
                      <option key={s.type} value={s.type}>
                        {s.label}
                      </option>
                    ))}
                  </select>
                  <select
                    value={r.minSeverity}
                    onChange={(e) => update(i, { minSeverity: e.target.value as "warning" | "critical" })}
                    className="field !w-auto !py-1.5 !text-[12px]"
                  >
                    <option value="warning">warning and above</option>
                    <option value="critical">critical only</option>
                  </select>
                  <label className="flex cursor-pointer items-center gap-1.5 text-[12px] text-[color:var(--text-secondary)]">
                    <input
                      type="checkbox"
                      checked={r.enabled}
                      onChange={(e) => update(i, { enabled: e.target.checked })}
                    />
                    enabled
                  </label>
                  <div className="ml-auto flex gap-2">
                    <button
                      className="btn btn-secondary !py-1.5 !text-[12px]"
                      onClick={() => void test(r)}
                      disabled={testing === r.id || !r.url}
                    >
                      {testing === r.id ? "Sending…" : "Send test"}
                    </button>
                    <button
                      className="btn btn-danger !py-1.5 !text-[12px]"
                      onClick={() => {
                        setRoutes((prev) => prev.filter((_, idx) => idx !== i));
                        setDirty(true);
                      }}
                    >
                      Remove
                    </button>
                  </div>
                </div>

                {spec?.notes && (
                  <p className="mt-2 text-[11.5px] leading-relaxed text-[color:var(--text-tertiary)]">
                    {spec.notes}
                  </p>
                )}

                <div className="mt-3 grid gap-2 sm:grid-cols-2">
                  <label className="text-[11px] text-[color:var(--text-tertiary)]">
                    {spec?.urlLabel ?? "URL"}
                    <input
                      value={r.url}
                      placeholder={spec?.urlHint}
                      onChange={(e) => update(i, { url: e.target.value })}
                      className="field mono mt-1 !py-1.5 !text-[12px]"
                    />
                  </label>
                  {spec?.tokenLabel && (
                    <label className="text-[11px] text-[color:var(--text-tertiary)]">
                      {spec.tokenLabel}
                      <input
                        value={r.token ?? ""}
                        type="password"
                        onChange={(e) => update(i, { token: e.target.value })}
                        className="field mono mt-1 !py-1.5 !text-[12px]"
                      />
                    </label>
                  )}
                  {spec?.targetLabel && (
                    <label className="text-[11px] text-[color:var(--text-tertiary)]">
                      {spec.targetLabel}
                      <input
                        value={r.target ?? ""}
                        placeholder={spec.targetHint}
                        onChange={(e) => update(i, { target: e.target.value })}
                        className="field mono mt-1 !py-1.5 !text-[12px]"
                      />
                    </label>
                  )}
                  {spec?.formatOptions?.length ? (
                    <label className="text-[11px] text-[color:var(--text-tertiary)]">
                      Format
                      <select
                        value={r.format ?? spec.formatOptions[0]}
                        onChange={(e) => update(i, { format: e.target.value })}
                        className="field mt-1 !py-1.5 !text-[12px]"
                      >
                        {spec.formatOptions.map((f) => (
                          <option key={f} value={f}>
                            {f === "cef" ? "CEF (ArcSight, QRadar, most SIEMs)" : "RFC5424 syslog"}
                          </option>
                        ))}
                      </select>
                    </label>
                  ) : null}
                </div>

                <div className="mt-3 flex flex-wrap items-center gap-2">
                  <span className="section-label">Only send</span>
                  {KINDS.map((k) => {
                    const on = (r.kinds ?? []).includes(k.id);
                    return (
                      <button
                        key={k.id}
                        onClick={() => {
                          const cur = r.kinds ?? [];
                          update(i, {
                            kinds: on ? cur.filter((x) => x !== k.id) : [...cur, k.id],
                          });
                        }}
                      >
                        <Chip tone={on ? "accent" : "neutral"}>{k.label}</Chip>
                      </button>
                    );
                  })}
                  {(r.kinds ?? []).length === 0 && (
                    <span className="text-[11px] text-[color:var(--text-tertiary)]">
                      everything
                    </span>
                  )}
                  <input
                    value={(r.namespaces ?? []).join(",")}
                    placeholder="namespace filter, comma separated (optional)"
                    onChange={(e) =>
                      update(i, {
                        namespaces: e.target.value
                          .split(",")
                          .map((s) => s.trim())
                          .filter(Boolean),
                      })
                    }
                    className="field mono ml-auto !w-72 !py-1 !text-[11px]"
                  />
                </div>
              </section>
            );
          })}
        </div>
      )}
    </>
  );
}
