import { useCallback, useEffect, useRef, useState } from "react";
import { api, events } from "./api";
import type { DonePayload, Session } from "./types";

// Block is one grey transcript row. Model output is rendered as React text
// children (textContent), never HTML — the CSP only backstops this (A6).
type Block =
  | { kind: "user"; text: string }
  | { kind: "assistant"; text: string }
  | { kind: "tool"; text: string };

const STATE_LABELS: Record<string, string> = {
  idle: "空闲",
  thinking: "思考中…",
  running_tool: "执行工具…",
  completed: "已完成",
  error: "出错",
  cancelled: "已取消",
  interrupted: "上次中断",
};

function transcriptToBlocks(msgs: Awaited<ReturnType<typeof api.transcript>>): Block[] {
  const out: Block[] = [];
  for (const m of msgs) {
    if (m.role === "user") out.push({ kind: "user", text: m.content });
    else if (m.role === "assistant") out.push({ kind: "assistant", text: m.content });
    else if (m.role === "tool") out.push({ kind: "tool", text: `${m.toolName ?? "tool"} 完成` });
  }
  return out;
}

export default function App() {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [currentId, setCurrentId] = useState<string>("");
  const [blocks, setBlocks] = useState<Block[]>([]);
  const [liveText, setLiveText] = useState("");
  const [liveTools, setLiveTools] = useState<Block[]>([]);
  const [state, setState] = useState<string>("idle");
  const [input, setInput] = useState("");
  const [workspace, setWorkspace] = useState("");
  const [notice, setNotice] = useState("");

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

  // Initial load: pick the most recent session, if any.
  useEffect(() => {
    (async () => {
      try {
        const list = await reloadSessions();
        if (list.length > 0) {
          setCurrentId(list[0].id);
          setWorkspace(list[0].workspaceRoot);
          await loadTranscript(list[0].id);
          setState(list[0].state);
        }
      } catch (e) {
        setNotice(`加载会话失败：${String(e)}`);
      }
    })();
  }, [reloadSessions, loadTranscript]);

  // Live events for the selected session.
  useEffect(() => {
    const offs = [
      events.onDelta((p) => {
        if (p.sessionID === currentIdRef.current) setLiveText((t) => t + p.text);
      }),
      events.onState((p) => {
        if (p.sessionID === currentIdRef.current) setState(p.state);
      }),
      events.onTool((p) => {
        if (p.sessionID !== currentIdRef.current) return;
        setLiveTools((rows) =>
          p.status === "running"
            ? [...rows, { kind: "tool" as const, text: `${p.name} 执行中…` }]
            : rows.map((r, i) =>
                i === rows.length - 1
                  ? { ...r, text: `${p.name} ${p.status === "ok" ? "完成" : "失败"}` }
                  : r,
              ),
        );
      }),
      events.onDone((p: DonePayload) => {
        if (p.sessionID !== currentIdRef.current) return;
        setState(p.finalState);
        setLiveText("");
        setLiveTools([]);
        void loadTranscript(p.sessionID);
        void reloadSessions();
      }),
    ];
    return () => offs.forEach((off) => off());
  }, [loadTranscript, reloadSessions]);

  // Keep the transcript scrolled to the bottom.
  useEffect(() => {
    transcriptAnchor.current?.scrollIntoView({ block: "end" });
  }, [blocks, liveText, liveTools]);

  const createSession = async () => {
    const root = workspace.trim();
    if (!root) {
      setNotice("请先填写工作区目录");
      return;
    }
    try {
      const s = await api.createSession(root);
      setNotice("");
      await reloadSessions();
      setBlocks([]);
      setLiveText("");
      setLiveTools([]);
      setState(s.state);
      setCurrentId(s.id);
    } catch (e) {
      setNotice(`创建会话失败：${String(e)}`);
    }
  };

  const selectSession = async (id: string) => {
    setCurrentId(id);
    setLiveText("");
    setLiveTools([]);
    const sess = sessions.find((s) => s.id === id);
    if (sess) setWorkspace(sess.workspaceRoot);
    try {
      await loadTranscript(id);
      setState(sess?.state ?? "idle");
    } catch (e) {
      setNotice(`加载 transcript 失败：${String(e)}`);
    }
  };

  const send = async () => {
    const text = input.trim();
    if (!text || !currentId) return;
    setInput("");
    setBlocks((b) => [...b, { kind: "user", text }]);
    setLiveText("");
    try {
      await api.send(currentId, text);
    } catch (e) {
      setNotice(`发送失败：${String(e)}`);
    }
  };

  const cancel = async () => {
    if (!currentId) return;
    try {
      await api.cancel(currentId);
    } catch (e) {
      setNotice(`取消失败：${String(e)}`);
    }
  };

  const running = state === "thinking" || state === "running_tool";

  return (
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
        <div className="transcript" aria-live="polite">
          {blocks.length === 0 && !liveText && (
            <div className="empty-hint">
              没有会话或消息。填写一个存在的工作区目录并新建会话；mock 模型对包含 read_file / long /
              boom 的消息有专门剧本。
            </div>
          )}
          {blocks.map((b, i) =>
            b.kind === "tool" ? (
              <div key={i} className="block tool-row">
                {b.text}
              </div>
            ) : (
              <div key={i} className={`block ${b.kind}`}>
                {b.text}
              </div>
            ),
          )}
          {liveTools.map((t, i) => (
            <div key={`lt${i}`} className="block tool-row">
              {t.text}
            </div>
          ))}
          {liveText && (
            <div className="block assistant">
              {liveText}
              <span className="cursor">▍</span>
            </div>
          )}
          <div ref={transcriptAnchor} />
        </div>

        {notice && <div className="notice">{notice}</div>}

        <div className="composer">
          <textarea
            className="input"
            placeholder={currentId ? "输入消息…（Ctrl+Enter 发送）" : "先新建会话"}
            value={input}
            disabled={!currentId}
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
            {running ? (
              <button className="stop" onClick={() => void cancel()}>
                停止
              </button>
            ) : (
              <button
                className="send"
                disabled={!input.trim() || !currentId}
                onClick={() => void send()}
              >
                发送
              </button>
            )}
          </div>
        </div>
      </main>
    </div>
  );
}
