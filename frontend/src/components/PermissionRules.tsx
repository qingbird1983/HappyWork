import { SafeText } from "../render";
import type { PermissionRule } from "../types";

export interface PermissionRulesProps {
  rules: PermissionRule[];
  onDelete: (id: string) => void;
}

function shortTool(ref: string): string {
  return ref.replace(/^builtin:\/\//, "").replace(/^mcp:\/\//, "mcp ");
}

// PermissionRules lists persistent grants so they can be reviewed and revoked.
// Deleting a rule takes effect for the next permission check.
export function PermissionRules({ rules, onDelete }: PermissionRulesProps) {
  return (
    <details className="rules-panel">
      <summary>权限规则（{rules.length}）</summary>
      {rules.length === 0 ? (
        <p className="rules-empty">没有持久规则。审批时选择“持久允许”并输入确认短语才会创建。</p>
      ) : (
        <ul className="rules-list">
          {rules.map((rule) => (
            <li key={rule.id}>
              <div className="rule-main">
                <span className="rule-tool">{shortTool(rule.toolRef)}</span>
                <span className="rule-scope">
                  <SafeText text={rule.matchKey} />
                </span>
                <span className="rule-uses">使用 {rule.useCount} 次</span>
              </div>
              <button className="rule-delete" onClick={() => onDelete(rule.id)}>
                撤销
              </button>
            </li>
          ))}
        </ul>
      )}
    </details>
  );
}
