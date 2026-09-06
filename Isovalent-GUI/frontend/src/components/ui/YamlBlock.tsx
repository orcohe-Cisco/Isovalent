"use client";

import { useState } from "react";

/**
 * Read-only YAML view with just enough highlighting to read structure at a
 * glance: comments, keys, list markers, scalars. Deliberately regex-based —
 * pulling in a syntax-highlighting bundle for a preview pane is not worth the
 * kilobytes on a page that already ships React Flow and Recharts.
 */
function highlight(line: string) {
  if (/^\s*#/.test(line)) {
    return <span style={{ color: "var(--text-tertiary)" }}>{line}</span>;
  }
  if (/^---\s*$/.test(line)) {
    return <span style={{ color: "#9085e9" }}>{line}</span>;
  }
  const m = line.match(/^(\s*)(-\s+)?([A-Za-z0-9_."'/\\[\]-]+)(:)(.*)$/);
  if (m) {
    const [, indent, dash, key, colon, rest] = m;
    return (
      <>
        {indent}
        {dash && <span style={{ color: "#6f6e68" }}>{dash}</span>}
        <span style={{ color: "#8fbdf3" }}>{key}</span>
        <span style={{ color: "#6f6e68" }}>{colon}</span>
        <span style={{ color: "#d8d7cf" }}>{rest}</span>
      </>
    );
  }
  const listItem = line.match(/^(\s*)(-\s+)(.*)$/);
  if (listItem) {
    const [, indent, dash, rest] = listItem;
    return (
      <>
        {indent}
        <span style={{ color: "#6f6e68" }}>{dash}</span>
        <span style={{ color: "#d8d7cf" }}>{rest}</span>
      </>
    );
  }
  return <span style={{ color: "#d8d7cf" }}>{line}</span>;
}

export function YamlBlock({
  yaml,
  filename,
  maxHeight = 460,
}: {
  yaml: string;
  filename?: string;
  maxHeight?: number;
}) {
  const [copied, setCopied] = useState(false);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(yaml);
      setCopied(true);
      setTimeout(() => setCopied(false), 1600);
    } catch {
      /* clipboard blocked — the text is selectable anyway */
    }
  };

  const download = () => {
    const blob = new Blob([yaml], { type: "text/yaml" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = filename ?? "manifest.yaml";
    a.click();
    URL.revokeObjectURL(url);
  };

  return (
    <div className="panel overflow-hidden">
      <div className="hairline-b flex items-center justify-between px-3 py-2">
        <span className="mono text-[11px] text-[color:var(--text-tertiary)]">
          {filename ?? "manifest.yaml"}
        </span>
        <div className="flex items-center gap-1">
          <button onClick={copy} className="btn btn-ghost px-2 py-1 text-[11px]">
            {copied ? "Copied" : "Copy"}
          </button>
          <button onClick={download} className="btn btn-ghost px-2 py-1 text-[11px]">
            Download
          </button>
        </div>
      </div>
      <pre
        className="mono overflow-auto px-4 py-3 text-[11.5px] leading-[1.6]"
        style={{ maxHeight, background: "var(--surface-0)" }}
      >
        {yaml.split("\n").map((line, i) => (
          <div key={i}>{highlight(line) || " "}</div>
        ))}
      </pre>
    </div>
  );
}
