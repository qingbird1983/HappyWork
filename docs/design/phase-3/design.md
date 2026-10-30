# Phase 3 施工设计：权限引擎与 Hook

- 状态：定稿（2026-10-30；所有者放行，随立项生效）
- 日期：2026-10-30
- 关联：[phase-3-permission-engine-and-hooks.md](../../roadmap/phase-3-permission-engine-and-hooks.md)（范围与验收）；[architecture/03](../../architecture/03-permission-and-approval.md)、[04](../../architecture/04-security-and-hooks.md)；[ADR-0002](../../adr/ADR-0002-unified-permission-model.md)、[ADR-0003](../../adr/ADR-0003-dual-fingerprint.md)、[ADR-0004](../../adr/ADR-0004-path-canonicalization.md)、[ADR-0012](../../adr/ADR-0012-jcs-implementation.md)、[ADR-0013](../../adr/ADR-0013-hook-trust-model.md)、[ADR-0014](../../adr/ADR-0014-content-trust-scope.md)；Hook 细节见 [hooks.md](hooks.md)

## 1. 目标与非目标

目标即范围合同 A1~A8：把 Phase 2 的静态门卫升级为可配置、可解释、可审计的判定引擎；把审批从"参数快照"升级为 JCS execHash 执行锁定；把 Hook 作为受控扩展点接入同一条管道。

非目标与范围合同一致：外部进程 Hook/动态代码、自定义工具注入、删除/shell/网络工具、工作区外读取、OS 沙箱、不可变审计/回滚、多用户 RBAC、UI 精修。

## 2. 技术决策落位

| 决策点 | 结论 | 依据 |
|---|---|---|
| 权限事实源 | 每个工具必须在映射表声明一行；运行时按 `tool_ref` 查表；未声明即 deny | ADR-0002；启动时注册校验 |
| 合并规则 | `deny > ask > allow`；作用域取最小；条件取交集；预设只能收紧 | architecture/03 |
| 内容信任级 | DIM3 条件，不并入 matchKey；不可信内容闩锁使副作用强制 once | ADR-0014 |
| 持久规则 | 仅内置可逆写；审批内二次输入短语；可查可删；不覆盖 deny/不可逆 | architecture/03；范围合同 |
| JCS | RFC 8785 参考实现 + I-JSON 前置校验；`internal/jcs` 包装；固定伪版本 | ADR-0012 |
| 执行锁定 | `execHash` 入 approvals/tool_calls；执行前重算；不匹配 `ErrArgsDrift` | ADR-0003；architecture/04 |
| Hook 信任 | 仅编译期内置；配置选名字；无签名/TOFU；after 只读 | ADR-0013 |
| Hook 改参 | 仅 before_tool；仅工具声明的白名单顶层字段；逐次审计 | architecture/04 |
| DIM4 限额 | 只约束自动放行；会话内存滑动窗口；超限升级 ask/deny | architecture/03 |
| 工作区外读取 | 仍硬拒（比 architecture/03 更严） | 云端模型撤离风险；沙箱后再评估 |
| 预设配置 | 内置三预设 + `presets.d/*.yaml`；严格 schema；只收紧 | ADR-0002 |
| Hook 配置 | `hooks.d/*.yaml`；严格 schema；非法整份禁用并告警 | ADR-0013；mcp.json 先例 |

## 3. 代码切片与依赖方向

```
internal/permission/
  engine.go       Evaluate：行查询→预设→持久→条件→可逆→限额→信任
  table.go        内置映射表 + 注册校验
  preset.go       预设模型、内置预设、YAML 加载与只收紧校验
  rules.go        持久规则接口（存储实现在 storage）
  quota.go        会话滑动窗口计数
  condition.go    路径/敏感/信任/时间窗/域名条件（纯函数）
  matchkey.go     matchKey 构造（沿用 Phase 2 语义）
internal/jcs/
  jcs.go          I-JSON 校验、Transform、Hash
internal/hook/
  hook.go         接口、点、事件、结果
  runner.go       排序、超时、panic 恢复、失败策略、改参白名单
  config.go       hooks.d 严格加载与指纹
  builtin.go      secret-scan 示例 Hook
internal/agent/
  exec.go         管道接线：Hook→规范化→hash→引擎→审批→校验→执行
  trust.go        run 级内容信任闩锁
  approval.go     持久作用域与确认短语
internal/storage/
  permissions.go  规则 CRUD、命中 touch
migrations/0003_phase3.sql
frontend/src/components/PresetPicker.tsx / PermissionRules.tsx / ApprovalDialog.tsx（扩展）
```

依赖方向：`agent → {permission, hook, jcs, tool, storage}`；`permission` 不 import `tool`/`workspace`（输入是规范化后的纯数据）；`hook` 不 import `agent`/`permission`（只操作事件与参数）；`storage` 实现 `permission.RuleStore`；Wails 类型仍不越过 `internal/desktop`。

## 4. 权限引擎

### 4.1 输入与输出

保持 Phase 2 `Request/Verdict` 形状兼容，新增字段：

```go
type Request struct {
    ToolRef, Name, Source, Trust, Action string
    InsideWorkspace, Sensitive, Destructive, Reversible bool
    MatchKey      string // 写：目录；MCP：tool_ref
    Preset        string
    ContentTrust  string // trusted | untrusted
    Now           time.Time
}
type Verdict struct {
    Decision, Level, Scope, MatchKey, Reason string
    Auto          bool // 无人工弹窗的放行
    QuotaExceeded bool
}
```

### 4.2 映射表

`Row` 以 `tool_ref` 为键，声明：`Action`、`Decision`、`Scope`（none/once/session/persistent）、`Level`（info/confirm/block）、`Conditions`、`Quota`、`Reversible`、`RetrySafe`。工具注册时 `permission.RequireRow(meta)` 校验 `Action/Reversible` 与行一致，缺失即启动失败（编程错误）。

| 行 | 决策 | 作用域 | 级别 | 条件/限额 |
|---|---|---|---|---|
| `builtin://read_file` / `list_dir`（工作区内） | allow | — | info | path_prefix=工作区；300 次/分 → ask |
| `builtin://write_file`（工作区内） | ask | once/session/persistent | confirm | 可逆；持久 30 次/分 → ask |
| 工作区外路径 | deny | — | — | 硬拒（Phase 3 不变） |
| 敏感路径 | deny | — | — | 硬拒，永不可开启 |
| MCP `read`（非 destructive） | ask | once/session | confirm | 60 次/分（session 命中时）→ ask |
| MCP `write/unknown/destructive` | ask | once | confirm/block | 不可逆；无持久 |
| `builtin://delete_file` / `shell` / 网络工具占位 | deny | — | — | 未注册可执行工具，仅兜底行 |
| 未声明工具 | deny | — | — | 审计 `unknown tool row` |

### 4.3 判定流程

1. 查行；未声明 → deny。
2. 应用会话预设覆盖（只收紧）。
3. 若决策为 ask、可逆、非不可信闩锁、存在持久规则 → allow（`Auto=true`）。
4. 条件评估：不满足则降一级（allow→ask，ask→deny）。
5. 可逆性修正：`Reversible=false` → 至少 Block、禁自动重试、进审批。
6. DIM4：若最终为 allow 且限额超限 → `OnExceed`（默认 ask；不可逆 deny）。
7. 内容信任：副作用且闩锁为真 → 作用域强制 once，忽略 session/persistent，级别至少 confirm。
8. 生成 reason 与 matchKey；返回唯一判定。

## 5. 预设

### 5.1 内置

| 名称 | 语义 | 收紧点 |
|---|---|---|
| `default` | Phase 2 行为 + 新引擎默认 | 无 |
| `readonly` | 只读助手 | 本地写/执行/删除/网络 deny；MCP 写/unknown deny；MCP 读仍 once/session |
| `cautious` | 最小复用 | 写与 MCP 读降为 once；禁用持久规则（`persistent_allowed: false`） |

### 5.2 YAML

```yaml
name: docs-only
description: 只允许写 docs 目录
rules:
  - tool: builtin://write_file
    decision: ask
    scope: once
    conditions:
      path_prefix: docs
      time_window: "09:00-18:00"
```

严格 schema：未知字段、未知工具、未知条件、重复 `tool` 都是错误；加载失败只禁用该文件并在 AppInfo 告警。校验"只收紧"：决策等级不得低于基础行、作用域不得更宽、条件不得减少、`persistent_allowed` 只能从 true 改 false。会话创建时校验预设存在；`sessions.preset` 落库，运行中不可切换（切换=新会话）。

## 6. 持久规则

- 表 `permission_rules(id, tool_ref, action, match_key, decision='allow', note, created_at, last_used_at, use_count)`；`UNIQUE(tool_ref, action, match_key)`。
- 仅当基础行为 ask 且 `Reversible=true` 且非 MCP 时，审批弹窗才出现"持久允许"；用户必须输入短语 `允许持久`。后端校验短语，错误返回可重试错误，不关闭弹窗。
- 命中：`matchKey` 完全相等；写审计与 `permission:decision` Info 事件；更新 `last_used_at/use_count`。
- 不适用：deny 行、不可逆工具、内容信任闩锁为真、预设 `persistent_allowed=false`。
- 界面列出规则（工具、目录范围、创建/最近使用、次数）并可删除；删除立即生效。
- 持久规则只放宽 ask→allow 一步；不改变作用域大小、不覆盖预设收紧。

## 7. DIM4 限额

- 键 = `sessionID + tool_ref + action + matchKey`；60 秒滑动窗口（时间戳切片，懒清理）。
- 只统计 `Verdict.Auto=true` 的放行；人工审批后的执行不计入（避免"批准一次后批量自动"）。
- 超限：可逆 → ask（弹窗注明"限额触发"）；不可逆 → deny；每次超限写事件与审计。
- 默认值见 §4.2；常量集中在 `permission/table.go`，单测固化。重启归零（桌面单用户可接受；Phase 4 评估持久化）。
- 不计 deny 与审批拒绝；不跨会话共享。

## 8. execHash 与 TOCTOU 管道

顺序与 architecture/04 一致，执行端锁定 hash：

1. 模型 ToolCall → Hook `before_tool_call`，收集 patches；每次 patch 写审计（旧/新值 hash，不落敏感原文）。
2. 合并 patches → 仅工具白名单顶层字段，最多 16 条；值类型限 string/number/bool/null；越界/未知字段拒绝该 Hook 并记审计。
3. 路径/域名规范化（ADR-0004），重新生成审批预览与 matchKey。
4. I-JSON 校验（UTF-8、无重复键、数字可精确表示）→ `execHash = sha256(JCS(finalArgs))`。
5. 权限引擎评估（§4）。
6. `before_approval` Hook 追加警告（只读）；审批弹窗展示最终参数、影响范围、信任警示；决策与 `execHash`、`content_trust` 落库。
7. 执行前：重算 `sha256(JCS(args)) == execHash`，不等 `ErrArgsDrift`；对路径工具再次 `Resolve` + 敏感检查 + 拒绝 symlink/junction/8.3 变化。
8. 执行；`after_tool_call` Hook 只读观测；结果落盘，`exec_hash` 写入 tool_calls。

残余风险：路径二次校验与 `open/rename` 之间仍有窗口；Phase 3 用 no-follow（平台可用时）+ reparse point 检查收窄，完整解决依赖 OS 沙箱（Phase 5）。

## 9. Agent 接线

- **信任闩锁**：`Session.turnUntrusted` 在每次 Submit 重置；工具结果 `trust=untrusted` 落盘时置真。审批 payload 带 `ContentTrust` 与 `TrustWarning`。
- **审批扩展**：`ApprovalPayload` 增加 `Preset`、`ContentTrust`、`PersistentAllowed`、`QuotaNotice`；`Approve(id, scope, phrase)` 支持 `persistent`；错误短语返回可重试错误。
- **事件**：新增 `session:permission`（toolRef/action/decision/scope/auto/quota/trust/reason），Info 与自动放行、限额/信任降级均发；deny 仍写 audit。前端工具块显示来源/信任/限额徽章。
- **状态机**：不新增状态；审批等待/暂停/恢复/取消语义沿用 Phase 2。Hook 超时/失败发生在 `thinking/running_tool` 内，不引入新状态。
- **启动**：加载预设与 Hook 配置；失败不阻塞应用，AppInfo 告警；Hook 配置指纹写日志。
- **会话创建**：`CreateSession(root, preset)`；未知预设拒绝；旧数据默认 `default`。

## 10. 迁移 0003

```sql
ALTER TABLE sessions ADD COLUMN preset TEXT NOT NULL DEFAULT 'default';
ALTER TABLE approvals ADD COLUMN exec_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE approvals ADD COLUMN content_trust TEXT NOT NULL DEFAULT 'trusted';
ALTER TABLE tool_calls ADD COLUMN exec_hash TEXT NOT NULL DEFAULT '';
CREATE TABLE permission_rules (
  id TEXT PRIMARY KEY,
  tool_ref TEXT NOT NULL,
  action TEXT NOT NULL,
  match_key TEXT NOT NULL,
  decision TEXT NOT NULL DEFAULT 'allow' CHECK (decision = 'allow'),
  note TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  last_used_at TEXT NOT NULL DEFAULT '',
  use_count INTEGER NOT NULL DEFAULT 0,
  UNIQUE (tool_ref, action, match_key)
);
CREATE INDEX idx_permission_rules_match ON permission_rules (tool_ref, action, match_key);
```

迁移幂等 + Phase 2 库升级测试；旧审批行 `exec_hash=''` 表示未锁定（只读历史），新审批必须非空。

## 11. 桌面与前端

- 新 API：`CreateSession(root, preset)`、`ListPresets`、`ListRules`、`DeleteRule`、`Approve(id, scope, phrase)`；AppInfo 增加 `Preset`、`HooksFingerprint`、`HooksError`、`PresetsError`。
- 新会话区增加预设下拉；顶栏显示当前预设；规则面板列出持久规则并可删除。
- 审批弹窗：持久选项仅在后端允许时出现；选择后显示输入框（占位 `允许持久`），未输入正确短语禁用确认；不可信来源显示黄条"由外部内容触发，仅本次"；限额触发显示原因。
- 所有新增文本仍走 `SafeText`；渲染安全门禁不放宽；新增组件纳入 vitest 注入断言。

## 12. 测试策略

| 层 | 用例 |
|---|---|
| 映射表 golden | 每行 × 预设 × 持久 × 条件 × 可逆 × 限额；最严合并；未声明 deny |
| JCS | RFC 8785 官方向量、键排序/Unicode/`-0`/指数边界、重复键与非 I-JSON 拒绝、fuzz 幂等 |
| TOCTOU | 审批后改参 → ErrArgsDrift；Hook 白名单外改参拒绝；测试 Hook 替换路径后执行前拒绝 |
| Hook | 五点顺序、优先级、超时、panic、fail_closed/deny/warn、after 只读、配置非法 |
| 限额 | 滑动窗口、超限 ask/deny、会话隔离、人工审批不计数、重启归零 |
| 持久 | 短语校验、跨会话命中、撤销、deny/不可逆拒绝、不可信降级 |
| 迁移 | Phase 2 库升级、幂等、旧行默认值 |
| 前端 | 预设/规则/审批短语/信任警示的 SSR 文本断言；渲染安全门禁 |

## 13. 风险与遗留

- JCS 依赖无 v1：固定伪版本 + wrapper + 向量/fuzz；升级需评审。
- Hook 与主进程同地址空间：编译期内置 + 配置白名单；外部形态另立 ADR。
- 配额重启归零：接受为防失控而非对抗性机制；Phase 4 评估持久审计。
- 路径残窗：二次校验 + no-follow 收窄，沙箱前不宣称完全消除 TOCTOU。
- 预设/规则 UI 信息量增加：保持灰盒，只做信息完整性；视觉精修仍 Backlog。
