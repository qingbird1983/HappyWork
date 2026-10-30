import { useState } from "react";
import type { NoticePayload } from "../types";
import { AuditPanel } from "./AuditPanel";
import { BackupsPanel } from "./BackupsPanel";

interface SecurityPanelProps {
  sessionId: string;
  busy: boolean;
  onClose: () => void;
  onNotice: (level: NoticePayload["level"], text: string) => void;
}

type Tab = "audit" | "backups";

// SecurityPanel is the minimal Phase 4 dock: audit chain plus versioned backup
// controls, rendered as text-only panels behind the same CSP rules.
export function SecurityPanel({ sessionId, busy, onClose, onNotice }: SecurityPanelProps) {
  const [tab, setTab] = useState<Tab>("audit");
  return (
    <aside className="security-panel" aria-label="安全记录">
      <header>
        <div className="security-tabs">
          <button className={tab === "audit" ? "active" : ""} onClick={() => setTab("audit")}>
            审计
          </button>
          <button className={tab === "backups" ? "active" : ""} onClick={() => setTab("backups")}>
            备份
          </button>
        </div>
        <button onClick={onClose}>关闭</button>
      </header>
      {tab === "audit" ? (
        <AuditPanel sessionId={sessionId} onNotice={onNotice} />
      ) : (
        <BackupsPanel sessionId={sessionId} busy={busy} onNotice={onNotice} />
      )}
    </aside>
  );
}
