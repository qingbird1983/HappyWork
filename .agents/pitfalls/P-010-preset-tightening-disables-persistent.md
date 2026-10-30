# P-010-预设收紧作用域后不得再提供持久选项

## 现象

自定义预设把 `builtin://write_file` 收紧为 `decision: ask, scope: once`，引擎仍返回 `PersistentAllowed=true`，审批弹窗继续显示"持久允许"。这等于允许用户用持久规则绕过预设收紧，违背"预设只能收紧"。

## 根因

`PersistentAllowed` 只检查了行的 `Persistent` 标志和可逆性，没有把收紧后的 `Scope` 纳入判断；内置 `cautious` 通过 `persistent_allowed: false` 单独关闭，掩盖了通用缺口。

## 修复

- `PersistentAllowed` 增加 `scopeRank(row.Scope) >= scopeRank(ScopeSession)`；once 收紧自动关闭持久。
- 不可信内容闩锁仍强制 once 并关闭持久。
- 回归：`TestPresetScopeOnceDisablesPersistent`、`TestPersistentRuleLifecycle`。

## 预防

- 任何"额外放宽通道"（session/persistent/自动放行）都必须显式检查收紧后的作用域，而不是只检查基础行。
- 预设新增收紧维度时，同步问一句：这个维度会不会让某个放宽通道重新变宽？

##（如复发）再次发生记录

- （无）

## 归属

Phase 3 真机验收（2026-10-30）/ 权限合并语义。
