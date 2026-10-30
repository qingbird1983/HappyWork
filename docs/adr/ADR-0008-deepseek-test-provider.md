# ADR-0008：开发期真机测试 Provider 采用 DeepSeek

- 状态：定稿
- 日期：2026-10-30
- 关联：[research/02](../research/02-deepseek-api.md)；[architecture/01](../architecture/01-overview.md) §接缝 2、[02](../architecture/02-core-loop-and-data.md) §预算；[standards/05](../standards/05-testing.md) §3、[standards/06](../standards/06-security.md) §3

## 背景

Phase 2 起需要真实模型 Provider 联调（此前全用 mock）。所有者决策：真机测试选用 DeepSeek，并已在开发机配置专用 API key 环境变量；同时授权开发期由 AI 会话执行真机测试。

## 决策

1. 开发期真机测试 Provider 为 **DeepSeek**（默认 `deepseek-flash`，`deepseek-v4-pro` 备用），经 OpenAI 兼容端点接入（对接事实见 research/02）。
2. **key 注入**：默认读环境变量 `DEEPSEEK_API_KEY`（与官方文档一致的命名），支持配置覆盖；key 值严禁写入仓库、文档、日志与 transcript（standards/06 §3；日志脱敏遮蔽）。
3. **真机测试授权边界**：开发期 AI 会话被所有者授权使用真实 API 做联调与验收（费用由所有者承担，flash 档价格低）；**自动化测试（单元/集成/CI）仍然全部打桩回放，禁止外连**——真机联调是显式、人工触发（会话内）的动作，不是测试套件的一部分。
4. 预算计费口径以 DeepSeek 返回的 usage 为准（含缓存命中/未命中与 reasoning token，见 research/02），`MaxCostUSD` 按 flash 档单价折算。

## 理由

- OpenAI 兼容 + function calling 完备（tool_choice auto 路径），接入成本低，且不锁定：Provider 抽象下未来可加其他厂商。
- flash 输出 $1.20/M token 且支持 1M 上下文，适合高频开发期测试；缓存命中价差 50 倍，倒逼上下文组装前缀稳定（architecture/02）。
- 备选 OpenAI/Anthropic 官方：价格更高，且所有者已具备 DeepSeek key。

## 后果

- 积极：真机联调从 Phase 2 起即可闭环；usage 字段完整，预算无需估算兜底。
- 代价/义务：真机测试产生的费用需在进度报告披露量级；key 泄露风险由"环境变量 + 脱敏 + 禁止入库"三重控制。
- 重新评估条件：DeepSeek API 破坏性变更无法适配，或所有者更换主力模型。
