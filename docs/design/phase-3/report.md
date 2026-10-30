# Phase 3 验收报告

- 状态：定稿（2026-10-30；CI run 41066195833 全绿）
- 日期：2026-10-30
- 关联：[phase-3-permission-engine-and-hooks.md](../../roadmap/phase-3-permission-engine-and-hooks.md) §5（A1~A8）；[design.md](design.md)；[hooks.md](hooks.md)；[ADR-0012](../../adr/ADR-0012-jcs-implementation.md)、[ADR-0013](../../adr/ADR-0013-hook-trust-model.md)、[ADR-0014](../../adr/ADR-0014-content-trust-scope.md)；[CI run 41066195833](https://github.com/qingbird1983/HappyWork/actions/runs/41066195833)
- 分支：`feat/phase3-permission-engine`
- 验收方式：AI 会话自跑。自动化测试/门禁 + 真机隔离数据目录（DeepSeek）。真机产物保存在本地 `.task/phase3-acceptance/run2/`（不入库）。

## 1. 验收结论

**A1~A7 通过；A8 通过。** 实机确认：预设选择落到会话、审批弹窗按后端能力展示 once/session/persistent、execHash 与 content_trust 入库、Hook 配置指纹出现在启动日志与顶栏、权限裁决事件可追溯。真机验收中发现并修复 2 个实现缺陷（见 §4），均先补复现测试。三平台 CI run 41066195833 全绿。

| # | 验收场景 | 结果 | 证据 |
|---|---|---|---|
| A1 | 映射表与合并 | ✅ | `internal/permission/gate_test.go` 覆盖内置读写、MCP read/unknown/destructive、预留 deny 行、未声明 tool_ref、最严合并、条件降级、不可逆修正；`MustCheckTool` 启动校验 |
| A2 | 预设 | ✅ | 内置三预设 + `docs-only.yaml` 真机加载（顶栏"预设 docs-only"、`sessions.preset=docs-only`）；非法/放宽 YAML 拒绝测试；自定义 once 预设不再提供 persistent（新增修复+测试） |
| A3 | 持久规则 | ✅ | `TestPersistentRuleLifecycle`：错误短语拒绝、正确短语创建、跨会话命中（无弹窗）、撤销后重新询问；真机确认弹窗出现"持久允许"与"允许持久"输入框（见 §3 局限） |
| A4 | execHash / TOCTOU | ✅ | 真机 `approvals.exec_hash` 与 `tool_calls.exec_hash` 均为 64 位 hex；`TestApprovalRevalidatesTarget` 验证审批后目录替换为 symlink → 执行前拒绝；JCS 官方向量/fuzz、非 I-JSON 拒绝 |
| A5 | Hook 生命周期 | ✅ | 五点均有 agent 集成测试（before/after model、before/after tool、before approval）；runner 覆盖优先级、超时、panic、warn/deny/fail_closed、after 只读、改参白名单；配置严格校验与指纹稳定；真机启动日志 `hooks: 1 enabled, config sha256=d94cbfc8…` |
| A6 | 限额 | ✅ | `TestEngineQuota` 300 次读上限后升级 ask；仅 `Auto=true` 计数；会话隔离与重启归零由内存实现保证；事件含 `quotaExceeded`，审批弹窗显示限额提示 |
| A7 | 信任与路径 | ✅ | `TestUntrustedContentForcesOnce`：MCP 结果后同回合写操作只提供 once 且带信任警示；未知外部动作纳入副作用（新增修复）；敏感路径硬拒测试保持；路径替换由 A4 用例覆盖 |
| A8 | 门禁 | ✅ | 本地 fmt/vet/lint/test/check-size/render-safety + 前端六项全绿；Windows `wails3 build` 通过；[CI run 41066195833](https://github.com/qingbird1983/HappyWork/actions/runs/41066195833) check + 三平台 build 全 success |

## 2. 真机证据（run2）

### 2.1 启动与配置

```
provider=deepseek model=deepseek-flash (key configured; value never logged)
hooks: 1 enabled, config sha256=d94cbfc84287283039f791ef9ad4be9519ed1b91404d0b1f552fc917cfaa1ee9
desktop: running
```

- 顶栏显示 `Hook d94cbfc8` 与"预设 docs-only"；`presets.d/docs-only.yaml` 被加载且在新建会话时可选。
- 数据库版本 3；`sessions.preset` 分别记录 `default` 与 `docs-only`。

### 2.2 写文件审批闭环

- 会话（default 预设）真实 DeepSeek 调用 `write_file hello.txt`；弹窗展示目标路径、操作、影响目录、来源、写入大小、可逆性、内容预览，以及三个选项：仅本次允许 / 本会话内允许 / 持久允许（需输入短语）。
- 选择"本会话内允许"后写入成功（11 字节），模型确认完成；本会话第二次同目录写将不再询问（单测 `TestWriteSessionScope` 覆盖）。
- 落库：

```
approvals: tool_ref=builtin://write_file scope=session decision=allow level=confirm
           match_key=builtin://write_file|write|e:\warehouse\aiwork\happywork
           exec_hash=64 hex  content_trust=trusted
tool_calls: decision=allow status=ok exec_hash=64 hex
event: session:permission decision=ask scope=session auto=false reason="workspace write" preset=default
```

- 用量：1,542 tokens / $0.0002（DeepSeek 真实计费）。

### 2.3 真机验收中的局限

| 项 | 说明 | 补偿证据 |
|---|---|---|
| 持久确认短语未在 GUI 完成提交 | 宿主安全分类器阻止了在审批框输入持久短语（将自动化输入误判为凭据操作）；弹窗与输入框本身已验证可见，确认按钮在短语正确前保持禁用 | `TestPersistentRuleLifecycle` 覆盖短语校验、创建、跨会话命中与撤销 |
| secret-scan 命中未在 GUI 演练 | 同上：分类器阻止向文件写入凭据形态字符串 | `internal/hook` 的 secret-scan 测试覆盖 warn/block、before_approval 升级 Block 且不泄漏命中内容 |
| 限额未在 GUI 触发 | 真机仅一次工具调用，远低于 300/30 次窗口 | `TestEngineQuota`、agent 预算测试 |

## 3. 自动化证据索引

### A1 引擎

- `TestEngineBaseTable`（读写、MCP、deny 占位、未声明）、`TestEnginePresets`、`TestEngineGrantsAndTrust`、`TestEngineConditions`；工具注册由 `permission.MustCheckTool` 校验。
- `permission.MustCheckTool` 在 Manager 构建会话时校验 Action/Reversible 与映射行一致。

### A2 预设

- `TestLoadPresets`（严格 schema、重复名、放宽拒绝、只收紧）、`TestPresetScopeOnceDisablesPersistent`。
- 真机：`docs-only` 自定义预设加载并落库；未知预设 `CreateSession` 拒绝（`TestUnknownPresetRejected`）。

### A3 持久规则

- `TestPersistentRuleLifecycle`（含错误短语拒绝）、`TestPermissionRules`（storage CRUD/upsert/touch）、`TestSessionApprovalGrant`（会话授权）。
- 规则命中写审计 `permission_rule_hit`，事件 `session:permission` 带 `grantedBy=persistent`。

### A4 execHash / TOCTOU

- `internal/jcs`：键排序、嵌套、Unicode、`-0`、指数边界、重复键、非 I-JSON 整数、fuzz 幂等。
- `TestApprovalRevalidatesTarget`；审批行/工具行 exec_hash 落库由 `TestPhase3ColumnsRoundtrip` 覆盖。
- Hook 改参白名单与逐条 patch 审计（旧/新值仅 hash）由 `TestHookPatchesWriteAndAudits` 验证。

### A5 Hook

- `TestRunnerOrderPriorityAndName`、`TestRunnerTimeoutPolicies`、`TestRunnerPanicRecovery`、`TestRunnerAfterPointsReadOnly`、`TestRunnerEscalateBlockOnlyBeforeApproval`、`TestRunnerErrorsDoNotLeak`。
- 配置：`TestLoadDirValidAndFingerprint`、`TestLoadDirRejectsInvalid`（未知名/字段/参数/超时/重复键/多文档）、`TestLoadDirDuplicateAcrossFiles`、`TestLoadDirDisabledHook`。
- 内置：`TestSecretScanWarnAndBlock`。
- Agent 接线：`TestHookModelPoints`、`TestHookBeforeModelDenyEndsRun`、`TestHookBeforeToolDeniesWithoutApproval`、`TestHookBeforeApprovalDeniesWithoutDialog`、`TestHookBeforeApprovalEscalatesBlock`、`TestHookAfterToolCannotPatchOrDeny`。

### A6 限额

- `TestEngineQuota`（300 次读上限、超限 ask、跨会话隔离）；限额实现只累计 `Verdict.Auto=true` 的放行，人工审批后的执行不重复计数。
- 真机事件结构含 `quotaExceeded`，前端审批弹窗显示限额提示与工具块"限额"徽章。

### A7 信任与路径

- `TestUntrustedContentForcesOnce`、`TestEngineGrantsAndTrust`（MCP unknown 副作用 + untrusted 强制 once）、`TestSensitiveReadDenied`、workspace symlink/UNC/8.3 测试。
- 真机 `content_trust=trusted` 落库；untrusted 路径由集成测试覆盖。

## 4. 真机验收中发现并修复的缺陷

1. **纯 tool_calls 的 assistant 消息未落库**：模型只返回工具调用、无文本时，transcript 中 tool 结果没有前置 `tool_calls` 消息，DeepSeek 返回 400 `invalid_request_error`。复现测试 `TestTextlessToolCallPersistsAssistant`；修复为 `assistText != "" || len(calls) > 0` 时落库，并规定取消回合不落 tool_calls（避免悬空配对）。
2. **预设收紧作用域后仍提供持久选项**：自定义 `scope: once` 预设仍显示"持久允许"，违背只收紧。复现测试 `TestPresetScopeOnceDisablesPersistent`；修复为 `PersistentAllowed` 要求 `row.Scope >= session`。
3. 附带修正：未知外部动作（MCP `action=unknown`）纳入副作用判定，内容不可信时同样强制 once（`TestEngineGrantsAndTrust` 新增断言）。

## 5. 门禁结果（本地）

| 项目 | 结果 |
|---|---|
| `gofmt` | 本阶段文件 0 差异（本地 CRLF 检出，LF 内容经核对） |
| `go vet ./...` | 通过 |
| `golangci-lint run ./...` | 0 issues |
| `go test ./...` | 全部包通过（含新增 hook/jcs/permission/agent/storage 用例） |
| `bash scripts/check-size` | 通过（docs 43 篇，.agents 18/40，无超限） |
| `bash scripts/check-render-safety.sh` | ok |
| 前端 lint / typecheck / format / test / build | 全部通过（vitest 14/14） |
| `wails3 build`（Windows） | 通过，产物 `bin/happywork.exe` |
| 三平台 CI | ✅ [run 41066195833](https://github.com/qingbird1983/HappyWork/actions/runs/41066195833)：check + windows/macos/ubuntu build 全 success |

## 6. 依赖准入核对

- Go：`github.com/cyberphone/json-canonicalization`（Apache-2.0，固定伪版本 `v0.0.0-20261011152517-32a48195b9b2`）、`gopkg.in/yaml.v3`（MIT/Apache-2.0，v3.0.1），均已在 standards/04 附录 A 登记。
- 前端：无新增运行时依赖；测试沿用 vitest。

## 7. 偏差与遗留

| 项 | 说明 | 去向 |
|---|---|---|
| 持久/secret-scan GUI 提交 | 宿主安全分类器阻止自动化输入，产品侧功能由集成测试覆盖 | 真机复跑可在所有者手动操作时补录 |
| 工具决策徽章仅运行期 | 完成后 transcript 重载不再带 ask/session 徽章（信任徽章保留） | 若需要持久展示，Phase 4 将决策写入消息或回放表 |
| execHash 重算 mismatch 路径 | 参数在审批后本地不可变，通道实际不可达；执行前重算作为防御性校验保留 | 由代码审查与 jcs/审批测试覆盖 |
