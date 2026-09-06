"use client";

import { useEffect, type ReactNode } from "react";

/**
 * Right-hand detail drawer. Backdrop dims and blurs the page behind it, the
 * panel slides in on the long-tail ease, Escape closes. Deliberately not a
 * modal dialog element — we want the page underneath to stay legible.
 */
export function Sheet({
  open,
  onClose,
  title,
  subtitle,
  children,
  footer,
  width = 520,
}: {
  open: boolean;
  onClose: () => void;
  title: ReactNode;
  subtitle?: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
  width?: number;
}) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKey);
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = prev;
    };
  }, [open, onClose]);

  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50">
      <button
        aria-label="Close"
        onClick={onClose}
        className="animate-fade absolute inset-0 cursor-default"
        style={{
          background: "rgba(0,0,0,0.5)",
          backdropFilter: "blur(3px)",
          WebkitBackdropFilter: "blur(3px)",
        }}
      />
      <aside
        role="dialog"
        aria-modal="false"
        // Positioned rather than flex-stretched: a short panel used to collapse
        // to its content height and let the page show through underneath.
        className="animate-slide-in absolute inset-y-0 right-0 flex flex-col"
        style={{
          width: `min(${width}px, 100vw)`,
          background: "var(--surface-1)",
          borderLeft: "1px solid var(--hairline-strong)",
          boxShadow: "var(--shadow-3)",
        }}
      >
        <header className="hairline-b flex items-start justify-between gap-4 px-5 py-4">
          <div className="min-w-0">
            <h2 className="text-[15px] font-semibold">{title}</h2>
            {subtitle && (
              <p className="mt-0.5 text-xs text-[color:var(--text-secondary)]">
                {subtitle}
              </p>
            )}
          </div>
          <button
            onClick={onClose}
            className="btn btn-ghost -mr-2 -mt-1 px-2 py-1 text-base leading-none"
            aria-label="Close panel"
          >
            ✕
          </button>
        </header>
        <div className="flex-1 overflow-y-auto px-5 py-4">{children}</div>
        {footer && <div className="hairline-t px-5 py-3">{footer}</div>}
      </aside>
    </div>
  );
}
