# P-004-错误 JSON 流入成功模板渲染成假成功

## 现象

Phase 1 真机验收截图：工具调用失败后，mock 回复显示"已读取（0 字节，截断：false），内容如下："——失败被渲染成了一次 0 字节的成功读取；transcript 重载后工具行一律显示"完成"。安全侧（越界拒绝 + 审计）实际是好的，纯展示层假象。

## 根因

两个叠加：

1. mock 的 `say_tool_echo` 把工具结果 JSON 无条件灌进成功模板；`json.Unmarshal` 把 `{"error":...}` 解进成功结构体得到全零值（`{path:"",size_bytes:0,truncated:false}`），零值顺利走完模板——**"静默零值"把错误伪装成了成功**。
2. transcript 重载的映射把 role=tool 一律标"完成"（且 tool_name 当时未落库，显示为 "tool"）。

## 修复

- mock：解析结构体加 `Error` 字段，非空即改走"工具执行失败：{原因}"分支，不再进成功模板（`internal/model/mock/mock.go`）。
- agent：工具结果消息补 `ToolName` 落库（`internal/agent/session.go`）。
- 前端：transcript 重载解析结果 JSON，含 `error` 键显示"失败"（`frontend/src/App.tsx`）。
- 回归测试：`TestToolResultEchoRendersError`、agent 测试断言 ToolName。

## 预防

- **凡在展示/回喂边界解析 JSON，必须显式分支错误形态**，禁止把错误负载灌进成功模板靠零值"自然"渲染。
- 验收看 UI 文案时要对照库里的 status/audit——UI 是可能说谎的那一层。

##（如复发）再次发生记录

- （无）

## 归属

Phase 1 真机验收（2026-10-30）/ Windows / UI 层，未影响安全行为。
