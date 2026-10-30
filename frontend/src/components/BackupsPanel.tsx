import { useCallback, useEffect, useState } from "react";
import { api } from "../api";
import type { BackupGroup, FileBackup, NoticePayload } from "../types";
import { SafeText } from "../render";

interface BackupsPanelProps {
  sessionId: string;
  busy: boolean;
  onNotice: (level: NoticePayload["level"], text: string) => void;
}

export type PendingAction = { mode: "restore" | "undo"; backup: FileBackup };

function formatBytes(value: number): string {
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KiB`;
  return `${(value / (1024 * 1024)).toFixed(1)} MiB`;
}

// backupLabel describes whether a version is usable. An overwrite without
// verified_at never passed the post-write hash check, so it is not offered.
function backupLabel(backup: FileBackup): string {
  if (backup.restoredAt) return "已恢复/已撤销";
  if (backup.kind === "create" && !backup.writtenSha256) return "未完成";
  if (backup.kind === "overwrite" && !backup.verifiedAt) return "未确认";
  return "可用";
}

export function canRestore(backup: FileBackup): boolean {
  return (
    backup.kind === "overwrite" && !!backup.backupRel && !!backup.verifiedAt && !backup.restoredAt
  );
}

export function canUndo(backup: FileBackup): boolean {
  return backup.kind === "create" && !!backup.writtenSha256 && !backup.restoredAt;
}

// BackupList groups versions per relative path. Buttons are disabled while the
// session is running because restore/undo must not race with tool execution.
export function BackupList({
  groups,
  busy,
  onPick,
}: {
  groups: BackupGroup[];
  busy: boolean;
  onPick: (action: PendingAction) => void;
}) {
  return (
    <ul className="backup-list">
      {groups.map((group) => (
        <li key={group.relPath} className="backup-file">
          <h4>
            <SafeText text={group.relPath} />
          </h4>
          <ul>
            {group.backups.map((backup) => (
              <li key={backup.id}>
                <span>v{backup.version}</span>
                <span>{backup.kind === "create" ? "新建" : "覆盖"}</span>
                <span>{formatBytes(backup.size)}</span>
                <span>{backup.createdAt.slice(11, 19)}</span>
                <span className="backup-state">
                  <SafeText text={backupLabel(backup)} />
                </span>
                {canRestore(backup) && (
                  <button disabled={busy} onClick={() => onPick({ mode: "restore", backup })}>
                    恢复
                  </button>
                )}
                {canUndo(backup) && (
                  <button disabled={busy} onClick={() => onPick({ mode: "undo", backup })}>
                    撤销创建
                  </button>
                )}
              </li>
            ))}
          </ul>
        </li>
      ))}
      {groups.length === 0 && <li className="empty">本会话还没有文件备份。</li>}
    </ul>
  );
}

// BackupConfirm states the side effect before it happens: restoring first turns
// the current content into a new version, undo moves the file into the backup
// area instead of deleting it.
export function BackupConfirm({ pending }: { pending: PendingAction }) {
  return (
    <div className="backup-confirm">
      <SafeText
        text={
          pending.mode === "restore"
            ? `恢复 ${pending.backup.relPath} v${pending.backup.version}？当前内容会先成为新版本。`
            : `撤销创建 ${pending.backup.relPath} v${pending.backup.version}？内容将移入备份区，不会永久删除。`
        }
      />
    </div>
  );
}

// BackupsPanel is the version history and restore entry point.
export function BackupsPanel({ sessionId, busy, onNotice }: BackupsPanelProps) {
  const [groups, setGroups] = useState<BackupGroup[]>([]);
  const [pending, setPending] = useState<PendingAction | null>(null);
  const [loading, setLoading] = useState(false);

  const reload = useCallback(async () => {
    if (!sessionId) return;
    setLoading(true);
    try {
      setGroups((await api.listBackups(sessionId)) ?? []);
    } catch (e) {
      onNotice("error", `读取备份失败：${String(e)}`);
    } finally {
      setLoading(false);
    }
  }, [onNotice, sessionId]);

  // Initial load happens in an async body; switching sessions remounts the panel.
  useEffect(() => {
    if (!sessionId) return;
    let ignore = false;
    (async () => {
      try {
        const first = (await api.listBackups(sessionId)) ?? [];
        if (!ignore) setGroups(first);
      } catch (e) {
        if (!ignore) onNotice("error", `读取备份失败：${String(e)}`);
      }
    })();
    return () => {
      ignore = true;
    };
  }, [onNotice, sessionId]);

  const confirm = async () => {
    if (!pending || !sessionId) return;
    setLoading(true);
    try {
      if (pending.mode === "restore") {
        await api.restoreBackup(sessionId, pending.backup.id);
        onNotice("info", `已恢复 ${pending.backup.relPath} v${pending.backup.version}`);
      } else {
        await api.undoCreatedFile(sessionId, pending.backup.id);
        onNotice("info", `已撤销创建 ${pending.backup.relPath}，文件移入备份区`);
      }
      setPending(null);
      await reload();
    } catch (e) {
      onNotice("error", `操作失败：${String(e)}`);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="backups-panel">
      <div className="panel-status">
        <span>{groups.length} 个文件有备份</span>
        <button disabled={loading} onClick={() => void reload()}>
          刷新
        </button>
      </div>
      {busy && <p className="backup-busy">会话正在运行；请先停止，再恢复或撤销创建。</p>}
      {pending && (
        <>
          <BackupConfirm pending={pending} />
          <div className="panel-actions">
            <button disabled={loading || busy} onClick={() => void confirm()}>
              确认
            </button>
            <button disabled={loading} onClick={() => setPending(null)}>
              取消
            </button>
          </div>
        </>
      )}
      <BackupList groups={groups} busy={busy} onPick={setPending} />
      <p className="threat-note">
        每个文件保留最近 20 个版本；备份区接近 512 MiB
        软上限时新的覆盖写会被拒绝，而不是悄悄丢版本。
      </p>
    </div>
  );
}
