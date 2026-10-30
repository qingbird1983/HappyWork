# ADR-0003：审批匹配指纹与执行锁定 hash 分离，参数规范化采用 JCS

- 状态：定稿
- 日期：2026-10-30
- 关联：[research/01](../research/01-v2-design-review.md) A1/D；[architecture/03](../architecture/03-permission-and-approval.md)、[04](../architecture/04-security-and-hooks.md)

## 背景

v2 把 session 作用域的信任绑定与执行一致性锁定共用同一个"全参数指纹"：

1. 写/读类工具每次调用参数必不同（路径、内容），session 作用域实践上永不二次命中 → 用户被迫选 persistent（更宽、更危险），制造审批疲劳，反噬"防权限坍塌"的设计目标；
2. `canonical(JSON)` 未指定规范，Go 编码器的 map 键序与浮点格式不保证稳定，会产生假 `ErrArgsDrift`。

## 决策

1. **双指纹分离**：
   - `matchKey`（审批匹配，粗粒度）= `tool_ref + 动作 + 规范化路径前缀/域名前缀`（MCP 工具按 schema 声明的资源字段构造，未声明则退化为 tool_ref）。session 作用域用它匹配。
   - `execHash`（执行锁定，全参数）= `sha256(JCS(finalArgs))`，用于审批记录与执行器校验（不等即 `ErrArgsDrift`）。
2. **JCS 规范**：canonical JSON 一律采用 RFC 8785（JCS）；实现必须通过 JCS fuzz/一致性测试。
3. 审批弹窗**不展示** execHash（用户无法验证 hash，属安全剧场）；展示人可读参数与影响范围。

## 理由

- "信任管多大范围"与"锁住执行的是什么"是两个正交问题；合并必然顾此失彼（v2 顾了锁定、失了作用域可用性）。
- matchKey 粒度按工具类别在映射表声明，可单独评审与测试，不污染执行锁定语义。

## 后果

- 积极：session 作用域真正可用（"本会话内写 docs/ 不再问"），审批弹窗减负；execHash 语义纯净。
- 代价/义务：matchKey 粒度成为独立设计面，需 golden tests 固化；两指纹都入审计。
- 重新评估条件：发现 matchKey 粒度被注入内容系统性利用（考虑引入内容信任级维度）。
