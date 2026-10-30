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

export interface ToolBlockProps {
  name: string;
  status: string;
  trust?: "trusted" | "untrusted";
  argsSummary?: string;
  detail?: string;
}

export function ToolBlock({ name, status, trust, argsSummary, detail }: ToolBlockProps) {
  return (
    <div className="tool-block" data-trust={trust === "untrusted" ? "untrusted" : undefined}>
      <details>
        <summary>
          <span className="tool-name">{name}</span>
          <span className="tool-status">{STATUS_LABELS[status] ?? status}</span>
          <TrustBadge trust={trust} />
          {argsSummary && <span className="tool-args">{argsSummary}</span>}
        </summary>
        <pre className="tool-detail">
          <SafeText text={detail ?? "（暂无结果）"} trust={trust} />
        </pre>
      </details>
    </div>
  );
}
