# P-003-windowsgui 子系统下启动错误完全不可见

## 现象

双击 `bin/happywork.exe`（`-ldflags="-H windowsgui"` 构建）毫无反应、无窗口、无报错。控制台版（不带 `-H windowsgui`）同一份代码运行即打印出真实原因：`desktop: emitter must come from desktop.NewEmitter`——main.go 组装 `desktop.Options` 时漏传 `Emitter` 字段，`Run` 校验失败走 `log.Fatalf`，而 GUI 子系统下 stderr 是无效句柄，日志去向虚空。

## 根因

两个问题叠加：(1) 组装遗漏（nil 接口断言失败）；(2) windowsgui 二进制的 stderr/stdout 不落地，任何启动期 `log.Fatal`/panic 都是无声死亡，双击用户端零反馈。

## 修复

1. main.go：`emitter` 显式变量，同时传入 `agent.NewManager` 与 `desktop.Options.Emitter`；`Manager.Emitter()` 暴露同一实例供 desktop 绑定（`internal/agent/manager.go`、`internal/desktop/desktop.go`）。
2. main.go `newLogger()`：日志双写 stderr + `%AppData%/happywork/happywork.log`（追加）——GUI 下文件是唯一可追的痕迹（standards/06：日志无密钥，允许落盘）。
3. `desktop.Run` 的校验错误信息写明后果（"without it frontend events are silently dropped"），便于下次从文件日志直读病因。

## 预防

- 桌面 GUI 程序的启动日志必须落文件，不能只写 stderr；该约定已写入 cmd/agent/main.go 注释与 .agents/CONTEXT.md 关键提醒。
- 发布前用"控制台版"跑一遍启动冒烟（`go build -tags production` 不带 `-H windowsgui`），是排查 GUI 静默死亡的标准手法。

##（如复发）再次发生记录

- （无）

## 归属

Phase 1（2026-10-30）/ Windows / 首次双击验收发现，未入 main。
