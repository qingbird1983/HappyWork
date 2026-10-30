# ADR-0012：execHash 的 JCS 实现采用 RFC 8785 参考实现

- 状态：定稿（2026-10-30，随 Phase 3 立项放行）
- 日期：2026-10-30
- 关联：[ADR-0003](ADR-0003-dual-fingerprint.md)；[architecture/04](../architecture/04-security-and-hooks.md) §TOCTOU；[research/02](../research/02-deepseek-api.md)（无直接关联，仅依赖纪律参照）

## 背景

ADR-0003 已决定 `execHash = sha256(JCS(finalArgs))`，但未定实现来源。JCS（RFC 8785）的难点在数字格式化（ECMAScript 最短表示、`-0`、指数阈值）与 UTF-16 键排序；自行实现极易在边界值上产生不同字节，造成假 `ErrArgsDrift` 或哈希歧义。

## 决策

1. 采用 RFC 8785 附录 G 列出的 Go 参考实现 `github.com/cyberphone/json-canonicalization`（Apache-2.0），导入路径为 `.../go/src/webpki.org/jsoncanonicalizer`。
2. 在 `internal/jcs` 做薄包装：输入校验（I-JSON：UTF-8、无重复键、数字可被 IEEE-754 精确表示，拒绝 NaN/Inf/超出 2^53 的不可逆整数）→ `Transform` → `sha256`；业务代码不直接依赖上游 API。
3. 固定精确伪版本（当前 `v0.0.0-20261011152517-32a48195b9b2`）；升级必须同时通过 RFC 8785 官方测试向量与本地 fuzz，不得静默漂移。
4. 测试：官方 `testdata` 向量 + 属性测试（排序幂等、Unicode 键、`-0`、指数边界、嵌套）；非 I-JSON 数字必须返回错误并写审计，而不是被规范化后继续执行。

## 理由

- 该实现由 RFC 作者维护，被 sigstore/containers 等生态广泛使用；数字格式化是对齐规范的最大风险点，借成熟实现优于自研。
- 包装层把"格式正确"与"输入合法"分开：JCS 只定义 I-JSON 子集，非法数字必须在进入 hash 前拒绝。
- 备选 `json-canon` 等新库虽更严格，但生态验证与维护历史不足；自研成本与回归风险最高。

## 后果

- 积极：execHash 可在跨平台、跨时间稳定复现；错误输入在审批前暴露。
- 代价/义务：引入一个无 v1 tag 的 Apache-2.0 依赖（登记 standards/04）；上游升级需重新跑向量与 fuzz。
- 触发重新评估：上游停止维护、发现与 RFC 向量不一致、或需要流式超大参数 hash。

## 备选方案

| 方案 | 结论 |
|---|---|
| 自研 JCS（排序 + `strconv.FormatFloat`） | 拒绝：浮点边界与 UTF-16 排序风险高，测试成本不低于依赖 |
| 其他 Go JCS 库 | 暂缓：缺少足够生态验证；如上游失效可作为迁移候选 |
| 对原始参数字符串排序后 hash（非 JCS） | 拒绝：违反 ADR-0003，编码差异会产生假漂移 |
