# P-009-纯 tool_calls 回合必须落 assistant 消息

## 现象

真实 DeepSeek 会话在一次"只调用工具、不输出文本"的回合后，下一次请求返回 HTTP 400：`Messages with role 'tool' must be a response to a preceding message with 'tool_calls'`。UI 显示工具已执行，但会话随即出错。

## 根因

`session.run` 只在 `assistText != ""` 时持久化 assistant 消息。模型常见的行为是 `tool_calls` 与空文本一起返回；该消息被丢弃后，tool 结果在 transcript 中没有前置的 `tool_calls`，provider 历史非法。Mock 脚本总是先输出文本，因此自动化测试长期未覆盖。

## 修复

- 持久化条件改为 `assistText != "" || len(calls) > 0`；`ToolCallsJSON` 必须随消息落库。
- 取消回合（流已结束但 ctx 已取消）不落 `tool_calls`，避免永远不会有结果的悬空配对；已输出的文本仍可保留。
- 回归：`TestTextlessToolCallPersistsAssistant`、既有 `TestCancelMidStream`。

## 预防

- transcript 合法性按 provider 契约建模：每个 `role=tool` 必须能追溯到紧邻 assistant 的 `tool_calls`。
- 用真实模型做验收时优先覆盖"无文本工具调用"这种常见形态；mock 脚本不应成为唯一证据。

##（如复发）再次发生记录

- （无）

## 归属

Phase 3 真机验收（2026-10-30）/ Provider 历史契约。
