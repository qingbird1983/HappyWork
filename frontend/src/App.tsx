import { useCallback, useEffect, useRef, useState } from "react";
import { api, events } from "./api";
import type {
  AppInfo,
  ApprovalPayload,
  Message,
  NoticePayload,
  PausedPayload,
  Session,
  UsagePayload,
} from "./types";
import { SafeText } from "./render";
import { ApprovalDialog } from "./components/ApprovalDialog";
import { PausedBanner } from "./components/PausedBanner";
import { InterruptedBanner } from "./components/InterruptedBanner";
import { ToolBlock } from "./components/ToolBlock";

type Block =
  | { kind: "user"; text: string }
  | { kind: "assistant"; text: string }
  | {
      kind: "tool";
      id: string;
      name: string;
      status: string;
      trust?: "trusted" | "untrusted";
      argsSummary?: string;
      detail?: string;
    };

interface LiveState {
  text: string;
  tools: Block[];
}

const EMPTY_LIVE: LiveState = { text: "", tools: [] };

const STATE_LABELS: Record<string, string> = {
  idle: "空闲",
  thinking: "思考中…",
  running_tool: "执行工具…",
  persisting: "保存中…",
  awaiting_approval: "等待批准…",
  paused: "已暂停",
  completed: "已完成",
  error: "出错",
  cancelled: "已取消",
  interrupted: "上次中断",
};

function toolStatus(content: string): "ok" | "failed" {
  try {
    const parsed = JSON.parse(content) as { error?: string };
    return parsed.error ? "failed" : "ok";
  } catch {
    return "ok";
  }
}

function transcriptToBlocks(msgs: Message[]): Block[] {
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

export default function App() {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [currentId, setCurrentId] = useState<string>("");
  const [blocks, setBlocks] = useState<Block[]>([]);
  const [live, setLive] = useState<Record<string, LiveState>>({});
  const [state, setState] = useState<string>("idle");
  const [input, setInput] = useState("");
  const [workspace, setWorkspace] = useState("");
  const [appInfo, setAppInfo] = useState<AppInfo | null>(null);
  const [approvals, setApprovals] = useState<Record<string, ApprovalPayload>>({});
  const [paused, setPaused] = useState<Record<string, PausedPayload>>({});
  const [notices, setNotices] = useState<Record<string, NoticePayload>>({});
  const [usage, setUsage] = useState<Record<string, UsagePayload>>({});

  const currentIdRef = useRef("");
  useEffect(() => {
    currentIdRef.current = currentId;
  }, [currentId]);
  const transcriptAnchor = useRef<HTMLDivElement | null>(null);

  const reloadSessions = useCallback(async () => {
    const list = (await api.listSessions()) ?? [];
    setSessions(list);
    return list;
  }, []);

  const loadTranscript = useCallback(async (id: string) => {
    const msgs = (await api.transcript(id)) ?? [];
    setBlocks(transcriptToBlocks(msgs));
  }, []);

  const patchLive = useCallback((sessionId: string, fn: (s: LiveState) => LiveState) => {
    setLive((prev) => ({ ...prev, [sessionId]: fn(prev[sessionId] ?? EMPTY_LIVE) }));
  }, []);

  // Initial load: provider info and the most recent session.
  useEffect(() => {
    (async () => {
      try {
        setAppInfo(await api.appInfo());
      } catch {
        /* older backend: leave the bar empty */
      }
      try {
        const list = await reloadSessions();
        if (list.length > 0) {
          setCurrentId(list[0].id);
          setWorkspace(list[0].workspaceRoot);
          await loadTranscript(list[0].id);
          setState(list[0].state);
        }
      } catch (e) {
        setNotices((n) => ({
          ...n,
          [currentIdRef.current]: {
            sessionID: currentIdRef.current,
            level: "error",
            text: `加载会话失败：${String(e)}`,
          },
        }));
      }
    })();
  }, [reloadSessions, loadTranscript]);

  // Event subscriptions (all sessions; screens filter by id).
  useEffect(() => {
    const offs = [
      events.onDelta((p) => {
        patchLive(p.sessionID, (s) => ({ ...s, text: s.text + p.text }));
      }),
      events.onState((p) => {
        setState((cur) => (p.sessionID === currentIdRef.current ? p.state : cur));
        setSessions((list) =>
          list.map((s) => (s.id === p.sessionID ? { ...s, state: p.state } : s)),
        );
      }),
      events.onTool((p) => {
        patchLive(p.sessionID, (s) => {
          const id = p.toolCallID || `${p.name}-${s.tools.length}`;
          const idx = s.tools.findIndex((t) => t.kind === "tool" && t.id === id);
          if (p.status === "running") {
            if (idx >= 0) return s;
            const row: Block = {
              kind: "tool",
              id,
              name: p.name,
              status: "running",
              trust: p.trust,
              argsSummary: p.argsSummary,
            };
            return { ...s, tools: [...s.tools, row] };
          }
          if (idx >= 0) {
            const tools = [...s.tools];
            tools[idx] = {
              ...(tools[idx] as Extract<Block, { kind: "tool" }>),
              status: p.status,
              trust: p.trust,
              argsSummary: p.argsSummary,
              detail: p.detail,
            };
            return { ...s, tools };
          }
          return {
            ...s,
            tools: [
              ...s.tools,
              {
                kind: "tool",
                id,
                name: p.name,
                status: p.status,
                trust: p.trust,
                argsSummary: p.argsSummary,
                detail: p.detail,
              },
            ],
          };
        });
      }),
      events.onDone((p) => {
        setState((cur) => (p.sessionID === currentIdRef.current ? p.finalState : cur));
        setSessions((list) =>
          list.map((s) => (s.id === p.sessionID ? { ...s, state: p.finalState } : s)),
        );
        patchLive(p.sessionID, () => EMPTY_LIVE);
        setApprovals((prev) => {
          if (!prev[p.sessionID]) return prev;
          const next = { ...prev };
          delete next[p.sessionID];
          return next;
        });
        setPaused((prev) => {
          if (!prev[p.sessionID]) return prev;
          const next = { ...prev };
          delete next[p.sessionID];
          return next;
        });
        if (p.sessionID === currentIdRef.current) {
          void loadTranscript(p.sessionID);
        }
        void reloadSessions();
      }),
      events.onApprovalRequest((p) => {
        setApprovals((prev) => ({ ...prev, [p.sessionID]: p }));
      }),
      events.onApprovalResolved((p) => {
        setApprovals((prev) => {
          const current = prev[p.sessionID];
          if (!current || current.approvalID !== p.approvalID) return prev;
          const next = { ...prev };
          delete next[p.sessionID];
          return next;
        });
      }),
      events.onPaused((p) => {
        setPaused((prev) => ({ ...prev, [p.sessionID]: p }));
        setState((cur) => (p.sessionID === currentIdRef.current ? "paused" : cur));
        // The approval dialog expires with the request; Resume re-issues it.
        setApprovals((prev) => {
          if (!prev[p.sessionID]) return prev;
          const next = { ...prev };
          delete next[p.sessionID];
          return next;
        });
      }),
      events.onNotice((p) => {
        setNotices((prev) => ({ ...prev, [p.sessionID]: p }));
      }),
      events.onUsage((p) => {
        setUsage((prev) => ({ ...prev, [p.sessionID]: p }));
      }),
    ];
    return () => offs.forEach((off) => off());
  }, [loadTranscript, patchLive, reloadSessions]);

  useEffect(() => {
    transcriptAnchor.current?.scrollIntoView({ block: "end" });
  }, [blocks, live, currentId]);

  const currentLive = live[currentId] ?? EMPTY_LIVE;
  const currentApproval = approvals[currentId];
  const currentPaused = paused[currentId];
  const currentNotice = notices[currentId];
  const currentUsage = usage[currentId];
  const interrupted = state === "interrupted";
  const busy =
    state === "thinking" ||
    state === "running_tool" ||
    state === "persisting" ||
    state === "awaiting_approval";

  const notice = (level: NoticePayload["level"], text: string) => {
    if (!currentId) return;
    setNotices((prev) => ({ ...prev, [currentId]: { sessionID: currentId, level, text } }));
  };

  const createSession = async () => {
    const root = workspace.trim();
    if (!root) {
      notice("warn", "请先填写工作区目录");
      return;
    }
    try {
      const s = await api.createSession(root);
      setNotices((prev) => ({ ...prev, [s.id]: { sessionID: s.id, level: "info", text: "" } }));
      await reloadSessions();
      setBlocks([]);
      setLive((prev) => ({ ...prev, [s.id]: EMPTY_LIVE }));
      setState(s.state);
      setCurrentId(s.id);
    } catch (e) {
      notice("error", `创建会话失败：${String(e)}`);
    }
  };

  const selectSession = async (id: string) => {
    setCurrentId(id);
    const sess = sessions.find((s) => s.id === id);
    if (sess) setWorkspace(sess.workspaceRoot);
    try {
      await loadTranscript(id);
      setState(sess?.state ?? "idle");
    } catch (e) {
      notice("error", `加载 transcript 失败：${String(e)}`);
    }
  };

  const send = async () => {
    const text = input.trim();
    if (!text || !currentId) return;
    setInput("");
    setBlocks((b) => [...b, { kind: "user", text }]);
    setNotices((prev) => {
      const next = { ...prev };
      delete next[currentId];
      return next;
    });
    patchLive(currentId, (s) => ({ ...s, text: "" }));
    try {
      await api.send(currentId, text);
    } catch (e) {
      notice("error", `发送失败：${String(e)}`);
    }
  };

  const cancel = async () => {
    if (!currentId) return;
    try {
      await api.cancel(currentId);
    } catch (e) {
      notice("error", `取消失败：${String(e)}`);
    }
  };

  const approve = async (approval: ApprovalPayload, scope: "once" | "session") => {
    try {
      await api.approve(approval.sessionID, approval.approvalID, scope);
      setApprovals((prev) => {
        const next = { ...prev };
        delete next[approval.sessionID];
        return next;
      });
    } catch (e) {
      notice("error", `批准失败：${String(e)}`);
    }
  };

  const deny = async (approval: ApprovalPayload, feedback: string) => {
    try {
      await api.deny(approval.sessionID, approval.approvalID, feedback);
      setApprovals((prev) => {
        const next = { ...prev };
        delete next[approval.sessionID];
        return next;
      });
    } catch (e) {
      notice("error", `拒绝失败：${String(e)}`);
    }
  };

  const resume = async () => {
    if (!currentId) return;
    setPaused((prev) => {
      const next = { ...prev };
      delete next[currentId];
      return next;
    });
    try {
      await api.resume(currentId);
    } catch (e) {
      notice("error", `继续失败：${String(e)}`);
    }
  };

  const discard = async () => {
    if (!currentId) return;
    try {
      await api.discard(currentId);
    } catch (e) {
      notice("error", `放弃失败：${String(e)}`);
    }
  };

  return (
    <div className="shell">
      <header className="topbar">
        <span className="brand">HappyWork</span>
        {appInfo && (
          <>
            <span className="provider">
              {appInfo.provider} / {appInfo.model}
            </span>
            <span className={appInfo.mock ? "destination mock" : "destination cloud"}>
              <SafeText text={appInfo.dataDestination} />
            </span>
            {appInfo.mcpServers > 0 && (
              <span className="mcp-status">MCP × {appInfo.mcpServers}</span>
            )}
            {appInfo.mcpError && (
              <span className="mcp-status error" title={appInfo.mcpError}>
                MCP 异常
              </span>
            )}
          </>
        )}
        {currentUsage && (
          <span className="usage">
            {currentUsage.totalTokens.toLocaleString()} tokens · ${currentUsage.costUSD.toFixed(4)}
            {currentUsage.estimated ? "（估算）" : ""}
          </span>
        )}
      </header>

      <div className="app">
        <aside className="sidebar">
          <div className="sidebar-head">会话</div>
          <ul className="session-list">
            {sessions.map((s) => (
              <li key={s.id}>
                <button
                  className={s.id === currentId ? "session active" : "session"}
                  onClick={() => void selectSession(s.id)}
                  title={s.workspaceRoot}
                >
                  <span className="session-title">{s.title || "(untitled)"}</span>
                  <span className="session-state">{STATE_LABELS[s.state] ?? s.state}</span>
                </button>
              </li>
            ))}
          </ul>
          <div className="workspace-row">
            <input
              className="workspace-input"
              placeholder="工作区目录（绝对路径）"
              value={workspace}
              onChange={(e) => setWorkspace(e.target.value)}
            />
            <button className="new-session" onClick={() => void createSession()}>
              新建会话
            </button>
          </div>
        </aside>

        <main className="main">
          {interrupted && (
            <InterruptedBanner onResume={() => void resume()} onDiscard={() => void discard()} />
          )}
          {currentPaused && (
            <PausedBanner
              paused={currentPaused}
              onResume={() => void resume()}
              onStop={() => void cancel()}
            />
          )}
          {currentNotice && currentNotice.text && (
            <div className={`notice ${currentNotice.level}`}>
              <SafeText text={currentNotice.text} />
            </div>
          )}

          <div className="transcript" aria-live="polite">
            {blocks.length === 0 && !currentLive.text && currentLive.tools.length === 0 && (
              <div className="empty-hint">
                没有会话或消息。填写一个存在的工作区目录并新建会话；写文件会先弹出审批。
              </div>
            )}
            {blocks.map((b, i) =>
              b.kind === "tool" ? (
                <ToolBlock
                  key={`${b.id}-${i}`}
                  name={b.name}
                  status={b.status}
                  trust={b.trust}
                  argsSummary={b.argsSummary}
                  detail={b.detail}
                />
              ) : (
                <div key={`${b.kind}-${i}`} className={`block ${b.kind}`}>
                  <SafeText text={b.text} />
                </div>
              ),
            )}
            {currentLive.tools.map((t, i) =>
              t.kind === "tool" ? (
                <ToolBlock
                  key={`${t.id}-${i}`}
                  name={t.name}
                  status={t.status}
                  trust={t.trust}
                  argsSummary={t.argsSummary}
                  detail={t.detail}
                />
              ) : null,
            )}
            {currentLive.text && (
              <div className="block assistant">
                <SafeText text={currentLive.text} />
                <span className="cursor">▍</span>
              </div>
            )}
            <div ref={transcriptAnchor} />
          </div>

          <div className="composer">
            <textarea
              className="input"
              placeholder={
                !currentId
                  ? "先新建会话"
                  : currentApproval
                    ? "请在弹窗中批准或拒绝当前操作"
                    : "输入消息…（Ctrl+Enter 发送）"
              }
              value={input}
              disabled={!currentId || !!currentApproval || !!currentPaused || interrupted}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
                  e.preventDefault();
                  void send();
                }
              }}
            />
            <div className="composer-actions">
              <span className="state-label">{STATE_LABELS[state] ?? state}</span>
              {busy ? (
                <button className="stop" onClick={() => void cancel()}>
                  停止
                </button>
              ) : (
                <button
                  className="send"
                  disabled={!input.trim() || !currentId || !!currentPaused || interrupted}
                  onClick={() => void send()}
                >
                  发送
                </button>
              )}
            </div>
          </div>
        </main>
      </div>

      {currentApproval && (
        <ApprovalDialog
          key={currentApproval.approvalID}
          payload={currentApproval}
          onApprove={(scope) => void approve(currentApproval, scope)}
          onDeny={(feedback) => void deny(currentApproval, feedback)}
        />
      )}
    </div>
  );
}
