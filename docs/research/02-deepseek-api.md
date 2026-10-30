# 02-DeepSeek API 对接调研

- 状态：定稿（事实冻结；API 变更时新开篇）
- 日期：2026-10-30
- 关联：[ADR-0008](../adr/ADR-0008-deepseek-test-provider.md)；[architecture/02](../architecture/02-core-loop-and-data.md) §预算与计费口径

## 背景与问题

所有者决策：开发期真机测试 LLM 选用 DeepSeek（ADR-0008）。本篇记录对接所需的官方事实：端点、鉴权、模型、function calling、流式、usage 字段与价格。

## 方法与来源

检索 DeepSeek 官方文档 api-docs.deepseek.com（2026-10-30）：概览页、`/api/create-chat-completion` 参考、`/quick_start/pricing`。来源为官方，可信度高；API 处于快速演进期，接入时以当日文档为准。

## 事实与数据

### 端点与鉴权

- OpenAI 兼容端点：`https://api.deepseek.com`（chat：`/chat/completions`）；另有 Anthropic 兼容端点 `/anthropic`（本项目不用）。
- 鉴权：`Authorization: Bearer <API_KEY>`；兼容 OpenAI SDK（改 base_url 即可）。

### 模型（2026-10 现状）

| 模型 | 定位 | 上下文 | 最大输出 | 并发 |
|---|---|---|---|---|
| `deepseek-flash`（V4.1-Flash-1017） | 便宜快速，支持视觉 | 1M | 384K | 2500 |
| `deepseek-v4-pro`（V4-Pro-0813） | 更强 | 1M | 384K | 500 |

旧名（`deepseek-v4-flash` 等）仍可用但映射到 flash 计费。

### function calling（工具调用）

- `tools`：仅 `function` 类型；`name` ≤128 字符，`parameters` 为 JSON Schema，`strict`（默认 false，beta）。
- `tool_choice`：`none / auto / required / 具名函数`；默认 `auto`。**thinking 模式下 `required` 与具名选择返回 400**——本项目循环用默认 `auto`，无冲突。
- 响应：`message.tool_calls[]`（id、function.name、function.arguments 为 JSON 字符串，官方注明"使用前须校验"——与架构 03 的规范化/JCS 管线要求一致）。

### 流式与 usage

- `stream: true` 走 SSE，`data: [DONE]` 结束；`stream_options.include_usage: true` 时 usage 挂在最后一个 chunk。
- tool_calls 分片规则：首个 chunk 带 id/type/name，后续 chunk 增量拼接 `arguments` 字符串。
- usage 字段：`prompt_tokens / completion_tokens / total_tokens` + `prompt_cache_hit_tokens` / `prompt_cache_miss_tokens` + `reasoning_tokens`——**完整覆盖架构 02 的预算计费口径需求，且缓存命中可单独观测**。

### 其他参数要点

- `thinking`：`{type: enabled|disabled}`，默认 enabled；`reasoning_effort` 默认 high。thinking 模式下 `temperature` 无效、`top_p` 建议 0.95–1.0。
- `response_format`：`text` / `json_object`。
- `frequency_penalty` / `presence_penalty` 已废弃（无效果）。

### 价格（每 1M token，峰时）

| 模型 | 输入·缓存命中 | 输入·缓存未命中 | 输出 |
|---|---|---|---|
| flash | $0.006 | $0.30 | $1.20 |
| v4-pro | $0.044 | $1.32 | $3.96 |

非峰时全部五折（周末全天 + 工作日部分时段，按 UTC 定义）。

## 结论

1. OpenAI 兼容层可直接复用，接入成本低；function calling 能力满足 Agent 循环（tool_choice auto 路径无限制）。
2. usage 字段（含缓存命中/未命中、reasoning token）完整支撑 Budget 计费口径，无需本地估算兜底。
3. flash 价格低（输出 $1.20/M），适合开发期高频真机测试；上下文 1M 对长会话友好。

## 对本项目的影响

- Provider 适配层（architecture/01 接缝 2）按 OpenAI 兼容协议实现，DeepSeek 为首个真实实现。
- **上下文组装保持前缀稳定**（system prompt + 会话摘要在前，工具结果增量在后）以吃到 prompt cache——缓存命中与未命中价差 50 倍，组装策略（architecture/02）直接影响测试成本。
- 流式 tool_calls 分片拼装按上述规则实现并写 fixture 测试（standards/05 契约层）。
- `function.arguments` 是字符串，解析失败按 `ErrArgsDrift` 同级错误处理（进入审计，不静默重试）。

## 更新记录

- 2026-10-30：初稿（官方文档三页核实，冻结）。
