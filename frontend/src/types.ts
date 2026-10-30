// Wire types shared by the UI. Field names mirror the Go JSON tags in
// internal/storage (design.md §5).

export interface Session {
  id: string;
  title: string;
  workspaceRoot: string;
  state: string;
  createdAt: string;
  updatedAt: string;
}

export interface Message {
  id: string;
  sessionId: string;
  seq: number;
  role: "user" | "assistant" | "tool" | "system";
  content: string;
  toolCallId?: string;
  toolName?: string;
  createdAt: string;
}

// Event payloads (internal/agent/types.go).
export interface StatePayload {
  sessionID: string;
  state: string;
}
export interface DeltaPayload {
  sessionID: string;
  text: string;
}
export interface ToolPayload {
  sessionID: string;
  name: string;
  status: "running" | "ok" | "failed";
  detail?: string;
}
export interface DonePayload {
  sessionID: string;
  finalState: string;
}
