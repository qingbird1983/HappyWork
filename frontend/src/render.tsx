// Rendering safety (A6): every model/MCP/tool string goes through a React text
// node. There is deliberately no Markdown, no HTML injection and no link
// generation — a URL stays text, so javascript:/data: click paths cannot exist.
import type { ReactNode } from "react";

export interface SafeTextProps {
  text: string;
  trust?: "trusted" | "untrusted";
  className?: string;
}

// SafeText renders untrusted content as escaped text.
export function SafeText({ text, trust, className }: SafeTextProps): ReactNode {
  return (
    <span className={className} data-trust={trust === "untrusted" ? "untrusted" : undefined}>
      {text}
    </span>
  );
}

// plainText normalizes non-string payloads to a string without invoking HTML.
export function plainText(value: unknown): string {
  if (typeof value === "string") return value;
  if (value === null || value === undefined) return "";
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}
