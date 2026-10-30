// A6 automated injection cases: the renderer must escape every payload and
// never emit script/image/iframe/anchor elements. These assertions run against
// server-rendered markup, which uses the same React escaping as the app.
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { SafeText } from "./render";
import { ToolBlock } from "./components/ToolBlock";

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
});
