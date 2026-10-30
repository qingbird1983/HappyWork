import { useEffect, useRef, useState } from "react";
import type { ApprovalPayload } from "../types";
import { SafeText } from "../render";

export type ApprovalScope = "once" | "session" | "persistent";

export interface ApprovalDialogProps {
  payload: ApprovalPayload;
  onApprove: (scope: ApprovalScope, phrase: string) => void;
  onDeny: (feedback: string) => void;
}

// ApprovalDialog follows research/03: label+description option pairs, explicit
// scope, preview, reversibility and "deny with feedback". Block-level requests
// require a typed phrase before any allow button activates; persistent rules
// require the separate "允许持久" phrase and are only offered by the backend.
export function ApprovalDialog({ payload, onApprove, onDeny }: ApprovalDialogProps) {
  const ref = useRef<HTMLDialogElement | null>(null);
  const [feedback, setFeedback] = useState("");
  const [blockPhrase, setBlockPhrase] = useState("");
  const [persistPhrase, setPersistPhrase] = useState("");
  const [persistentSelected, setPersistentSelected] = useState(false);

  useEffect(() => {
    const dialog = ref.current;
    if (dialog && !dialog.open) {
      dialog.showModal();
    }
  }, [payload.approvalID]);

  const blockOK = payload.level !== "block" || blockPhrase.trim() === "确认执行";
  const persistOK = !persistentSelected || persistPhrase.trim() === "允许持久";

  const choose = (scope: ApprovalScope) => {
    if (scope === "persistent") {
      if (!blockOK) return;
      setPersistentSelected(true);
      return;
    }
    if (!blockOK) return;
    onApprove(scope, blockPhrase);
  };

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
      {payload.trustWarning && (
        <div className="approval-warning trust">
          <SafeText text={payload.trustWarning} />
        </div>
      )}
      {payload.quotaNotice && (
        <div className="approval-warning quota">
          <SafeText text={payload.quotaNotice} />
        </div>
      )}
      {payload.warnings?.map((warning, i) => (
        <div className="approval-warning hook" key={i}>
          <SafeText text={warning} />
        </div>
      ))}
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
            该操作被标记为不可逆或高风险。输入“确认执行”以解锁：
            <input value={blockPhrase} onChange={(e) => setBlockPhrase(e.target.value)} />
          </label>
        </div>
      )}
      <div className="approval-options">
        {payload.scopeOptions.map((option) => {
          const active = option.scope === "persistent" && persistentSelected;
          return (
            <button
              key={option.scope}
              className={active ? "approval-allow active" : "approval-allow"}
              disabled={!blockOK}
              onClick={() => choose(option.scope)}
            >
              <strong>{option.label}</strong>
              <span>{option.description}</span>
            </button>
          );
        })}
        <button className="approval-deny" onClick={() => onDeny("")}>
          <strong>拒绝</strong>
          <span>不执行，模型继续</span>
        </button>
        <button className="approval-deny" onClick={() => onDeny(feedback)}>
          <strong>拒绝并说明</strong>
          <span>不执行，并把意见回给模型</span>
        </button>
      </div>
      {persistentSelected && (
        <div className="approval-persistent">
          <label>
            创建持久规则会在本机所有会话生效。输入“允许持久”确认：
            <input
              value={persistPhrase}
              placeholder="允许持久"
              onChange={(e) => setPersistPhrase(e.target.value)}
            />
          </label>
          <button
            className="approval-allow confirm"
            disabled={!blockOK || !persistOK}
            onClick={() => onApprove("persistent", persistPhrase)}
          >
            <strong>确认持久允许</strong>
            <span>可在权限规则面板中撤销</span>
          </button>
        </div>
      )}
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
        {payload.reversible ? "是（覆盖前已保留副本）" : "未知"}
        {payload.preset ? ` · 预设：${payload.preset}` : ""} · 有效期至{" "}
        {new Date(payload.expiresAt).toLocaleTimeString()}
      </p>
    </dialog>
  );
}
