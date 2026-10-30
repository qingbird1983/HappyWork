# 01-v2 设计方案评审（可行性 / 漏洞 / 缺失）

- 状态：定稿（事实冻结；后续评审新开篇）
- 日期：2026-10-30
- 关联：评审对象为 `briefs/`《Go + Wails 3 桌面端 Agent 设计方案 v2》（所有者投料）；结论已沉淀为 architecture/01–04 与 ADR-0001~0005

## 背景与问题

所有者投料 v2 设计简报，要求评估：可行性、漏洞与不足、优化建议。本篇记录评审发现的事实与依据，**不做重复决策**（决策在 ADR）。

## 方法与来源

- 通读 v2 全文（644 行），按"设计内在一致性 / 跨平台事实 / 缺失块 / 范围工期"四个维度核查；
- 外部事实核实（2026-10-30 检索）：

| 来源 | 内容 | 可信度 |
|---|---|---|
| [wails.io/blog/wails-v3-released](https://wails.io/blog/wails-v3-released) | Wails v3.0.0 于 2026-09 转正（桌面三平台 stable；移动端实验性；要求 Go 1.26+）；2026-08 为 beta | 官方，高 |
| [wails.io/contributing/release-policy](https://wails.io/contributing/release-policy) | 官方建议钉版本，nightly/weekly 构建并存 | 官方，高 |
| OpenAI 工程博客《Building a safe, effective sandbox to enable Codex on Windows》 | Codex 沙箱路线：macOS Seatbelt / Linux Landlock(+seccomp 等) / Windows AppContainer；Windows 沙箱复杂度足以单写一篇博文 | 官方，高 |
| RFC 8785 | JSON Canonicalization Scheme（JCS） | 标准，高 |
| Go 官方文档 | `plugin` 包不支持 Windows；桌面应用分发中 dylib 加载与签名公证冲突 | 官方，高 |

## 事实与数据

### A. 设计内在矛盾（4 项）

| # | 发现 | 影响 |
|---|---|---|
| A1 | v2 §6.1 session 作用域绑定**全参数指纹**，但写/读类工具每次调用参数必不同（路径、内容），session 作用域实践上永不二次命中 | 用户被迫选 persistent（更宽），制造审批疲劳，与 v2 自己的"防权限坍塌"目标矛盾 |
| A2 | v2 §5.2 RBAC+ABAC 引擎与 §6 四维审批+工具映射表是两套并行决策体系，`Evaluate` 骨架串行调用两者但无优先级/冲突消解规则 | 实现期退化为任意 if-else 顺序；角色 YAML 说 allow、映射表说 ask 时无答案 |
| A3 | v2 §5.2 角色 YAML 示例 `allow:` 键重复出现两次，是非法 YAML | 配置无 schema 校验器的直接证据 |
| A4 | v2 §4.3 状态机 `paused` 有入口（审批超时）无出口转移；无 `interrupted`（崩溃恢复）状态 | 恢复语义不完整；崩溃时 running_tool 的副作用不明 |

### B. 跨平台技术漏洞（3 项）

| # | 发现 | 影响 |
|---|---|---|
| B1 | v2 §10.1 单实例锁写 `flock` + `~/.config/...`：Windows 无 `flock`，`~/.config` 非 Windows 约定路径 | 按原文实现 Windows 直接不可用 |
| B2 | v2 §7.1 把三平台 OS 沙箱（namespace/Seatbelt/AppContainer）当作"统一接口"一件事排期 2 周 | AppContainer 需 profile SID + 目录 ACL（见 OpenAI 博文），成本远超前两者，工期不可行 |
| B3 | v2 §7.1 文件系统快照：Windows VSS / APFS 快照（需管理员）/ btrfs 互不可移植；用户态全量复制对大工作区过慢 | 快照承诺无法跨平台兑现；同类产品均不做快照（沙箱+审批兜底） |

### C. 关键设计缺失（5 项）

| # | 缺失 | 影响 |
|---|---|---|
| C1 | **上下文窗口管理**：仅"工具结果 256KB 存附件"，无对话级组装预算、历史折叠、压缩策略 | 长会话必然撑爆 context window；Agent 产品第一工程问题 |
| C2 | **崩溃恢复语义**：崩溃时正在执行的工具副作用是否发生不可知，无 interrupted 标记与"结果未知"处理 | 恢复后可能二次执行副作用 |
| C3 | **测试策略**：全文无测试设计（权限引擎是纯函数、天然适合表驱动 golden test） | 与 standards/05 复现先行义务脱节 |
| C4 | **应用更新机制**：安全敏感桌面应用无补丁通道意味着渲染层漏洞修不了 | 安全债 |
| C5 | **模型层细节**：function-calling 跨 provider 归一化、流式 tool call 分片拼装、token 计费口径（provider usage vs 本地估算）均空白 | 预算与流式实现的基础约定缺失 |

### D. 小问题

- `canonical(finalArgs)` 未指定规范：Go `encoding/json` 的 map 键序与浮点格式不保证稳定 → 假 `ErrArgsDrift`；
- 审批弹窗向用户展示 argsHash：用户无法验证 hash，属安全剧场，徒增噪音；
- `MaxTurns` 默认 20 偏低（真实任务 50+ turn 常见）；
- 循环检测（连续 3 次相同指纹）抓不住参数渐变循环，未明示此局限。

### E. 值得保留的设计（评审确认成立）

四维正交审批（对七级枚举的重构判断正确）；TOCTOU 七步管道 + hash 锁定执行；审批超时挂起而非静默拒绝；渲染安全基线进阶段 1；Wails 适配层隔离；事件 30–50ms 批量 flush；单实例锁问题意识；approvals 独立审计表；`Reversible` 与 `RetrySafe` 字段分离。

## 结论

1. **可行性成立**：核心理念（最小权限 × 可逆性 × 参数一致性）站得住；Wails v3 已转正，最大框架风险解除；三平台沙箱路线有 Codex 先例可循。
2. **v2 不能直接施工**：A1/A2 是权限模型的内在矛盾，实现后会产生"比不修更糟"的效果（审批疲劳反噬）；B1–B3 按原文在 Windows 不可用或不可行。
3. **工期不可信**：v2 阶段 2（1–2 周）与阶段 3（2 周）严重低估，需拆分并降级（快照→备份、沙箱分期、外部 Hook 延后）。

## 对本项目的影响（结论去向）

| 发现 | 去向 |
|---|---|
| A1 | [ADR-0003](../adr/ADR-0003-dual-fingerprint.md)：双指纹分离（matchKey / execHash） |
| A2 + A3 | [ADR-0002](../adr/ADR-0002-unified-permission-model.md)：单一工具映射表 + 角色预设 + 最严者胜 |
| B2 + B3 + 工期 | [ADR-0005](../adr/ADR-0005-mvp-security-downgrade.md)：MVP 安全降级（备份式回滚、沙箱分期、外部 Hook 延后） |
| B1 + 路径逃逸 + JCS | [ADR-0004](../adr/ADR-0004-path-canonicalization.md) + architecture/04 §单实例锁 |
| A4 + C1 + C2 + C4 + C5 | architecture/02（状态机补全、上下文管理、崩溃恢复）、04（更新机制） |
| C3 | architecture/04 §测试基线 + standards/05 回填 |
| 外部事实 E | [ADR-0001](../adr/ADR-0001-adopt-go-wails3.md)（Wails v3 stable、Go 1.26+、钉版本） |

## 更新记录

- 2026-10-30：初稿（评审结论已全部沉淀，本篇冻结）。
