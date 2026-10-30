import { useEffect, useRef, useState } from "react";
import type { ApprovalPayload } from "../types";
import { SafeText } from "../render";

export interface ApprovalDialogProps {
  payload: ApprovalPayload;
  onApprove: (scope: "once" | "session") => void;
  onDeny: (feedback: string) => void;
}

// ApprovalDialog follows research/03: label+description option pairs, explicit
// scope, preview, reversibility and "deny with feedback". Block-level requests
// require a typed phrase before any allow button activates.
export function ApprovalDialog({ payload, onApprove, onDeny }: ApprovalDialogProps) {
  const ref = useRef<HTMLDialogElement | null>(null);
  const [feedback, setFeedback] = useState("");
  const [phrase, setPhrase] = useState("");

  useEffect(() => {
    const dialog = ref.current;
    if (dialog && !dialog.open) {
      dialog.showModal();
    }
  }, [payload.approvalID]);

  const blockOK = payload.level !== "block" || phrase.trim() === "确认执行";

  return (
    <dialog
      ref={ref}
      className="approval-dialog"
      aria-labelledby="approval-title"
      onCancel={(e) => e.preventDefault()}
    >
      <h2 id="approval-title">
        <SafeText text={payload.title} />
      </h2>
      <p className="approval-summary">
        <SafeText text={payload.summary} />
      </p>
      <dl className="approval-fields">
        {payload.fields.map((field, i) => (
          <div key={i} className="approval-field">
            <dt>{field.label}</dt>
            <dd>
              <SafeText
                text={field.value}
                trust={payload.source.startsWith("mcp:") ? "untrusted" : undefined}
              />
            </dd>
          </div>
        ))}
      </dl>
      {payload.preview && (
        <div className="approval-preview">
          <div className="approval-preview-head">内容预览</div>
          <pre>
            <SafeText text={payload.preview} />
          </pre>
        </div>
      )}
      {payload.level === "block" && (
        <div className="approval-block">
          <label>
            该操作被标记为不可逆。输入“确认执行”以解锁：
            <input value={phrase} onChange={(e) => setPhrase(e.target.value)} />
          </label>
        </div>
      )}
      <div className="approval-options">
        {payload.scopeOptions.map((option) => (
          <button
            key={option.scope}
            className="approval-allow"
            disabled={!blockOK}
            onClick={() => onApprove(option.scope)}
          >
            <strong>{option.label}</strong>
            <span>{option.description}</span>
          </button>
        ))}
        <button className="approval-deny" onClick={() => onDeny("")}>
          <strong>拒绝</strong>
          <span>不执行，模型继续</span>
        </button>
        <button className="approval-deny" onClick={() => onDeny(feedback)}>
          <strong>拒绝并说明</strong>
          <span>不执行，并把意见回给模型</span>
        </button>
      </div>
      <label className="approval-feedback">
        给模型的说明（可选）
        <textarea
          value={feedback}
          placeholder="例如：先不要改动这个文件，改为给出修改建议"
          onChange={(e) => setFeedback(e.target.value)}
        />
      </label>
      <p className="approval-meta">
        来源：
        <SafeText text={payload.source} /> · 可逆性：
        {payload.reversible ? "是（覆盖前已保留副本）" : "未知"} · 有效期至{" "}
        {new Date(payload.expiresAt).toLocaleTimeString()}
      </p>
    </dialog>
  );
}
