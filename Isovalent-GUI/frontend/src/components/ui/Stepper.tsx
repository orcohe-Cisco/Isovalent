"use client";

export interface Step {
  id: string;
  label: string;
  hint?: string;
}

/**
 * Wizard progress rail. Completed steps are clickable so people can go back
 * without losing their selection; steps ahead of the current one are not,
 * because they may depend on choices not yet made.
 */
export function Stepper({
  steps,
  current,
  onJump,
}: {
  steps: Step[];
  current: number;
  onJump?: (index: number) => void;
}) {
  return (
    <ol className="flex items-center gap-1">
      {steps.map((step, i) => {
        const done = i < current;
        const active = i === current;
        const clickable = done && Boolean(onJump);
        return (
          <li key={step.id} className="flex flex-1 items-center gap-1">
            <button
              type="button"
              disabled={!clickable}
              onClick={() => clickable && onJump?.(i)}
              className={`group flex min-w-0 flex-1 items-center gap-2.5 rounded-lg px-2.5 py-2 text-left transition-colors duration-200 ${
                clickable ? "hover:bg-[color:var(--surface-2)]" : ""
              } ${clickable ? "cursor-pointer" : "cursor-default"}`}
            >
              <span
                className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-[11px] font-semibold"
                style={{
                  background: done
                    ? "#199e70"
                    : active
                      ? "#3987e5"
                      : "var(--surface-3)",
                  color: done || active ? "#fff" : "var(--text-tertiary)",
                  transition:
                    "background-color var(--dur) var(--ease-out), color var(--dur) var(--ease-out)",
                }}
              >
                {done ? "✓" : i + 1}
              </span>
              <span className="min-w-0">
                <span
                  className={`block truncate text-[13px] font-medium ${
                    active
                      ? "text-[color:var(--text-primary)]"
                      : "text-[color:var(--text-secondary)]"
                  }`}
                >
                  {step.label}
                </span>
                {step.hint && (
                  <span className="block truncate text-[11px] text-[color:var(--text-tertiary)]">
                    {step.hint}
                  </span>
                )}
              </span>
            </button>
            {i < steps.length - 1 && (
              <span
                aria-hidden
                className="h-px w-6 shrink-0"
                style={{
                  background: done ? "#199e70" : "var(--hairline-strong)",
                  transition: "background-color var(--dur) var(--ease-out)",
                }}
              />
            )}
          </li>
        );
      })}
    </ol>
  );
}
