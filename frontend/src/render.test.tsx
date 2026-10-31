// A6 automated injection cases: the renderer must escape every payload and
// never emit script/image/iframe/anchor elements. These assertions run against
// server-rendered markup, which uses the same React escaping as the app.
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { SafeText } from "./render";
import { ToolBlock } from "./components/ToolBlock";
import { ApprovalDialog } from "./components/ApprovalDialog";
import { PermissionRules } from "./components/PermissionRules";
import { AuditList, AuditStatus } from "./components/AuditPanel";
import { BackupConfirm, BackupList } from "./components/BackupsPanel";
import type { ApprovalPayload, AuditRecord, AuditVerify, FileBackup } from "./types";

const payloads = [
  `<script>alert("xss")</script>`,
  `<img src=x onerror="alert(1)">`,
  `<iframe src="https://evil.example"></iframe>`,
  `<a href="javascript:alert(1)">click</a>`,
  `<a href="data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==">data</a>`,
  `<svg onload=alert(1)>`,
  `javascript:alert(1)`,
  `data:text/html,<script>alert(1)</script>`,
];

describe("SafeText", () => {
  for (const payload of payloads) {
    it(`escapes ${payload.slice(0, 24)}`, () => {
      const html = renderToStaticMarkup(<SafeText text={payload} trust="untrusted" />);
      expect(html).toContain('data-trust="untrusted"');
      expect(html).not.toMatch(/<script|<img|<iframe|<svg|<a[\s>]/i);
      if (payload.includes("<")) {
        expect(html).toContain("&lt;");
        expect(html).toContain("&gt;");
      }
    });
  }

  it("renders URLs as text, not clickable links", () => {
    const html = renderToStaticMarkup(<SafeText text="https://example.com/a?b=1" />);
    expect(html).toContain("https://example.com/a?b=1");
    expect(html).not.toContain("<a");
  });
});

describe("ToolBlock", () => {
  it("escapes tool output and marks MCP content untrusted", () => {
    const html = renderToStaticMarkup(
      <ToolBlock
        name={`mcp__demo__<script>`}
        status="ok"
        trust="untrusted"
        argsSummary={`<img src=x onerror=alert(1)>`}
        detail={`<iframe src="javascript:alert(1)"></iframe>`}
      />,
    );
    expect(html).toContain('data-trust="untrusted"');
    expect(html).toContain("外部内容 · 不可信");
    expect(html).not.toMatch(/<script|<img|<iframe/i);
    expect(html).toContain("&lt;script&gt;");
    expect(html).toContain("&lt;iframe");
  });

  it("shows the permission decision and quota badge", () => {
    const html = renderToStaticMarkup(
      <ToolBlock
        name="write_file"
        status="running"
        decision="ask"
        automatic={false}
        quotaExceeded
        scope="once"
      />,
    );
    expect(html).toContain("需审批");
    expect(html).toContain("限额");
    expect(html).toContain("once");
  });
});

const approvalPayload: ApprovalPayload = {
  approvalID: "a1",
  sessionID: "s1",
  toolCallID: "c1",
  toolName: "write_file",
  toolRef: "builtin://write_file",
  level: "confirm",
  title: "写入文件",
  summary: "新建 a.txt",
  fields: [{ label: "目标路径", value: "a.txt" }],
  preview: "<script>alert(1)</script>",
  scopeOptions: [
    { scope: "once", label: "仅本次允许", description: "只执行这一次" },
    { scope: "session", label: "本会话内允许", description: "本会话内写该目录下不再询问" },
    {
      scope: "persistent",
      label: "持久允许（需输入短语）",
      description: "本机所有会话有效；可在权限规则中查看和撤销",
    },
  ],
  source: "builtin",
  reversible: true,
  contentTrust: "untrusted",
  trustWarning: "当前回合包含外部不可信内容，本次仅允许一次",
  quotaNotice: "自动放行限额已触发，本次需要你确认",
  warnings: ["secret-scan: 字段 content 疑似包含 aws_access_key"],
  preset: "default",
  expiresAt: "2026-10-30T00:00:00Z",
};

describe("ApprovalDialog", () => {
  it("shows trust, quota and hook warnings plus the persistent phrase", () => {
    const html = renderToStaticMarkup(
      <ApprovalDialog payload={approvalPayload} onApprove={() => {}} onDeny={() => {}} />,
    );
    expect(html).toContain("外部不可信内容");
    expect(html).toContain("限额已触发");
    expect(html).toContain("secret-scan");
    expect(html).toContain("持久允许");
    expect(html).not.toMatch(/<script/i);
    expect(html).toContain("&lt;script&gt;");
  });

  it("requires the block phrase for block-level requests", () => {
    const html = renderToStaticMarkup(
      <ApprovalDialog
        payload={{ ...approvalPayload, level: "block", contentTrust: "trusted" }}
        onApprove={() => {}}
        onDeny={() => {}}
      />,
    );
    expect(html).toContain("确认执行");
    expect(html).toContain("disabled");
  });
});

describe("PermissionRules", () => {
  it("escapes rule paths and offers revoke", () => {
    const html = renderToStaticMarkup(
      <PermissionRules
        rules={[
          {
            id: "r1",
            toolRef: "builtin://write_file",
            action: "write",
            matchKey: `/ws/<img src=x onerror=alert(1)>`,
            decision: "allow",
            note: "",
            createdAt: "",
            lastUsedAt: "",
            useCount: 0,
          },
        ]}
        onDelete={() => {}}
      />,
    );
    expect(html).toContain("撤销");
    expect(html).not.toMatch(/<img/i);
    expect(html).toContain("&lt;img");
  });
});

const brokenVerify: AuditVerify = {
  ok: false,
  sealedRows: 7,
  legacyRows: 2,
  lastId: 9,
  headHash: "deadbeef",
  brokenId: 5,
  brokenReason: `entry_hash 不匹配 <script>alert(1)</script>`,
  headMatch: false,
};

const auditRow: AuditRecord = {
  id: 12,
  sessionId: "s1",
  turnId: 3,
  traceId: "t1",
  action: "tool_call",
  decision: `deny"><img src=x onerror=alert(1)>`,
  reason: `<script>alert(1)</script> <iframe src="https://evil.example"></iframe>`,
  detail: `{"relPath":"<script>x</script>"}`,
  createdAt: "2026-10-30T08:15:30Z",
  prevHash: "aa",
  entryHash: "bb",
};

describe("AuditPanel", () => {
  it("escapes audit reasons and whitelists the decision class", () => {
    const html = renderToStaticMarkup(<AuditList entries={[auditRow]} />);
    expect(html).not.toMatch(/<script|<img|<iframe/i);
    expect(html).toContain("&lt;iframe");
    expect(html).toContain("&lt;script&gt;");
    // The hostile decision value stays text and never becomes a class name.
    expect(html).toContain('class="audit-decision other"');
    expect(html).toContain("08:15:30");
  });

  it("states a broken chain with the first bad id instead of hiding it", () => {
    const html = renderToStaticMarkup(<AuditStatus verify={brokenVerify} />);
    expect(html).toContain("链状态：破损");
    expect(html).toContain("第 5 条起中断");
    expect(html).toContain('class="chain broken"');
    expect(html).not.toMatch(/<script/i);
    expect(html).toContain("&lt;script&gt;");
  });

  it("reports legacy rows without claiming the whole history is sealed", () => {
    const html = renderToStaticMarkup(
      <AuditStatus verify={{ ...brokenVerify, ok: true, brokenId: undefined, brokenReason: "" }} />,
    );
    expect(html).toContain("链状态：完整（2 条旧记录未密封）");
    expect(html).not.toContain("chain-banner");
  });
});

const overwriteBackup: FileBackup = {
  id: "b1",
  sessionId: "s1",
  turnId: 2,
  relPath: "notes/a.md",
  kind: "overwrite",
  reason: "tool_write",
  version: 3,
  mode: 420,
  size: 2048,
  backupRel: "backups/s1/notes/a.md/v000003-b1.bak",
  backupSha256: "aa",
  writtenSha256: "bb",
  createdAt: "2026-10-30T08:15:30Z",
  verifiedAt: "2026-10-30T08:15:31Z",
};

// Without verified_at the version never passed the post-write hash check, so it
// is listed but not offered as a restore source.
const unverifiedBackup: FileBackup = {
  ...overwriteBackup,
  id: "b2",
  version: 2,
  verifiedAt: undefined,
};

const createBackup: FileBackup = {
  ...overwriteBackup,
  id: "b3",
  version: 1,
  kind: "create",
  backupRel: "",
  writtenSha256: "cc",
};

const incompleteCreate: FileBackup = { ...createBackup, id: "b4", version: 0, writtenSha256: "" };

// A pre-restore safety copy is hashed at copy time and rechecked on restore;
// it has no write to verify, so verified_at is empty and it must still be
// offered as a restore source (undo the restore).
const safetyBackup: FileBackup = {
  ...overwriteBackup,
  id: "b5",
  version: 4,
  reason: "pre_restore",
  verifiedAt: undefined,
};

// Paths come from the model, so they must stay escaped text.
const hostilePath = `notes/<img src=x onerror=alert(1)>.md`;

describe("BackupsPanel", () => {
  it("escapes backup paths and offers restore only for verified overwrites", () => {
    const html = renderToStaticMarkup(
      <BackupList
        groups={[
          { relPath: hostilePath, backups: [overwriteBackup, unverifiedBackup] },
          { relPath: "notes/b.md", backups: [createBackup, incompleteCreate] },
        ]}
        busy={false}
        onPick={() => {}}
      />,
    );
    expect(html).not.toMatch(/<img/i);
    expect(html).toContain("&lt;img");
    expect(html).toContain("2.0 KiB");
    expect(html.match(/恢复/g)).toHaveLength(1);
    expect(html.match(/撤销创建/g)).toHaveLength(1);
    expect(html).toContain("未确认");
    expect(html).toContain("未完成");
  });

  it("offers the pre-restore safety copy as a restore source", () => {
    const html = renderToStaticMarkup(
      <BackupList
        groups={[{ relPath: "note.txt", backups: [safetyBackup] }]}
        busy={false}
        onPick={() => {}}
      />,
    );
    expect(html).toContain("安全副本");
    expect(html.match(/恢复/g)).toHaveLength(1);
    expect(html).not.toContain("未确认");
  });

  it("disables restore and undo while the session is running", () => {
    const html = renderToStaticMarkup(
      <BackupList
        groups={[{ relPath: "notes/a.md", backups: [overwriteBackup, createBackup] }]}
        busy
        onPick={() => {}}
      />,
    );
    expect(html.match(/disabled/g)).toHaveLength(2);
  });

  it("explains the safety copy before restoring", () => {
    const html = renderToStaticMarkup(
      <BackupConfirm
        pending={{ mode: "restore", backup: { ...overwriteBackup, relPath: hostilePath } }}
      />,
    );
    expect(html).toContain("当前内容会先成为新版本");
    expect(html).not.toMatch(/<img/i);
    expect(html).toContain("&lt;img");
  });

  it("explains that undo keeps the content in the backup area", () => {
    const html = renderToStaticMarkup(
      <BackupConfirm pending={{ mode: "undo", backup: createBackup }} />,
    );
    expect(html).toContain("不会永久删除");
  });
});
