// Backend calls via Wails runtime Call.ByName. Names are collected here so a
// typo fails in one place (design.md §14).
import { Call, Events } from "@wailsio/runtime";
import type {
  DeltaPayload,
  DonePayload,
  Message,
  Session,
  StatePayload,
  ToolPayload,
} from "./types";

// Wails v3 qualifies bound methods as <full import path>.<Struct>.<Method>
// (pkg/application/bindings.go fqn) — not the short package name.
const SVC = "github.com/happywork/happywork/internal/desktop.SessionService";

export const api = {
  listSessions: () => Call.ByName(`${SVC}.ListSessions`) as Promise<Session[]>,
  createSession: (workspaceRoot: string) =>
    Call.ByName(`${SVC}.CreateSession`, workspaceRoot) as Promise<Session>,
  send: (sessionId: string, text: string) =>
    Call.ByName(`${SVC}.Send`, sessionId, text) as Promise<void>,
  cancel: (sessionId: string) => Call.ByName(`${SVC}.Cancel`, sessionId) as Promise<void>,
  transcript: (sessionId: string) =>
    Call.ByName(`${SVC}.Transcript`, sessionId) as Promise<Message[]>,
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
};
