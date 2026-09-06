"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import yaml from "js-yaml";
import { apiDelete, apiPost, apiText, apiUrl } from "@/lib/api";
import { usePoll } from "@/lib/usePoll";
import type { ApiKey } from "@/lib/types";
import { PageHeader } from "@/components/ui/PageHeader";
import { ErrorNote } from "@/components/ui/ErrorNote";
import { EmptyState } from "@/components/ui/EmptyState";
import { Chip } from "@/components/ui/Chip";

type Tab = "tokens" | "reference";

interface Operation {
  method: string;
  path: string;
  summary?: string;
  description?: string;
  tags?: string[];
  parameters?: { name: string; in: string; required?: boolean; description?: string; schema?: { type?: string; enum?: string[]; default?: unknown } }[];
  responses?: Record<string, { description?: string }>;
}

interface Spec {
  info?: { title?: string; version?: string; description?: string };
  tags?: { name: string }[];
  paths?: Record<string, Record<string, unknown>>;
}

const METHOD_TONE: Record<string, "accent" | "ok" | "warn" | "danger" | "violet"> = {
  get: "accent",
  post: "ok",
  put: "warn",
  delete: "danger",
  patch: "violet",
};

export default function ApiPage() {
  const [tab, setTab] = useState<Tab>("tokens");
  return (
    <>
      <PageHeader
        title="API"
        subtitle="Everything the console does, it does through this API — there is no privileged back channel. Anything you can click, you can automate."
        actions={
          <div className="flex gap-1">
            {(["tokens", "reference"] as Tab[]).map((t) => (
              <button
                key={t}
                onClick={() => setTab(t)}
                className={`btn ${t === tab ? "btn-secondary" : "btn-ghost"} !px-2.5 !py-1.5 !text-[12px]`}
              >
                {t === "tokens" ? "Tokens" : "Reference"}
              </button>
            ))}
          </div>
        }
      />
      {tab === "tokens" ? <Tokens /> : <Reference />}
    </>
  );
}

/* ------------------------------------------------------------- tokens --- */

function Tokens() {
  const { data, error, reload } = usePoll<ApiKey[]>("/api/v1/apikeys", 30000, []);
  const [name, setName] = useState("");
  const [role, setRole] = useState("viewer");
  const [ttl, setTtl] = useState(0);
  const [created, setCreated] = useState<{ token: string; name: string } | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const create = useCallback(async () => {
    setBusy(true);
    setErr(null);
    try {
      const res = await apiPost<{ key: ApiKey; token: string }>("/api/v1/apikeys", {
        name,
        role,
        ttlDays: ttl,
      });
      setCreated({ token: res.token, name: res.key.name });
      setName("");
      void reload();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }, [name, role, ttl, reload]);

  return (
    <div className="space-y-4">
      <ErrorNote error={err ?? error} />

      <section className="panel px-4 py-3">
        <h2 className="text-[14px] font-medium">Issue a token</h2>
        <p className="mt-1 max-w-3xl text-[12px] leading-relaxed text-[color:var(--text-secondary)]">
          For CI, scripts and external agents, so automation never has to hold a person&apos;s SSO
          credentials. Only a hash is stored — the token is shown once, here, and cannot be read
          back afterwards.
        </p>
        <div className="mt-3 flex flex-wrap items-end gap-2">
          <label className="text-[11px] text-[color:var(--text-tertiary)]">
            Name
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="ci-pipeline"
              className="field mt-1 !w-48 !py-1.5"
            />
          </label>
          <label className="text-[11px] text-[color:var(--text-tertiary)]">
            Role
            <select value={role} onChange={(e) => setRole(e.target.value)} className="field mt-1 !w-32 !py-1.5">
              <option value="viewer">viewer</option>
              <option value="editor">editor</option>
              <option value="admin">admin</option>
            </select>
          </label>
          <label className="text-[11px] text-[color:var(--text-tertiary)]">
            Expires
            <select
              value={ttl}
              onChange={(e) => setTtl(Number(e.target.value))}
              className="field mt-1 !w-36 !py-1.5"
            >
              <option value={0}>never</option>
              <option value={30}>in 30 days</option>
              <option value={90}>in 90 days</option>
              <option value={365}>in a year</option>
            </select>
          </label>
          <button className="btn btn-primary" onClick={create} disabled={!name.trim() || busy}>
            {busy ? "Issuing…" : "Issue token"}
          </button>
        </div>

        {created && (
          <div className="mt-3 rounded-lg px-3 py-2.5" style={{ background: "rgba(25,158,112,0.1)" }}>
            <div className="text-[12px]" style={{ color: "#6dd3ab" }}>
              Token for <strong>{created.name}</strong> — copy it now, it will not be shown again.
            </div>
            <code className="mono mt-1.5 block break-all rounded p-2 text-[12px]" style={{ background: "var(--surface-0)" }}>
              {created.token}
            </code>
            <button
              className="btn btn-secondary mt-2 !py-1 !text-[11px]"
              onClick={() => void navigator.clipboard.writeText(created.token)}
            >
              Copy
            </button>
          </div>
        )}
      </section>

      {(data ?? []).length === 0 ? (
        <EmptyState title="No tokens issued" body="Issue one above, or set IC_API_TOKENS in the deployment for tokens managed as configuration." />
      ) : (
        <div className="panel overflow-hidden">
          <table className="w-full text-[12px]">
            <thead>
              <tr className="section-label">
                <th className="px-4 py-2 text-left font-medium">Name</th>
                <th className="px-2 py-2 text-left font-medium">Role</th>
                <th className="px-2 py-2 text-left font-medium">Prefix</th>
                <th className="px-2 py-2 text-right font-medium">Uses</th>
                <th className="px-2 py-2 text-left font-medium">Last used</th>
                <th className="px-2 py-2 text-left font-medium">Expires</th>
                <th className="px-4 py-2" />
              </tr>
            </thead>
            <tbody>
              {(data ?? []).map((k) => (
                <tr key={k.id} className="hairline-t">
                  <td className="px-4 py-2">{k.name}</td>
                  <td className="px-2 py-2">
                    <Chip tone={k.role === "admin" ? "danger" : k.role === "editor" ? "warn" : "neutral"}>
                      {k.role}
                    </Chip>
                  </td>
                  <td className="mono px-2 py-2 text-[color:var(--text-tertiary)]">{k.prefix}…</td>
                  <td className="mono px-2 py-2 text-right">{k.uses}</td>
                  <td className="px-2 py-2 text-[color:var(--text-tertiary)]">
                    {k.lastUsed ? new Date(k.lastUsed).toLocaleString() : "never"}
                  </td>
                  <td className="px-2 py-2 text-[color:var(--text-tertiary)]">
                    {k.expires ? new Date(k.expires).toLocaleDateString() : "never"}
                  </td>
                  <td className="px-4 py-2 text-right">
                    {k.static ? (
                      <Chip title="Set in configuration; remove it there">config</Chip>
                    ) : (
                      <button
                        className="btn btn-ghost !px-2 !py-1 !text-[11px]"
                        onClick={async () => {
                          await apiDelete(`/api/v1/apikeys/${k.id}`);
                          void reload();
                        }}
                      >
                        Revoke
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

/* ---------------------------------------------------------- reference --- */

function Reference() {
  const [spec, setSpec] = useState<Spec | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");

  useEffect(() => {
    apiText("/api/openapi.yaml")
      .then((t) => setSpec(yaml.load(t) as Spec))
      .catch((e) => setError(e instanceof Error ? e.message : String(e)));
  }, []);

  const operations = useMemo(() => {
    const out: Operation[] = [];
    for (const [path, methods] of Object.entries(spec?.paths ?? {})) {
      for (const [method, op] of Object.entries(methods)) {
        if (!["get", "post", "put", "delete", "patch"].includes(method)) continue;
        out.push({ method, path, ...(op as object) } as Operation);
      }
    }
    return out;
  }, [spec]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return operations;
    return operations.filter((o) =>
      `${o.method} ${o.path} ${o.summary ?? ""} ${(o.tags ?? []).join(" ")}`.toLowerCase().includes(q),
    );
  }, [operations, query]);

  const grouped = useMemo(() => {
    const g = new Map<string, Operation[]>();
    for (const o of filtered) {
      const tag = o.tags?.[0] ?? "Other";
      g.set(tag, [...(g.get(tag) ?? []), o]);
    }
    return g;
  }, [filtered]);

  if (error) return <ErrorNote error={error} />;
  if (!spec) return <p className="text-[13px] text-[color:var(--text-tertiary)]">Loading the spec…</p>;

  return (
    <div className="space-y-4">
      <div className="panel px-4 py-3">
        <div className="flex flex-wrap items-center gap-2">
          <h2 className="text-[14px] font-medium">
            {spec.info?.title} <span className="mono text-[12px] text-[color:var(--text-tertiary)]">v{spec.info?.version}</span>
          </h2>
          <a className="btn btn-ghost !py-1 !text-[11px]" href={apiUrl("/api/openapi.yaml")} target="_blank" rel="noreferrer">
            openapi.yaml ↗
          </a>
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Filter endpoints…"
            className="field ml-auto !w-56 !py-1.5"
          />
        </div>
        {spec.info?.description && (
          <div className="mt-2 max-w-3xl whitespace-pre-wrap text-[12px] leading-relaxed text-[color:var(--text-secondary)]">
            {spec.info.description}
          </div>
        )}
      </div>

      {[...grouped.entries()].map(([tag, ops]) => (
        <section key={tag} className="panel overflow-hidden">
          <div className="section-label px-4 pb-1.5 pt-3">{tag}</div>
          <ul>
            {ops.map((o) => (
              <li key={`${o.method}:${o.path}`} className="hairline-t">
                <details>
                  <summary className="flex cursor-pointer flex-wrap items-center gap-2 px-4 py-2">
                    <Chip tone={METHOD_TONE[o.method] ?? "neutral"}>{o.method.toUpperCase()}</Chip>
                    <code className="mono text-[12px]">{o.path}</code>
                    <span className="min-w-0 flex-1 truncate text-[12px] text-[color:var(--text-secondary)]">
                      {o.summary}
                    </span>
                  </summary>
                  <div className="space-y-3 px-4 pb-3 pl-14">
                    {o.description && (
                      <p className="whitespace-pre-wrap text-[12px] leading-relaxed text-[color:var(--text-secondary)]">
                        {o.description}
                      </p>
                    )}
                    {o.parameters?.length ? (
                      <div>
                        <div className="section-label mb-1">Parameters</div>
                        <table className="w-full text-[11.5px]">
                          <tbody>
                            {o.parameters.map((p, i) => (
                              <tr key={i} className="align-top">
                                <td className="mono py-1 pr-3 whitespace-nowrap">
                                  {p.name}
                                  {p.required && <span style={{ color: "#f0a3a3" }}>*</span>}
                                </td>
                                <td className="py-1 pr-3 text-[color:var(--text-tertiary)]">{p.in}</td>
                                <td className="mono py-1 pr-3 text-[color:var(--text-tertiary)]">
                                  {p.schema?.enum ? p.schema.enum.join(" | ") : p.schema?.type}
                                </td>
                                <td className="py-1 text-[color:var(--text-secondary)]">{p.description}</td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      </div>
                    ) : null}
                    <div>
                      <div className="section-label mb-1">Try it</div>
                      <pre className="mono overflow-x-auto rounded-lg p-2.5 text-[11px]" style={{ background: "var(--surface-0)", border: "1px solid var(--hairline)" }}>
{`curl -H "Authorization: Bearer $IC_TOKEN" \\
  -X ${o.method.toUpperCase()} "${apiUrl(o.path)}"`}
                      </pre>
                    </div>
                  </div>
                </details>
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}
