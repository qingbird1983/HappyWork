# Phase 3：权限引擎与 Hook

- 状态：定稿（2026-10-30；所有者放行，随立项生效）
- 日期：2026-10-30
- 关联：[00-roadmap](00-roadmap.md) §1；[architecture/03](../architecture/03-permission-and-approval.md)、[04](../architecture/04-security-and-hooks.md)；[ADR-0002](../adr/ADR-0002-unified-permission-model.md)、[ADR-0003](../adr/ADR-0003-dual-fingerprint.md)、[ADR-0004](../adr/ADR-0004-path-canonicalization.md)、[ADR-0012](../adr/ADR-0012-jcs-implementation.md)、[ADR-0013](../adr/ADR-0013-hook-trust-model.md)、[ADR-0014](../adr/ADR-0014-content-trust-scope.md)；施工设计 [design.md](../design/phase-3/design.md)、[hooks.md](../design/phase-3/hooks.md)

## 1. 目标

Phase 2 证明"模型能在人的控制下真做事"；Phase 3 证明"**控制本身是确定、可扩展、可追溯的**"。

一句话验收：同一个操作在预设、持久规则、Hook 改参、限额与路径替换同时作用时，只有唯一确定的判定；用户看到的参数就是最终执行的参数；每一次放宽、降级、拒绝与改参都有痕迹。

## 2. 范围

### 做

- **完整权限引擎**：工具映射表（唯一事实源）、四维模型（DIM1 决策 / DIM2 作用域 / DIM3 条件 / DIM4 限额）、最严合并、可逆性修正；未声明工具一律 deny。
- **预设（preset）**：内置 `default` / `readonly` / `cautious`，支持 `presets.d/*.yaml` 自定义；严格 schema，非法即拒绝整份；只能收紧不能放宽；会话创建时选择并落库。
- **持久规则（persistent）**：仅内置可逆写操作可创建；审批弹窗内二次输入确认短语；跨会话按 matchKey 命中；可查、可删；每次命中记 Info + 审计；deny/不可逆操作永不可持久。
- **内容信任级**：当前回合出现过不可信工具结果时，新的写/外部操作强制 once（不命中 session/persistent），弹窗显示触发来源（[ADR-0014](../adr/ADR-0014-content-trust-scope.md)）。
- **execHash 与 TOCTOU**：`sha256(JCS(finalArgs))` 入 `approvals`；执行前重算校验；Hook 只可改白名单字段且改参留痕；执行前对路径与文件身份做二次校验。
- **进程内 Hook**：Go Hook 接口 + `hooks.d/*.yaml` 配置（严格 schema）、五个 Hook 点、优先级、超时、panic 恢复、失败策略（warn/deny/fail_closed）；after 只读；内置 `secret-scan` 示例 Hook（默认关闭）。
- **DIM4 限额**：按会话隔离的滑动窗口计数，只约束自动放行通道；超限升级为 ask 或 deny；事件可见。
- **条件**：路径前缀、敏感级、内容信任级、时间窗；域名规范化作为引擎能力实现并以单测固化（本期无网络工具接入）。
- **前端**：预设选择、持久规则列表与删除、审批弹窗的持久确认短语与信任警示、权限决策/限额事件展示；渲染安全基线不变。
- **迁移 0003**：`sessions.preset`、`approvals.exec_hash/content_trust`、`tool_calls.exec_hash`、`permission_rules` 表。
- **测试**：映射表 golden tests、JCS 官方测试向量与 fuzz、TOCTOU 改参/替身路径演练、Hook 生命周期、限额与迁移升级。

### 不做（明确排除）

- 外部进程 Hook、动态加载代码/插件、自定义工具注入 → 后续阶段独立 ADR；Go `plugin` 永不采用（[ADR-0013](../adr/ADR-0013-hook-trust-model.md)）。
- 删除、shell、网络工具（映射表保留 deny 占位行，不注册可执行工具）。
- 工作区外读取放行：Phase 3 仍硬拒；开放需沙箱或更强隔离后评估。
- OS 沙箱、不可变审计/hash-chain、正式回滚 → Phase 4/5。
- 多用户/组织 RBAC、远程策略分发、UI/UX 精修、Markdown 渲染。
- 对 deny 行或不可逆操作创建持久规则。

## 3. 开工前置条件

- [x] Phase 2 完成（tag `phase-2.end`，main CI 绿）
- [ ] 本文件、design.md、hooks.md 与 ADR-0012/0013/0014 经所有者过目
- [ ] 新增依赖（JCS 参考实现 Apache-2.0、YAML 解析 MIT/Apache-2.0）登记 standards/04 附录 A
- [ ] 在 `feat/phase3-permission-engine` 分支开工

## 4. 交付物

| 类别 | 清单 |
|---|---|
| 代码 | `internal/permission/`（引擎/预设/规则/限额/条件）、`internal/jcs/`（JCS 包装与 I-JSON 校验）、`internal/hook/`（接口/注册表/执行器/配置/内置 secret-scan）、`internal/agent/`（execHash 管道、信任闸门、规则与限额接线）、`internal/storage/`（迁移 0003）、`frontend/`（预设/规则/审批扩展） |
| 文档 | 本文件、`design/phase-3/{design,hooks}.md`、ADR-0012/0013/0014、`report.md`（验收后） |
| 配置 | `presets.d/*.yaml`、`hooks.d/*.yaml` 严格 schema 与示例 |
| 其他 | 迁移 0003；golden/fuzz/TOCTOU 测试；更新 standards/04 依赖表 |

## 5. 验收场景

- [ ] A1 映射表与合并：每个内置工具、MCP 变体与预留 deny 行都有 golden 用例；未声明工具 deny；最严合并、条件降级、可逆性修正（不可逆 → Block、禁自动重试）符合规则表。
- [ ] A2 预设：三个内置预设与 YAML 自定义预设可用；非法 YAML/放宽规则被拒绝整份并提示；会话预设落库；预设只收紧。
- [ ] A3 持久规则：弹窗内输入确认短语才创建；跨会话按 matchKey 命中并记 Info；可在界面查看/删除；deny/不可逆操作不可创建；不可信内容在场时不命中持久规则。
- [ ] A4 execHash/TOCTOU：审批后改参被 `ErrArgsDrift` 拒绝；Hook 只改白名单字段且改参有审计；execHash 入审批与工具记录；JCS 官方向量与 fuzz 通过；非 I-JSON 数字被拒绝。
- [ ] A5 Hook 生命周期：五个 Hook 点按优先级执行；超时、panic、失败策略（warn/deny/fail_closed）符合契约；after 不可改参改结果；配置非法时 Hook 整体禁用并提示；配置指纹写入日志。
- [ ] A6 限额：自动放行超限升级 ask（不可逆行 deny）；按会话隔离；重启归零；事件与审计可见；无静默截断。
- [ ] A7 信任与路径：MCP 不可信内容触发的新写/外部操作强制 once 且弹窗有来源警示；symlink/junction/8.3/大小写/UNC 替换在执行前被拒绝并留痕；敏感文件硬拒不变。
- [ ] A8 门禁：golden/fuzz/TOCTOU/Hook/限额/迁移测试全绿；`gofmt/vet/golangci-lint/go test/check-size/render-safety` + 前端 lint/typecheck/format/test/build 全绿；三平台 `wails3 build` 与 CI 全绿；验收报告落盘。

## 6. 依赖与风险

| 依赖/风险 | 说明与缓解 |
|---|---|
| JCS 参考实现无 v1 tag | 固定精确伪版本，包内 wrapper 隔离；引入官方测试向量与 fuzz（[ADR-0012](../adr/ADR-0012-jcs-implementation.md)） |
| JCS 数字精度与 I-JSON | 计算 hash 前校验数字可被 IEEE-754 精确表示；NaN/Inf/超范围整数拒绝并审计 |
| Hook 与主进程共享地址空间 | 只允许编译期内置 Hook；配置只选内置名字；panic 恢复、超时与失败策略兜底（[ADR-0013](../adr/ADR-0013-hook-trust-model.md)） |
| persistent 被注入滥用 | 仅可逆内置写；二次确认短语；内容信任级强制 once；可查可删；命中留痕 |
| 路径二次校验仍有残窗 | 执行前重新规范化 + 拒绝链接/短名 + 文件身份检查；沙箱前承认残余风险（Phase 5） |
| 限额计数重启归零 | 本期内存计数；防的是失控循环而非对抗性绕过；Phase 4 审计与持久化评估 |
| 引擎规则组合爆炸 | 映射表 + 最严合并 + golden tests；条件类型封闭枚举，未知条件拒绝加载 |

## 7. 与前后阶段的边界

- **承接 Phase 2**：静态 `permission.Gate` 扩展为引擎；审批 DIM1~3、matchKey、session 授权、execHash 快照语义保留；MCP 逐条审批与 untrusted 标记不变。
- **交给 Phase 4**：不可变审计/hash-chain、正式备份回滚、签名与发布；Hook 外部进程形态另立 ADR。
- **UI 精修触发条件已满足**：Phase 3 只做信息完整性所需的最小界面，视觉精修继续留 Backlog。
