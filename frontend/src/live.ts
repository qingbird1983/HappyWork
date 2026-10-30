// Live transcript state and permission merging, shared by App and tests.
import type { Message, PermissionPayload } from "./types";

export type Block =
  | { kind: "user"; text: string }
  | { kind: "assistant"; text: string }
  | {
      kind: "tool";
      id: string;
      name: string;
      toolRef?: string;
      status: string;
      trust?: "trusted" | "untrusted";
      argsSummary?: string;
      detail?: string;
      decision?: "allow" | "ask" | "deny";
      scope?: string;
      automatic?: boolean;
      quotaExceeded?: boolean;
    };

export interface LiveState {
  text: string;
  tools: Block[];
}

export const EMPTY_LIVE: LiveState = { text: "", tools: [] };

export function toolStatus(content: string): "ok" | "failed" {
  try {
    const parsed = JSON.parse(content) as { error?: string };
    return parsed.error ? "failed" : "ok";
  } catch {
    return "ok";
  }
}

export function transcriptToBlocks(msgs: Message[]): Block[] {
  const out: Block[] = [];
  for (const m of msgs) {
    if (m.role === "user") out.push({ kind: "user", text: m.content });
    else if (m.role === "assistant") out.push({ kind: "assistant", text: m.content });
    else if (m.role === "tool") {
      out.push({
        kind: "tool",
        id: m.id,
        name: m.toolName || "tool",
        status: toolStatus(m.content),
        trust: m.trust,
        detail: m.content,
      });
    }
  }
  return out;
}

// applyPermission attaches the engine verdict to the most recent live tool row;
// the running event always precedes the permission event (same actor goroutine).
export function applyPermission(s: LiveState, p: PermissionPayload): LiveState {
  for (let i = s.tools.length - 1; i >= 0; i--) {
    const t = s.tools[i];
    if (t.kind !== "tool") continue;
    if (t.toolRef !== p.toolRef && t.name !== p.name) continue;
    const tools = [...s.tools];
    tools[i] = {
      ...t,
      decision: p.decision,
      scope: p.scope,
      automatic: p.auto,
      quotaExceeded: p.quotaExceeded,
    };
    return { ...s, tools };
  }
  return s;
}
