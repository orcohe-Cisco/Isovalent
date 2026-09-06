/**
 * An empty table is ambiguous: it can mean "nothing happened" or "nothing is
 * connected". Every empty state here has to say which, and what to do next.
 */
export function EmptyState({
  title,
  body,
  action,
  tone = "neutral",
}: {
  title: string;
  body?: React.ReactNode;
  action?: React.ReactNode;
  tone?: "neutral" | "warn";
}) {
  return (
    <div
      className="panel flex flex-col items-start gap-2 px-5 py-8"
      style={
        tone === "warn"
          ? { borderColor: "rgba(217,89,38,0.35)", background: "rgba(217,89,38,0.06)" }
          : undefined
      }
    >
      <p className="text-[14px] font-medium">{title}</p>
      {body && (
        <div className="max-w-xl text-[13px] leading-relaxed text-[color:var(--text-secondary)]">
          {body}
        </div>
      )}
      {action && <div className="pt-1">{action}</div>}
    </div>
  );
}
