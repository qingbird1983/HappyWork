# P-002-Go 标准库不支持监听 Windows 命名管道

## 现象

按 architecture/04 §单实例锁的既定方案，用 `net.Listen("pipe", \\.\pipe\happywork-focus)` 实现第二实例聚焦通知。Go 标准库 `net` 包中的 `"pipe"` 网络类型实为内存管道（`net.Pipe()`，见 `src/net/pipe.go`），不支持以该网络名监听/拨号 Windows 命名管道；`net.Listen("pipe", ...)` 会直接报错，编译期不暴露。

## 根因

标准库的命名管道支持仅存在于 `os` 层面的内部实现，未作为 `net` 的地址族开放；跨进程命名管道服务端需要 `Microsoft/go-winio` 之类的第三方库。

## 修复

聚焦通道降级为**请求文件 + 低频轮询**：第二实例写 `%AppData%/happywork/focus.req` 后退出，第一实例每 400ms 轮询 `os.Stat` 感知并聚焦窗口，消费后删除请求文件（`internal/singleinst/singleinst.go`）。亚秒延迟对"聚焦已有窗口"足够。

## 预防

- 涉及"stdlib 是否支持某平台原语"的架构表述，落码前先以最小程序验证（本例 5 行即可验证），再写入设计文档。
- architecture/04 §单实例锁与 design/phase-1 §9 已同步为请求文件方案；若未来延迟不达标，候选替代为 go-winio（需先走依赖准入）。

##（如复发）再次发生记录

- （无）

## 归属

Phase 1 开工首日（2026-10-30）/ Windows / 无线上影响（实现前发现，未产生坏提交）。
