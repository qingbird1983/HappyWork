import { useCallback, useEffect, useState } from "react";
import { api } from "../api";
import type { AuditExport, AuditPage, AuditRecord, AuditVerify, NoticePayload } from "../types";
import { SafeText } from "../render";

interface AuditPanelProps {
  sessionId: string;
  onNotice: (level: NoticePayload["level"], text: string) => void;
}

function chainLabel(verify: AuditVerify | undefined): string {
  if (!verify) return "正在读取审计…";
  if (!verify.ok) return "链状态：破损";
  if (verify.legacyRows > 0) return `链状态：完整（${verify.legacyRows} 条旧记录未密封）`;
  return "链状态：完整";
}

// AuditStatus is the integrity headline. Legacy rows are counted instead of
// pretending pre-Phase-4 history was sealed, and a broken chain is stated as
// broken with the first bad id.
export function AuditStatus({ verify }: { verify: AuditVerify | undefined }) {
  return (
    <>
      <div className="panel-status">
        <span className={verify && !verify.ok ? "chain broken" : "chain ok"}>
          <SafeText text={chainLabel(verify)} />
        </span>
        {verify && (
          <span>
            密封 {verify.sealedRows} 条 · 遗留 {verify.legacyRows} 条
          </span>
        )}
      </div>
      {verify && !verify.ok && (
        <div className="chain-banner" role="alert">
          <SafeText
            text={`第 ${verify.brokenId ?? 0} 条起中断：${verify.brokenReason || "原因未知"}`}
          />
          <span className="chain-detail">
            <SafeText
              text={`链头 ${verify.headHash || "（空）"} · 最后已检查 id ${verify.lastId}`}
            />
          </span>
          <SafeText text="导出 JSONL 与 manifest 后可离线核对。" />
        </div>
      )}
    </>
  );
}

// KNOWN_DECISIONS whitelists the class modifier so an unexpected decision value
// from storage can only render as "other", never as attacker-chosen classes.
const KNOWN_DECISIONS = new Set([
  "auto",
  "allow",
  "ask",
  "deny",
  "create",
  "hit",
  "delete",
  "restore",
  "undo_create",
  "prune",
  "quota",
  "verify_fail",
  "ok",
  "broken",
  "warn",
  "fail_closed",
  "patch",
  "unknown",
]);

function decisionClass(decision: string): string {
  return KNOWN_DECISIONS.has(decision) ? decision : "other";
}

// AuditList renders newest first; every field is text-only because reasons and
// details can carry tool or MCP content.
export function AuditList({ entries }: { entries: AuditRecord[] }) {
  return (
    <ul className="audit-list">
      {entries.map((row) => (
        <li key={row.id}>
          <span className="audit-time">{row.createdAt.slice(11, 19)}</span>
          <span className="audit-action">
            <SafeText text={row.action} />
          </span>
          <span className={`audit-decision ${decisionClass(row.decision)}`}>
            <SafeText text={row.decision} />
          </span>
          <span className="audit-reason">
            <SafeText text={row.reason} />
          </span>
        </li>
      ))}
      {entries.length === 0 && <li className="empty">本会话暂无审计记录。</li>}
    </ul>
  );
}

// AuditPanel is the local integrity view for one session.
export function AuditPanel({ sessionId, onNotice }: AuditPanelProps) {
  const [page, setPage] = useState<AuditPage | null>(null);
  const [exported, setExported] = useState<AuditExport | null>(null);
  const [busy, setBusy] = useState(false);

  const reload = useCallback(async () => {
    if (!sessionId) return;
    setBusy(true);
    try {
      setPage(await api.listAudit(sessionId, 100));
    } catch (e) {
      onNotice("error", `读取审计失败：${String(e)}`);
    } finally {
      setBusy(false);
    }
  }, [onNotice, sessionId]);

  // First fetch runs inside an async body so state only changes after a reply;
  // the App mounts this panel per selected session.
  useEffect(() => {
    if (!sessionId) return;
    let ignore = false;
    (async () => {
      try {
        const first = await api.listAudit(sessionId, 100);
        if (!ignore) setPage(first);
      } catch (e) {
        if (!ignore) onNotice("error", `读取审计失败：${String(e)}`);
      }
    })();
    return () => {
      ignore = true;
    };
  }, [onNotice, sessionId]);

  const verify = async () => {
    setBusy(true);
    try {
      const result = await api.verifyAudit();
      setPage((current) => (current ? { ...current, verify: result } : current));
      onNotice(
        result.ok ? "info" : "error",
        result.ok ? "审计链验证通过" : "审计链已破损，请导出证据",
      );
    } catch (e) {
      onNotice("error", `验证失败：${String(e)}`);
    } finally {
      setBusy(false);
    }
  };

  const exportEvidence = async () => {
    setBusy(true);
    try {
      const result = await api.exportAudit();
      setExported(result);
      onNotice("info", `已导出 ${result.rows} 条审计记录`);
    } catch (e) {
      onNotice("error", `导出失败：${String(e)}`);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="audit-panel">
      <AuditStatus verify={page?.verify} />
      <div className="panel-actions">
        <button disabled={busy} onClick={() => void reload()}>
          刷新
        </button>
        <button disabled={busy} onClick={() => void verify()}>
          验证整链
        </button>
        <button disabled={busy} onClick={() => void exportEvidence()}>
          导出 JSONL
        </button>
      </div>
      {exported && (
        <p className="exported">
          <span className="exported-label">证据文件（JSONL 与 manifest，不重新密封）</span>
          <SafeText className="exported-path" text={exported.jsonlPath} />
          <SafeText className="exported-path" text={exported.manifestPath} />
        </p>
      )}
      <AuditList entries={(page?.entries ?? []).slice().reverse()} />
      <p className="threat-note">
        本地完整性护栏：能发现改行、删行、插队和链头不一致；不防拥有数据库完整写权限、重算整条链的攻击者。
      </p>
    </div>
  );
}
