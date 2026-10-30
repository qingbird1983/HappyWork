// Wire types shared by the UI. Field names mirror the Go JSON tags in
// internal/storage and internal/agent.

export interface Session {
  id: string;
  title: string;
  workspaceRoot: string;
  modelId?: string;
  preset?: string;
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
  hooksFingerprint?: string;
  hooksError?: string;
  presetsError?: string;
}

export interface PresetInfo {
  name: string;
  description: string;
  persistentAllowed: boolean;
}

export interface PermissionRule {
  id: string;
  toolRef: string;
  action: string;
  matchKey: string;
  decision: string;
  note: string;
  createdAt: string;
  lastUsedAt: string;
  useCount: number;
}

// Phase 4 audit and backup wire types (internal/audit, internal/rollback).
export interface AuditRecord {
  id: number;
  sessionId: string;
  turnId: number;
  traceId: string;
  action: string;
  decision: string;
  reason: string;
  detail: string;
  createdAt: string;
  prevHash: string;
  entryHash: string;
}

export interface AuditVerify {
  ok: boolean;
  sealedRows: number;
  legacyRows: number;
  firstSealedId?: number;
  lastId: number;
  headHash: string;
  brokenId?: number;
  brokenReason?: string;
  headMatch: boolean;
}

export interface AuditPage {
  entries: AuditRecord[];
  verify: AuditVerify;
  total: number;
}

export interface AuditExport {
  jsonlPath: string;
  manifestPath: string;
  rows: number;
  verify: AuditVerify;
}

export interface FileBackup {
  id: string;
  sessionId: string;
  turnId: number;
  relPath: string;
  kind: "overwrite" | "create";
  reason: string;
  version: number;
  mode: number;
  size: number;
  backupRel?: string;
  backupSha256?: string;
  writtenSha256?: string;
  discardedRel?: string;
  restoreOf?: string;
  createdAt: string;
  verifiedAt?: string;
  restoredAt?: string;
}

export interface BackupGroup {
  relPath: string;
  backups: FileBackup[];
}

export interface BackupRestore {
  backupId: string;
  relPath: string;
  restored: boolean;
  created?: boolean;
  alreadyAbsent?: boolean;
  safetyBackupId?: string;
  sha256?: string;
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
  scope: "once" | "session" | "persistent";
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
  contentTrust?: "trusted" | "untrusted";
  trustWarning?: string;
  quotaNotice?: string;
  warnings?: string[];
  preset?: string;
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

// session:permission carries the engine verdict for every tool call.
export interface PermissionPayload {
  sessionID: string;
  toolRef: string;
  name: string;
  action: string;
  decision: "allow" | "ask" | "deny";
  scope?: string;
  reason: string;
  auto: boolean;
  quotaExceeded: boolean;
  contentTrust: "trusted" | "untrusted";
  grantedBy?: string;
  preset: string;
}
