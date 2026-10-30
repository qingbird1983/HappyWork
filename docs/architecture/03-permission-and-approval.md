# 03-权限模型与审批

- 状态：评审中
- 日期：2026-10-30
- 关联：[01-overview](01-overview.md)；[ADR-0002](../adr/ADR-0002-unified-permission-model.md)、[ADR-0003](../adr/ADR-0003-dual-fingerprint.md)、[ADR-0004](../adr/ADR-0004-path-canonicalization.md)；[04-security-and-hooks](04-security-and-hooks.md) §TOCTOU

## 定位与职责

权限引擎是控制面里**每个工具调用的唯一闸门**：输入规范化后的调用请求，输出唯一确定的判定（放行 / 拒绝 / 人工审批 + 作用域 + 条件 + 限额）。不做模型调用、不做工具执行——它是纯判定函数，可独立做表驱动测试。

> **v3 关键修订**：v2 存在两套并行系统（§5.2 RBAC+ABAC 角色策略 与 §6 四维审批+工具映射表），无冲突消解规则。v3 **合并为一套**：工具映射表是唯一事实源，"角色"降级为预设包（[ADR-0002](../adr/ADR-0002-unified-permission-model.md)）。独立的 ABAC 引擎不再存在，其能力（路径/域名/时间窗条件）并入条件维度。

## 权限三元组与判定输入

`Permission = 〈主体, 客体, 动作〉`

- **主体**：会话（含其绑定的角色预设）
- **客体**：工具引用（`builtin://read_file`、`mcp://fs/get_file`）+ **规范化后**的资源路径/域名
- **动作**：受限枚举 `read / write / execute / delete / network`

## 四维正交审批模型

| 维度 | 含义 | 取值 |
|---|---|---|
| **DIM1 决策** | 放不放行 | `allow` / `deny` / `ask` |
| **DIM2 作用域** | 批准后管多久 | `once` / `session`（绑定 **matchKey**，见下）/ `persistent`（写 kv，需显式二次确认） |
| **DIM3 条件** | 决策成立前提 | 路径前缀（工作区内/外）/ 域名白名单 / 资源敏感级 / 时间窗 / 内容信任级 |
| **DIM4 限额** | 自动放行的量控 | 速率/次数上限（按会话隔离计数）；超限升级 `→ ask` 或 `→ deny` |

每个调用经四维求值后**有唯一确定的判定结果**——这是 v2 把"七级审批"重构为四维的动机，v3 保留。

## 双指纹分离（v3 核心修订）

> v2 缺陷：session 作用域绑定**全参数**指纹——写文件/读文件类工具每次调用参数必不同（路径、内容各异），session 作用域**永远不会命中第二次**，用户被逼向 `persistent`（更宽、更危险），恰恰制造了要防的审批疲劳。详见 [research/01](../research/01-v2-design-review.md)。

**信任的粒度和锁定的粒度是两个问题，用两个指纹：**

| 指纹 | 构成 | 用途 | 变化时 |
|---|---|---|---|
| **matchKey（匹配指纹，粗）** | `tool_ref + 动作 + 规范化路径前缀或域名前缀`，**不含**文件内容等易变字段 | session 作用域的匹配键："本会话内写 `workspace/docs/` 下不再问" | matchKey 变 → 需重新审批 |
| **execHash（执行锁定，细）** | `sha256(JCS(全参数))`，规范见 [04](04-security-and-hooks.md) §TOCTOU | 审批与执行的一致性锁定：执行器校验不等即拒（`ErrArgsDrift`）；写审计与 approvals 表 | 任何参数变化必然改变 |

MCP 工具同样适用：`mcp://gitlab/read_issue` 的 matchKey 按 schema 声明的资源字段（如 project）构造，schema 未声明资源字段的工具 matchKey 退化为 `tool_ref`（更严，session 作用域对它等于 once）。

## 路径规范化管线（v3 新增，[ADR-0004](../adr/ADR-0004-path-canonicalization.md)）

> v2 缺陷：hash 锁的是**参数字符串**，不是参数指向的资源。审批时展示 `workspace/docs/a.txt`（前缀检查通过），执行时它是软链接/junction 指向 `~/.ssh/id_rsa`；Windows 还有大小写不敏感、8.3 短名（`PROGRA~1`）、UNC、`..` 拼接等逃逸向量。

**所有作为客体的路径，必须在权限评估之前完成规范化：**

1. `filepath.Clean`（消 `..`、重复分隔符）；
2. `filepath.EvalSymlinks`，失败即拒绝（可疑链接）；解析后**再次**检查是否仍在允许前缀内（防链接目标逃逸）；
3. Windows：大小写归一（NTFS 不区分大小写）；拒绝 8.3 短名（检测 `~` 数字模式）与目录 junction；UNC 路径一律视为"工作区外"；
4. 工作区根在**会话启动时**规范化一次并固定（含校验其本身非链接目标变更）；会话中途工作区根变化视为新会话；
5. 域名客体同理：小写化、去尾部点、Punycode 归一后再做白名单前缀匹配。

规范化结果写入 `finalArgs`（审计可见原始值与规范化值），matchKey 与 execHash 均基于规范化后参数计算。

## 工具默认映射表（唯一事实源）

每个工具注册时必须声明下表的一行，**代码里不允许出现"未声明默认值"**。角色预设只能收紧不能放宽（见下节）。"可逆"定义见 [04](04-security-and-hooks.md) §可逆性。

| 工具类别 | 可逆 | 决策 | 作用域 | 条件 | 限额/升级 |
|---|---|---|---|---|---|
| 本地只读（工作区内） | 是 | `allow` | — | path_prefix 命中 | — |
| 本地只读（工作区外） | 是 | `ask` | once | — | — |
| 本地写（工作区内） | 是 | `ask` | session + matchKey | path_prefix 命中 | 先 preview 再落盘 |
| 本地写（工作区外 / 覆盖） | 是 | `ask` | once | — | — |
| 本地删除 | 否 | `deny` | — | 需显式开启 | 开启后仍 once + 进回收站 |
| shell 执行 | 否 | `deny` | — | 需显式开启 | 开启后逐条 ask |
| 网络读（域名白名单） | 是 | `allow` | — | domain 命中 | N 次/分，超限 `→ ask` |
| 网络写 / 外部 API 副作用 | 否 | `ask` | once | — | 禁止自动重试 |
| MCP 只读 | 是 | `ask`（首会话） | session + matchKey | schema 白名单 | — |
| MCP 写 / 删除 | 否 | `ask` | once | schema 白名单 | 禁止自动重试 |
| 凭据访问 / 密钥读取 | 否 | `deny` | — | — | 不可开启 |

判为不可逆的工具一律不走自动放行通道、禁止自动重试、审批弹窗升为 Block 级。

## 判定流程

```go
func (e *PermissionEngine) Evaluate(sub, obj, act, attrs) Decision {
    // 0. 客体已规范化（前置硬约束，见上节）
    // 1. 查工具映射表默认行（角色预设已折算进表：预设只收紧）
    d := e.lookupToolDefault(obj, act)
    // 2. 条件评估（DIM3）：不满足时降一级——allow→ask，ask→deny
    if !d.Cond.Satisfied(attrs) { d.Downgrade() }
    // 3. persistent 规则匹配（用户显式保存的规则，matchKey 相同才命中）
    if r := e.matchPersistent(sub, obj); r != nil { d = strictest(d, r) }
    // 4. 可逆性修正：不可逆 → 强审批（Block 级）+ 禁自动重试
    if !attrs.Reversible { d = forceStrongApproval(d) }
    // 5. 限额（DIM4）：超限按 OnExceed 升级
    if d.Quota != nil && e.quota.Exceeded(sub, obj) { d.Effect = d.OnExceed }
    return d
}
```

**合并规则（消解一切冲突）**：多条规则并存时取**最严者**（`deny > ask > allow`）；作用域取最小；条件全取交集。角色预设与映射表冲突时同规则处理——这就是"预设只能收紧"的形式化。

## 角色预设（preset）

角色（如 `只读助手`、`代码开发者`）= 一组工具映射表的**收紧覆盖** + 默认条件（如 `path_prefix` 限定项目根、`time_window` 限定时段），以 YAML 声明、启动时 schema 校验（非法即拒绝加载整份配置，v2 示例曾出现重复键的非法 YAML）。预设**不能**把映射表中的 `ask` 改 `allow`、不能缩小 once 为 session 之外的放宽、不能覆盖 `deny` 行。

## 审批弹窗三级

| 级别 | 语义 | 交互 |
|---|---|---|
| `Info` | 已自动放行，仅通知 | 无操作，transcript 回看 |
| `Confirm` | 必须用户点击 | 展示**人可读的**最终参数、影响范围（路径/域名）、可逆性、matchKey 覆盖范围 |
| `Block` | 高危默认阻断 | 展示风险原因；覆盖须**输入确认短语**而非单击 |

> v3 修订：弹窗**不再展示 execHash**（v2 缺陷：用户无法验证一个 sha256，纯噪音、制造虚假安心）。hash 留在 approvals 表与执行器校验里；弹窗展示的是参数 diff 与影响范围。取舍：如果所有动作都 Ask Always，用户疲劳后等于没有审批——按风险与可逆性分级，"确认"才保持有意义。

## 风险与待决

| 风险 | 对策 |
|---|---|
| matchKey 粒度不当（过宽→复用滥用，过窄→疲劳） | 按工具类别定粒度（映射表）；Phase 3 用 golden tests 固化 |
| 规范化本身性能开销（每次 EvalSymlinks） | 路径规范化结果按会话缓存（根固定后失效少） |
| persistent 规则被注入内容滥用 | persistent 需二次确认 + 可查可删 + 每次命中记 Info 事件 |
| Windows 大小写/短名规则遗漏新向量 | 规范化管线单独发包 + 模糊测试（04 §测试基线） |

待决：matchKey 是否纳入"内容信任级"维度（外部内容触发的写操作自动降级为 once）——倾向纳入，Phase 3 评审定（[research/01](../research/01-v2-design-review.md) 建议）。
