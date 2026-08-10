"use client";

/**
 * Form editors for the parts of a CiliumNetworkPolicy people actually change:
 * label selectors (tags), ingress sources, egress destinations, and ports.
 * Every edit mutates the manifest object, which the parent re-dumps to YAML —
 * so the form and the YAML stay in sync in both directions.
 */

type Labels = Record<string, string>;

export function LabelsEditor({
  labels,
  onChange,
  title,
}: {
  labels: Labels;
  onChange: (next: Labels) => void;
  title?: string;
}) {
  const entries = Object.entries(labels ?? {});
  return (
    <div className="space-y-1.5">
      {title && <div className="text-xs text-neutral-400">{title}</div>}
      {entries.length === 0 && (
        <div className="text-xs text-neutral-600">no labels — matches everything</div>
      )}
      {entries.map(([k, v], i) => (
        <div key={i} className="flex gap-1.5">
          <input
            value={k}
            placeholder="key (e.g. app)"
            onChange={(e) => {
              const next = entries.slice();
              next[i] = [e.target.value, v];
              onChange(Object.fromEntries(next));
            }}
            className="mono w-1/2 rounded border border-neutral-700 bg-neutral-900 px-2 py-1 text-xs text-white"
          />
          <input
            value={v}
            placeholder="value"
            onChange={(e) => {
              const next = entries.slice();
              next[i] = [k, e.target.value];
              onChange(Object.fromEntries(next));
            }}
            className="mono w-1/2 rounded border border-neutral-700 bg-neutral-900 px-2 py-1 text-xs text-white"
          />
          <button
            onClick={() => onChange(Object.fromEntries(entries.filter((_, idx) => idx !== i)))}
            className="rounded border border-neutral-700 px-2 text-xs text-neutral-400 hover:bg-neutral-800"
            title="remove label"
          >
            ✕
          </button>
        </div>
      ))}
      <button
        onClick={() => onChange({ ...Object.fromEntries(entries), "": "" })}
        className="rounded border border-dashed border-neutral-700 px-2 py-1 text-xs text-neutral-400 hover:bg-neutral-800"
      >
        ＋ label
      </button>
    </div>
  );
}

type Peer = { matchLabels?: Labels };
type Port = { port?: string; protocol?: string };
type Rule = {
  fromEndpoints?: Peer[];
  toEndpoints?: Peer[];
  toPorts?: { ports?: Port[] }[];
};

const PROTOCOLS = ["TCP", "UDP", "ANY"];

/** Editor for one ingress/egress rule: peers (source/destination) + ports. */
export function RulesEditor({
  rules,
  direction,
  onChange,
}: {
  rules: Rule[];
  direction: "ingress" | "egress";
  onChange: (next: Rule[]) => void;
}) {
  const peerKey = direction === "ingress" ? "fromEndpoints" : "toEndpoints";
  const peerLabel = direction === "ingress" ? "Sources (from)" : "Destinations (to)";

  const patch = (i: number, fn: (r: Rule) => void) => {
    const next = structuredClone(rules ?? []);
    fn(next[i]);
    onChange(next);
  };

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between">
        <div className="text-xs font-medium uppercase tracking-wider text-neutral-400">
          {direction}
        </div>
        <button
          onClick={() =>
            onChange([
              ...(rules ?? []),
              { [peerKey]: [{ matchLabels: { app: "" } }], toPorts: [{ ports: [{ port: "", protocol: "TCP" }] }] } as Rule,
            ])
          }
          className="rounded border border-dashed border-neutral-700 px-2 py-1 text-xs text-neutral-400 hover:bg-neutral-800"
        >
          ＋ {direction} rule
        </button>
      </div>

      {(rules ?? []).length === 0 && (
        <div className="rounded border border-neutral-800 px-3 py-2 text-xs text-neutral-600">
          no {direction} rules
        </div>
      )}

      {(rules ?? []).map((r, i) => {
        const peers: Peer[] = (r[peerKey] as Peer[]) ?? [];
        const ports: Port[] = r.toPorts?.[0]?.ports ?? [];
        return (
          <div key={i} className="space-y-2 rounded border border-neutral-800 bg-[color:var(--surface-0)] p-3">
            <div className="flex items-center justify-between">
              <span className="text-xs text-neutral-400">{peerLabel}</span>
              <button
                onClick={() => onChange(rules.filter((_, idx) => idx !== i))}
                className="text-xs text-neutral-500 hover:text-red-400"
              >
                remove rule
              </button>
            </div>

            {peers.map((p, pi) => (
              <div key={pi} className="rounded border border-neutral-800/70 p-2">
                <LabelsEditor
                  labels={p.matchLabels ?? {}}
                  onChange={(next) =>
                    patch(i, (rule) => {
                      const arr = (rule[peerKey] as Peer[]) ?? [];
                      arr[pi] = { matchLabels: next };
                      (rule as Record<string, unknown>)[peerKey] = arr;
                    })
                  }
                />
                {peers.length > 1 && (
                  <button
                    onClick={() =>
                      patch(i, (rule) => {
                        (rule as Record<string, unknown>)[peerKey] = (rule[peerKey] as Peer[]).filter((_, x) => x !== pi);
                      })
                    }
                    className="mt-1 text-xs text-neutral-500 hover:text-red-400"
                  >
                    remove peer
                  </button>
                )}
              </div>
            ))}
            <button
              onClick={() =>
                patch(i, (rule) => {
                  const arr = ((rule[peerKey] as Peer[]) ?? []).slice();
                  arr.push({ matchLabels: { app: "" } });
                  (rule as Record<string, unknown>)[peerKey] = arr;
                })
              }
              className="rounded border border-dashed border-neutral-700 px-2 py-1 text-xs text-neutral-400 hover:bg-neutral-800"
            >
              ＋ peer
            </button>

            <div className="text-xs text-neutral-400">Ports</div>
            <div className="space-y-1.5">
              {ports.map((pt, pti) => (
                <div key={pti} className="flex gap-1.5">
                  <input
                    value={pt.port ?? ""}
                    placeholder="port (e.g. 8080)"
                    onChange={(e) =>
                      patch(i, (rule) => {
                        rule.toPorts = rule.toPorts ?? [{ ports: [] }];
                        rule.toPorts[0].ports = (rule.toPorts[0].ports ?? []).slice();
                        rule.toPorts[0].ports![pti] = { ...pt, port: e.target.value };
                      })
                    }
                    className="mono w-32 rounded border border-neutral-700 bg-neutral-900 px-2 py-1 text-xs text-white"
                  />
                  <select
                    value={pt.protocol ?? "TCP"}
                    onChange={(e) =>
                      patch(i, (rule) => {
                        rule.toPorts = rule.toPorts ?? [{ ports: [] }];
                        rule.toPorts[0].ports = (rule.toPorts[0].ports ?? []).slice();
                        rule.toPorts[0].ports![pti] = { ...pt, protocol: e.target.value };
                      })
                    }
                    className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1 text-xs text-white"
                  >
                    {PROTOCOLS.map((p) => (
                      <option key={p}>{p}</option>
                    ))}
                  </select>
                  <button
                    onClick={() =>
                      patch(i, (rule) => {
                        rule.toPorts![0].ports = rule.toPorts![0].ports!.filter((_, x) => x !== pti);
                      })
                    }
                    className="rounded border border-neutral-700 px-2 text-xs text-neutral-400 hover:bg-neutral-800"
                  >
                    ✕
                  </button>
                </div>
              ))}
              <button
                onClick={() =>
                  patch(i, (rule) => {
                    rule.toPorts = rule.toPorts ?? [{ ports: [] }];
                    rule.toPorts[0].ports = [...(rule.toPorts[0].ports ?? []), { port: "", protocol: "TCP" }];
                  })
                }
                className="rounded border border-dashed border-neutral-700 px-2 py-1 text-xs text-neutral-400 hover:bg-neutral-800"
              >
                ＋ port
              </button>
            </div>
          </div>
        );
      })}
    </div>
  );
}
