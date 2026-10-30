import type { PermissionRule, PresetInfo, Session } from "../types";
import { PermissionRules } from "./PermissionRules";
import { PresetPicker } from "./PresetPicker";

export const STATE_LABELS: Record<string, string> = {
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

export interface SessionSidebarProps {
  sessions: Session[];
  currentId: string;
  workspace: string;
  presets: PresetInfo[];
  preset: string;
  rules: PermissionRule[];
  onSelect: (id: string) => void;
  onWorkspaceChange: (value: string) => void;
  onCreate: () => void;
  onPresetChange: (name: string) => void;
  onDeleteRule: (id: string) => void;
}

// SessionSidebar lists sessions and the new-session controls (workspace,
// preset) plus the persistent rule panel.
export function SessionSidebar({
  sessions,
  currentId,
  workspace,
  presets,
  preset,
  rules,
  onSelect,
  onWorkspaceChange,
  onCreate,
  onPresetChange,
  onDeleteRule,
}: SessionSidebarProps) {
  return (
    <aside className="sidebar">
      <div className="sidebar-head">会话</div>
      <ul className="session-list">
        {sessions.map((s) => (
          <li key={s.id}>
            <button
              className={s.id === currentId ? "session active" : "session"}
              onClick={() => onSelect(s.id)}
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
          onChange={(e) => onWorkspaceChange(e.target.value)}
        />
        <button className="new-session" onClick={onCreate}>
          新建会话
        </button>
      </div>
      {presets.length > 0 && (
        <PresetPicker presets={presets} value={preset} onChange={onPresetChange} />
      )}
      <PermissionRules rules={rules} onDelete={onDeleteRule} />
    </aside>
  );
}
