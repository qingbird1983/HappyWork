import { SafeText } from "../render";

export function TrustBadge({ trust }: { trust?: "trusted" | "untrusted" }) {
  if (trust !== "untrusted") return null;
  return (
    <span className="trust-badge" data-trust="untrusted">
      外部内容 · 不可信
    </span>
  );
}

const STATUS_LABELS: Record<string, string> = {
  running: "执行中…",
  ok: "完成",
  failed: "失败",
  denied: "已拒绝",
  unknown: "结果未知",
};

const DECISION_LABELS: Record<string, string> = {
  allow: "自动放行",
  ask: "需审批",
  deny: "拒绝",
};

export interface ToolBlockProps {
  name: string;
  status: string;
  trust?: "trusted" | "untrusted";
  argsSummary?: string;
  detail?: string;
  decision?: "allow" | "ask" | "deny";
  scope?: string;
  automatic?: boolean;
  quotaExceeded?: boolean;
}

// ToolBlock renders one tool call. All fields are untrusted data and go through
// SafeText; badges only state engine facts.
export function ToolBlock({
  name,
  status,
  trust,
  argsSummary,
  detail,
  decision,
  scope,
  automatic,
  quotaExceeded,
}: ToolBlockProps) {
  return (
    <div className="tool-block" data-trust={trust === "untrusted" ? "untrusted" : undefined}>
      <details>
        <summary>
          <span className="tool-name">{name}</span>
          <span className="tool-status">{STATUS_LABELS[status] ?? status}</span>
          <TrustBadge trust={trust} />
          {decision && (
            <span className={`decision-badge ${decision}`}>
              {DECISION_LABELS[decision] ?? decision}
              {automatic ? " · 无弹窗" : ""}
              {quotaExceeded ? " · 限额" : ""}
              {scope && scope !== "none" ? ` · ${scope}` : ""}
            </span>
          )}
          {argsSummary && <span className="tool-args">{argsSummary}</span>}
        </summary>
        <pre className="tool-detail">
          <SafeText text={detail ?? "（暂无结果）"} trust={trust} />
        </pre>
      </details>
    </div>
  );
}
