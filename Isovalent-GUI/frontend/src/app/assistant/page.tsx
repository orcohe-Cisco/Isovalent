"use client";

import { useCallback, useState } from "react";
import { apiPost } from "@/lib/api";
import { usePoll } from "@/lib/usePoll";
import { useConfig } from "@/lib/config";
import type { AiResponse, AiSuggestion } from "@/lib/types";
import { PageHeader } from "@/components/ui/PageHeader";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorNote } from "@/components/ui/ErrorNote";
import { Chip } from "@/components/ui/Chip";

interface Status {
  enabled: boolean;
  provider: string;
  model?: string;
  reason?: string;
  providers: string[];
}

const KIND_TONE: Record<string, "accent" | "danger" | "ok" | "warn" | "violet"> = {
  enforce: "danger",
  monitor: "accent",
  exclude: "ok",
  disable: "warn",
  tune: "violet",
  investigate: "accent",
};

/**
 * The assistant reads; it does not write.
 *
 * Every suggestion has to be approved, and approval goes through exactly the
 * same audited endpoint a human click uses — the model never hands us a
 * manifest. The context it was given is shown alongside the answer, because an
 * agent whose inputs you cannot inspect is an agent you cannot audit.
 */
export default function AssistantPage() {
  const { config } = useConfig();
  const { data: status } = usePoll<Status>("/api/v1/ai/status", 60000);
  const [question, setQuestion] = useState("");
  const [answer, setAnswer] = useState<AiResponse | null>(null);
  const [context, setContext] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [applied, setApplied] = useState<Record<string, string>>({});

  const ask = useCallback(async () => {
    setBusy(true);
    setError(null);
    try {
      const res = await apiPost<{ response: AiResponse; context: unknown }>("/api/v1/ai/analyze", {
        question,
      });
      setAnswer(res.response);
      setContext(res.context);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }, [question]);

  const approve = useCallback(async (s: AiSuggestion) => {
    const key = `${s.kind}:${s.namespace ?? ""}/${s.policy}`;
    setApplied((p) => ({ ...p, [key]: "applying" }));
    try {
      await apiPost("/api/v1/ai/apply", {
        kind: s.kind,
        policy: s.policy,
        namespace: s.namespace ?? "",
        exclusions: s.exclusions ?? {},
      });
      setApplied((p) => ({ ...p, [key]: "applied" }));
    } catch (e) {
      setApplied((p) => ({ ...p, [key]: e instanceof Error ? e.message : String(e) }));
    }
  }, []);

  if (status && !status.enabled) {
    return (
      <>
        <PageHeader title="Assistant" />
        <EmptyState
          tone="warn"
          title="No model is configured"
          body={
            <>
              <p>{status.reason}</p>
              <p className="mt-2">
                Set <code className="mono">IC_AI_PROVIDER</code> to{" "}
                <code className="mono">anthropic</code>, <code className="mono">gemini</code> or{" "}
                <code className="mono">openai</code> (any OpenAI-compatible gateway works — set{" "}
                <code className="mono">IC_AI_BASE_URL</code>), and mount the key with{" "}
                <code className="mono">IC_AI_API_KEY_FILE</code> pointing at a Kubernetes Secret.
              </p>
              <p className="mt-2">
                The key stays on the backend. It is never sent to the browser, and this page can
                only ever report whether one is present.
              </p>
            </>
          }
        />
      </>
    );
  }

  return (
    <>
      <PageHeader
        title="Assistant"
        subtitle="An external model reviews what your policies have actually matched and proposes changes. It cannot apply anything — you approve each one, and the change goes through the same audited path as a click."
        actions={
          status && (
            <Chip mono>
              {status.provider} · {status.model}
            </Chip>
          )
        }
      />

      <ErrorNote error={error} />

      <div className="panel mb-4 p-4">
        <textarea
          value={question}
          onChange={(e) => setQuestion(e.target.value)}
          placeholder="Optional: ask something specific. “Which of these is safe to switch to enforcement?” · “Why is the file policy so noisy?” · Leave blank for a general review."
          className="field h-20 resize-y !text-[13px]"
        />
        <div className="mt-2.5 flex flex-wrap items-center gap-2">
          <button className="btn btn-primary" onClick={ask} disabled={busy}>
            {busy ? "Thinking…" : "Review the current posture"}
          </button>
          <span className="text-[11px] text-[color:var(--text-tertiary)]">
            Sends policy names, hit counts and the top matched values for cluster{" "}
            <span className="mono">{config.cluster}</span>. No manifests, no secrets, no raw logs.
          </span>
        </div>
      </div>

      {answer && (
        <div className="space-y-4">
          {answer.summary && (
            <div className="panel px-4 py-3">
              <div className="section-label mb-1">Summary</div>
              <p className="text-[13px] leading-relaxed">{answer.summary}</p>
            </div>
          )}

          {answer.suggestions?.length ? (
            <ul className="space-y-3">
              {answer.suggestions.map((s, i) => {
                const key = `${s.kind}:${s.namespace ?? ""}/${s.policy}`;
                const state = applied[key];
                const actionable = ["enforce", "monitor", "exclude", "disable"].includes(s.kind);
                return (
                  <li key={i} className="panel px-4 py-3">
                    <div className="flex flex-wrap items-center gap-2">
                      <Chip tone={KIND_TONE[s.kind] ?? "neutral"}>{s.kind}</Chip>
                      <span className="mono text-[12px]">
                        {s.namespace ? `${s.namespace}/` : ""}
                        {s.policy}
                      </span>
                      {s.confidence && <Chip>{s.confidence} confidence</Chip>}
                    </div>
                    <p className="mt-1.5 text-[13px] font-medium">{s.title}</p>
                    <p className="mt-1 text-[12px] leading-relaxed text-[color:var(--text-secondary)]">
                      {s.rationale}
                    </p>
                    {s.risk && (
                      <p className="mt-1.5 text-[12px] leading-relaxed" style={{ color: "#e0b25a" }}>
                        Risk: {s.risk}
                      </p>
                    )}
                    {s.exclusions && Object.values(s.exclusions).some((v) => Array.isArray(v) && v.length) && (
                      <pre className="mono mt-2 rounded-lg p-2 text-[11px]" style={{ background: "var(--surface-2)" }}>
                        {JSON.stringify(s.exclusions, null, 2)}
                      </pre>
                    )}
                    <div className="mt-2.5 flex items-center gap-2">
                      {actionable && (
                        <button
                          className="btn btn-secondary !py-1.5 !text-[12px]"
                          disabled={state === "applying" || state === "applied"}
                          onClick={() => void approve(s)}
                        >
                          {state === "applied" ? "Applied" : state === "applying" ? "Applying…" : "Approve and apply"}
                        </button>
                      )}
                      {state && state !== "applied" && state !== "applying" && (
                        <span className="text-[11px]" style={{ color: "#f0a3a3" }}>
                          {state}
                        </span>
                      )}
                    </div>
                  </li>
                );
              })}
            </ul>
          ) : (
            <EmptyState
              title="No changes proposed"
              body="An empty suggestion list is a valid answer — it usually means there is not enough evidence yet. Let the policies run in monitor mode for longer and ask again."
            />
          )}

          <details className="panel px-4 py-3">
            <summary className="cursor-pointer text-[12px] text-[color:var(--text-secondary)]">
              What the model was told
            </summary>
            <pre className="mono mt-2 max-h-96 overflow-auto whitespace-pre-wrap text-[11px] text-[color:var(--text-tertiary)]">
              {JSON.stringify(context, null, 2)}
            </pre>
          </details>

          {answer.raw && !answer.suggestions?.length && (
            <details className="panel px-4 py-3">
              <summary className="cursor-pointer text-[12px] text-[color:var(--text-secondary)]">
                Raw reply
              </summary>
              <pre className="mono mt-2 max-h-96 overflow-auto whitespace-pre-wrap text-[11px]">
                {answer.raw}
              </pre>
            </details>
          )}
        </div>
      )}
    </>
  );
}
