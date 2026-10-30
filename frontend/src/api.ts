// Backend calls via Wails runtime Call.ByName. Names are collected here so a
// typo fails in one place (design.md §14).
import { Call, Events } from "@wailsio/runtime";
import type {
  AppInfo,
  ApprovalPayload,
  ApprovalResolvedPayload,
  AuditExport,
  AuditPage,
  AuditVerify,
  BackupGroup,
  BackupRestore,
  DeltaPayload,
  DonePayload,
  Message,
  NoticePayload,
  PausedPayload,
  PermissionPayload,
  PermissionRule,
  PresetInfo,
  Session,
  StatePayload,
  ToolPayload,
  UsagePayload,
} from "./types";

// Wails v3 qualifies bound methods as <full import path>.<Struct>.<Method>
// (pkg/application/bindings.go fqn) — not the short package name.
const SVC = "github.com/happywork/happywork/internal/desktop.SessionService";

export const api = {
  listSessions: () => Call.ByName(`${SVC}.ListSessions`) as Promise<Session[]>,
  createSession: (workspaceRoot: string, preset: string) =>
    Call.ByName(`${SVC}.CreateSession`, workspaceRoot, preset) as Promise<Session>,
  listPresets: () => Call.ByName(`${SVC}.ListPresets`) as Promise<PresetInfo[]>,
  listRules: () => Call.ByName(`${SVC}.ListRules`) as Promise<PermissionRule[]>,
  deleteRule: (id: string) => Call.ByName(`${SVC}.DeleteRule`, id) as Promise<void>,
  send: (sessionId: string, text: string) =>
    Call.ByName(`${SVC}.Send`, sessionId, text) as Promise<void>,
  cancel: (sessionId: string) => Call.ByName(`${SVC}.Cancel`, sessionId) as Promise<void>,
  approve: (sessionId: string, approvalId: string, scope: string, phrase: string) =>
    Call.ByName(`${SVC}.Approve`, sessionId, approvalId, scope, phrase) as Promise<void>,
  deny: (sessionId: string, approvalId: string, feedback: string) =>
    Call.ByName(`${SVC}.Deny`, sessionId, approvalId, feedback) as Promise<void>,
  resume: (sessionId: string) => Call.ByName(`${SVC}.Resume`, sessionId) as Promise<void>,
  discard: (sessionId: string) => Call.ByName(`${SVC}.Discard`, sessionId) as Promise<void>,
  appInfo: () => Call.ByName(`${SVC}.AppInfo`) as Promise<AppInfo>,
  transcript: (sessionId: string) =>
    Call.ByName(`${SVC}.Transcript`, sessionId) as Promise<Message[]>,
  listAudit: (sessionId: string, limit: number) =>
    Call.ByName(`${SVC}.ListAudit`, sessionId, limit) as Promise<AuditPage>,
  verifyAudit: () => Call.ByName(`${SVC}.VerifyAudit`) as Promise<AuditVerify>,
  exportAudit: () => Call.ByName(`${SVC}.ExportAudit`) as Promise<AuditExport>,
  listBackups: (sessionId: string) =>
    Call.ByName(`${SVC}.ListBackups`, sessionId) as Promise<BackupGroup[]>,
  restoreBackup: (sessionId: string, backupId: string) =>
    Call.ByName(`${SVC}.RestoreBackup`, sessionId, backupId) as Promise<BackupRestore>,
  undoCreatedFile: (sessionId: string, backupId: string) =>
    Call.ByName(`${SVC}.UndoCreatedFile`, sessionId, backupId) as Promise<BackupRestore>,
};

export const events = {
  onState: (fn: (p: StatePayload) => void) =>
    Events.On("session:state", (ev) => fn(ev.data as StatePayload)),
  onDelta: (fn: (p: DeltaPayload) => void) =>
    Events.On("session:delta", (ev) => fn(ev.data as DeltaPayload)),
  onTool: (fn: (p: ToolPayload) => void) =>
    Events.On("session:tool", (ev) => fn(ev.data as ToolPayload)),
  onDone: (fn: (p: DonePayload) => void) =>
    Events.On("session:done", (ev) => fn(ev.data as DonePayload)),
  onApprovalRequest: (fn: (p: ApprovalPayload) => void) =>
    Events.On("approval:request", (ev) => fn(ev.data as ApprovalPayload)),
  onApprovalResolved: (fn: (p: ApprovalResolvedPayload) => void) =>
    Events.On("approval:resolved", (ev) => fn(ev.data as ApprovalResolvedPayload)),
  onPaused: (fn: (p: PausedPayload) => void) =>
    Events.On("session:paused", (ev) => fn(ev.data as PausedPayload)),
  onNotice: (fn: (p: NoticePayload) => void) =>
    Events.On("session:notice", (ev) => fn(ev.data as NoticePayload)),
  onUsage: (fn: (p: UsagePayload) => void) =>
    Events.On("session:usage", (ev) => fn(ev.data as UsagePayload)),
  onPermission: (fn: (p: PermissionPayload) => void) =>
    Events.On("session:permission", (ev) => fn(ev.data as PermissionPayload)),
};
