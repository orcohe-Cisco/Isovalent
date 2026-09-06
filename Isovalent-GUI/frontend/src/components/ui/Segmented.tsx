"use client";

import type { ReactNode } from "react";

export interface SegmentedOption<T extends string> {
  value: T;
  label: ReactNode;
  /** Colour of the sliding indicator when this option is active. */
  tone?: "neutral" | "accent" | "warn" | "danger";
  title?: string;
}

const TONE_BG: Record<NonNullable<SegmentedOption<string>["tone"]>, string> = {
  neutral: "rgba(255,255,255,0.14)",
  accent: "#3987e5",
  warn: "#c98500",
  danger: "#e66767",
};

/**
 * A segmented control with a single indicator that slides between options,
 * rather than three buttons that each light up. The moving element is what
 * makes the change of state feel physical instead of instantaneous.
 */
export function Segmented<T extends string>({
  value,
  options,
  onChange,
  size = "md",
  disabled = false,
  ariaLabel,
}: {
  value: T;
  options: SegmentedOption<T>[];
  onChange: (next: T) => void;
  size?: "sm" | "md";
  disabled?: boolean;
  ariaLabel?: string;
}) {
  const index = Math.max(
    0,
    options.findIndex((o) => o.value === value),
  );
  const active = options[index];
  const pct = 100 / options.length;

  return (
    <div
      role="radiogroup"
      aria-label={ariaLabel}
      className={`relative inline-flex select-none rounded-lg p-0.5 ${
        disabled ? "opacity-50" : ""
      }`}
      style={{
        background: "var(--surface-2)",
        border: "1px solid var(--hairline)",
      }}
    >
      <span
        aria-hidden
        className="absolute inset-y-0.5 rounded-[7px]"
        style={{
          width: `calc(${pct}% - 4px)`,
          left: 2,
          transform: `translateX(calc(${index} * (100% + 4px)))`,
          background: TONE_BG[active?.tone ?? "neutral"],
          transition:
            "transform var(--dur) var(--ease-out), background-color var(--dur) var(--ease-out)",
          boxShadow: "0 1px 2px rgba(0,0,0,0.35)",
        }}
      />
      {options.map((o) => {
        const isActive = o.value === value;
        return (
          <button
            key={o.value}
            type="button"
            role="radio"
            aria-checked={isActive}
            title={o.title}
            disabled={disabled}
            onClick={(e) => {
              e.stopPropagation();
              if (!isActive) onChange(o.value);
            }}
            className={`relative z-10 whitespace-nowrap rounded-[7px] font-medium transition-colors duration-150 ${
              size === "sm" ? "px-2.5 py-1 text-[11px]" : "px-3 py-1.5 text-xs"
            } ${
              isActive
                ? o.tone === "neutral" || !o.tone
                  ? "text-white"
                  : "text-white"
                : "text-[color:var(--text-secondary)] hover:text-[color:var(--text-primary)]"
            }`}
            style={{ flex: "1 1 0%" }}
          >
            {o.label}
          </button>
        );
      })}
    </div>
  );
}
