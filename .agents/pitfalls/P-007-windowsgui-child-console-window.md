# P-007-windowsgui-子进程弹出控制台窗口

## 现象

桌面应用启动 MCP stdio server（如 `python.exe`）时，屏幕上多出一个黑色控制台窗口；关闭它可能连带终止 server。

## 根因

windowsgui 子系统的父进程没有控制台，子进程是 console 子系统且未指定隐藏启动方式，Windows 会为它新建控制台。

## 修复

`internal/mcp/hide_windows.go` 为 `exec.Cmd.SysProcAttr` 设置 `HideWindow: true`；其他平台空实现。真机验证 `python` 子进程 `MainWindowHandle = 0`。终止时仅在优雅退出超时后的强制路径调用 `taskkill /T /F`，避免日常关闭也强杀整棵进程树。

## 预防

- 桌面壳拉起任何控制台程序，默认加隐藏窗口；验收看"进程句柄 + 屏幕"两项，不能只看日志。
- 强制终止是兜底路径，不要放在正常路径上无条件执行。

##（如复发）再次发生记录

- （无）

## 归属

Phase 2 实现/真机验收（2026-10-30）/ Windows / MCP 生命周期。
