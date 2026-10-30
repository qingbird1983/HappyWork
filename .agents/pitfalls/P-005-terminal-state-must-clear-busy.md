# P-005-终态未清 busy 标志导致立即发送被拒

## 现象

run 完成后立即 Send，偶发返回 `ErrBusy`；UI 上会话已显示"已完成"，用户却要先等一下才能继续。

## 根因

`running` 标志在 actor 循环退出 `run()` 之后才清除，而终态事件先于清理发出；前端收到 `done` 立即允许输入，请求落在两步之间。

## 修复

`Session.setState` 在写入终态时同步清除 `running`（`internal/agent/session.go`）；actor 循环末尾再次清理（幂等）。回归：`TestStateTransitionsIncludePersisting` 与完成后立即 `Send` 的用例。

## 预防

- "对用户可见的完成"与"内部可再次接单"必须是同一时刻；任何终态事件发出前，先让状态机自身可重入。
- 不要把资源清理放在事件广播之后才做。

##（如复发）再次发生记录

- （无）

## 归属

Phase 2 实现/真机验收（2026-10-30）/ actor 状态机。
