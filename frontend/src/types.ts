// Wire types shared by the UI. Field names mirror the Go JSON tags in
// internal/storage and internal/agent.

export interface Session {
  id: string;
  title: string;
  workspaceRoot: string;
  modelId?: string;
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
  toolCallsJson?: string;
  turnId?: number;
  trust?: "trusted" | "untrusted";
  createdAt: string;
}

export interface AppInfo {
  provider: string;
  model: string;
  mock: boolean;
  dataDestination: string;
  mcpServers: number;
  mcpError?: string;
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
  toolCallID?: string;
  name: string;
  toolRef?: string;
  trust?: "trusted" | "untrusted";
  argsSummary?: string;
  status: "running" | "ok" | "failed" | "denied" | "unknown";
  detail?: string;
}
export interface DonePayload {
  sessionID: string;
  finalState: string;
  detail?: string;
}

export interface ApprovalField {
  label: string;
  value: string;
}
export interface ApprovalOption {
  scope: "once" | "session";
  label: string;
  description: string;
}
export interface ApprovalPayload {
  approvalID: string;
  sessionID: string;
  toolCallID: string;
  toolName: string;
  toolRef: string;
  level: "confirm" | "block";
  title: string;
  summary: string;
  fields: ApprovalField[];
  preview?: string;
  scopeOptions: ApprovalOption[];
  source: string;
  reversible: boolean;
  expiresAt: string;
}
export interface ApprovalResolvedPayload {
  sessionID: string;
  approvalID: string;
  decision: string;
  scope: string;
}
export interface PausedPayload {
  sessionID: string;
  reason: "approval_timeout" | "budget";
  detail: string;
}
export interface NoticePayload {
  sessionID: string;
  level: "info" | "warn" | "error";
  text: string;
}
export interface UsagePayload {
  sessionID: string;
  model: string;
  turnTokens: number;
  totalTokens: number;
  costUSD: number;
  estimated: boolean;
}
