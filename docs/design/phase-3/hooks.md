# Phase 3 Hook 设计：受控的进程内扩展

- 状态：定稿（2026-10-30；所有者放行，随立项生效）
- 日期：2026-10-30
- 关联：[phase-3-permission-engine-and-hooks.md](../../roadmap/phase-3-permission-engine-and-hooks.md)；[architecture/04](../../architecture/04-security-and-hooks.md) §Hook 系统；[ADR-0013](../../adr/ADR-0013-hook-trust-model.md)；主设计 [design.md](design.md)

## 1. 定位与信任模型

Hook 是主循环上的**受控观察/改参点**，不是插件市场：Phase 3 只允许编译进二进制的 Go 实现，配置只能按名字选择并参数化。没有动态代码、脚本或外部进程；因此本期不做签名/TOFU（[ADR-0013](../../adr/ADR-0013-hook-trust-model.md)）。配置的 SHA-256 指纹写入启动日志，供跨会话比对，但不宣称"可信/已签名"。

红线：Hook 不能伪造审批、不能直接调用工具、不能在审批后改参、after 点只读、不能访问存储/密钥；panic/超时不得影响 GUI 与主循环。

## 2. Hook 点

| 点 | 时机 | 可见数据 | 允许改参 | 允许阻断 |
|---|---|---|---|---|
| `before_model_call` | 组装 provider 请求后、发送前 | 会话、回合、消息摘要 | 否 | 是（拒绝本次调用） |
| `after_model_call` | 流式结束、落盘前 | 文本、tool_calls、usage | 否 | 否（只读） |
| `before_tool_call` | 参数解析后、规范化前 | tool_ref、原始参数 | **是（白名单）** | 是 |
| `after_tool_call` | 工具返回后、落盘前 | 结果摘要、状态、时长 | 否 | 否（只读） |
| `before_approval` | 权限判定 ask 后、弹窗构造前 | 最终参数摘要、风险、matchKey | 否（只加警告） | 是（降为 deny） |

内容只给摘要/必要字段，不把完整 transcript 交给 Hook；after_model 的文本以流式累计结果为准，Hook 不得改写。

## 3. Go API

```go
type Point string // before_model_call | after_model_call | before_tool_call | after_tool_call | before_approval

type Event struct {
    Point     Point
    SessionID string
    TurnID    int64
    ToolRef   string
    ToolName  string
    Action    string
    Trust     string          // trusted | untrusted
    Args      map[string]any  // 仅 before_tool_call 可改
    Warnings  []string        // 只追加
}

type Patch struct {
    Field string
    Value any // string | number | bool | null
}

type Outcome struct {
    Patches    []Patch
    Warnings   []string
    DenyReason string // 非空即拒绝
}

type Hook interface {
    Name() string
    Points() []Point
    Priority() int // 数字小者先执行；同优先级按注册名排序
    Handle(ctx context.Context, ev *Event) (Outcome, error)
}
```

Runner 只把 `Outcome` 当作建议：最终是否接受由 runner 按白名单、数量、类型与失败策略裁决；Hook 无法直接修改工具、存储或审批通道。

## 4. 配置

位置：`%AppData%/happywork/hooks.d/*.yaml`（本地用户数据，不入库）。无目录/无文件 = 不启用任何 Hook。

```yaml
hooks:
  - name: secret-scan        # 必须是已编译的内置 Hook
    enabled: true
    priority: 100
    timeout_ms: 500
    on_error: fail_closed    # warn | deny | fail_closed
    params:
      mode: block            # warn | block
```

严格 schema：未知 Hook 名、未知字段、重复 name、非法 timeout（>5000ms）、未知 params 键、YAML 重复键都是错误。任一文件非法 → **整份 Hook 配置禁用**（不半加载），AppInfo 显示"Hook 配置错误"，应用照常启动；启动日志记录错误与已加载配置的 SHA-256 指纹。

## 5. 执行语义

- **顺序**：按 `priority` 升序，同级按 name；同一 Hook 可声明多个点，每个点单独注册。
- **超时**：默认 500ms，最大 5s；超时按 `on_error` 处理，并写审计。
- **panic**：recover 后视为 error，按 `on_error` 处理；记录 Hook 名与点，不打印参数原文。
- **失败策略**：
  - `warn`：记日志/审计，继续（before_tool 的改参丢弃）。
  - `deny`：before 点拒绝本次工具/模型调用；after 点只能降级为警告（副作用已发生）。
  - `fail_closed`：同 deny，并额外写审计 `hook fail_closed`；before_model 失败结束本次 run 为 error。
- **改参**：仅 `before_tool_call`；字段必须在工具 `Meta.PatchableFields` 白名单内（Phase 3：`builtin://write_file` 仅 `content`；MCP 默认空）；最多 16 条；禁止嵌套路径；值类型限 JSON 标量。
- **改参审计**：每条 patch 写 `hook_patch` 审计（Hook 名、字段、旧值 SHA-256、新值 SHA-256），不落原文；路径字段若被允许修改，必须重新规范化并重建审批预览。
- **after 只读**：返回的 Patches 被拒绝并记审计；Warnings 可展示，DenyReason 忽略。
- **审批前**：`before_approval` 只能追加 Warnings 或 DenyReason；DenyReason 使本次调用以 deny 结束（写审计），不进入弹窗。

## 6. 内置 Hook：secret-scan

- 点：`before_tool_call`、`before_approval`。
- 参数：`mode: warn|block`（默认 warn）、`patterns: default`（PEM 私钥、常见云/代码平台 token 前缀、JWT 形态）。
- 行为：扫描参数中的字符串值；命中只记录**模式名与字段名**，绝不记录命中内容。
  - before_tool：加 Warning；`block` 模式下 DenyReason（工具不执行）。
  - before_approval：加 Warning；`block` 模式下弹窗升级为 Block 级（需输入确认短语）。
- 默认不启用；示例配置见 §4。它是 Hook 框架的参考实现，不替代敏感路径硬拒。

## 7. 可观测

- 审计：`hook_patch`、`hook_deny`、`hook_error`、`hook_fail_closed`（session、turn、hook、point、reason）。
- 日志：Hook 名/点/耗时/错误；配置指纹；不打印参数与结果原文。
- 事件：不新增高频事件；审批弹窗通过 payload `Warnings` 展示；AppInfo 提供 `HooksFingerprint` 与 `HooksError`。

## 8. 测试

- 顺序与优先级：两个 fixture Hook 记录调用顺序。
- 超时：阻塞 Hook 在 timeout 后按策略处理，主循环不挂起。
- panic：recover 后按策略处理。
- 改参：白名单内接受并审计；白名单外/嵌套/超数量拒绝；after 改参被拒。
- 配置：未知名字/字段/重复/非法 timeout/重复键 → 整份禁用；指纹稳定（文件顺序无关）。
- secret-scan：命中不泄漏原文；warn/block 两种模式；before_approval 升级 Block。

## 9. 非目标与未来

- 不做外部进程 Hook、WASM/插件、动态脚本、自定义工具注入。
- 不做签名/TOFU、Hook 市场、热加载；配置变更需重启。
- 未来若引入外部 Hook：独立 ADR，先定进程隔离、资源限制、签名与吊销，再接入同一条 runner 接口；after 仍只读。
