// A6 automated injection cases: the renderer must escape every payload and
// never emit script/image/iframe/anchor elements. These assertions run against
// server-rendered markup, which uses the same React escaping as the app.
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { SafeText } from "./render";
import { ToolBlock } from "./components/ToolBlock";
import { ApprovalDialog } from "./components/ApprovalDialog";
import { PermissionRules } from "./components/PermissionRules";
import type { ApprovalPayload } from "./types";

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
