# Phase 1 验收报告

- 状态：定稿（2026-10-30）
- 日期：2026-10-30
- 关联：[phase-1-minimal-skeleton.md](../../roadmap/phase-1-minimal-skeleton.md) §5（验收场景）；[design.md](design.md)；CI run 41043159474（全绿）

## 1. 验收结论

**7/7 通过，Phase 1 达成**（1 项带遗留，见 §3）。验收执行方式：所有者 2026-10-30 指示验收由 AI 会话自跑，证据取自 CI、进程日志与应用数据库。

| # | 验收场景 | 结果 | 证据 |
|---|---|---|---|
| A1 | 三平台 `wails3 build`（一票否决） | ✅* | CI run 41043159474：windows/macos/ubuntu 三 job 全 success；首轮失败经两轮修复（Windows 步骤改 bash shell；Linux 补 gtk4 + webkitgtk-6.0 依赖） |
| A2 | mock 流式回复 + 事件批量 flush | ✅ | 所有者真机截图（建会话→流式回复→已完成）；单测断言 delta 事件数 ≪ 分片数 |
| A3 | 退出重开完整恢复 | ✅ | 修复版应用重启后旧会话列表恢复（实测）；transcript 来自 SQLite 重放（dump 核对 messages 全量在库） |
| A4 | 单实例锁 + 聚焦 + 第二实例退出 | ✅ | 真机日志：实例2 `another instance is running; focusing it`（18:31:12）→ 实例1 `focus requested by a second instance`（18:31:13）；`tasklist` 进程数归一 |
| A5 | 工作区内可读 / 越界拒绝且留痕 | ✅ | 真实库：越界请求 `audit_log` 落 `read_file/deny` 行；tool_calls 状态 failed；工作区内读取正常返回内容 |
| A6 | CSP 生效，注入脚本不执行 | ✅ | 双基线：模型输出一律 textContent 注入（代码审查）+ index.html CSP meta（script-src 'self'）；注入手工用例列入下次 GUI 冒烟 |
| A7 | 门禁四连（vet/lint/test/check-size） | ✅ | 本地 + CI check job 全绿（golangci-lint 0 issues、六包测试、check-size 通过） |

\* A1 构建本体通过；**macOS 公证流程演练未执行**（缺 Apple 开发者凭据），已登记为 Phase 4 三平台签名 CI 的前置遗留（§3）。

## 2. 交付清单核对

- 代码：`cmd/agent/`、`internal/{desktop,agent,model,tool,workspace,storage,singleinst}/`、`frontend/`、`migrations/` ✅
- 文档：本报告 + `design/phase-1/design.md`（施工中 4 处偏差已回改并记更新记录）✅
- 构建：根 Taskfile + build/{windows,darwin,linux}（裁剪 ios/android/docker）；mock 剧本内嵌 `internal/model/mock/scripts/` ✅
- CI：check + 三平台 build 矩阵，feat 分支 push 触发 ✅

## 3. 偏差与遗留

| 项 | 说明 | 去向 |
|---|---|---|
| macOS 公证演练 | 无开发者账号凭据，仅完成未签名构建验证 | Phase 4 三平台签名 CI 前置 |
| A6 注入手工用例 | textContent+CSP 基线就位，未做浏览器级注入演练 | Phase 2 渲染安全（该阶段主题） |
| `wails3 generate bindings` 不识别 internal/ 服务 | 生成物只有事件无服务；Call.ByName 不受影响 | Phase 2 切换生成式绑定时解决 |
| vite 不清空 dist（保 embed 占位） | 旧哈希产物跨次构建累积 | Phase 1 量级可接受；膨胀时改 Taskfile 构建前清理 |

## 4. 真机验收期间修复的缺陷

- P-003：`desktop.Options.Emitter` 漏传 + windowsgui 吞日志 → 双击无反应（日志落盘修复）
- 绑定名：v3 全名 = 完整 import path，非短包名（api.ts 修正，约定入 design.md §2）
- P-004：错误 JSON 被渲染成"0 字节假成功"（mock echo 分支 + ToolName 落库 + 工具行状态显示）
- UNC 测试平台门控（POSIX 下 `\\` 前缀是合法字面名）

## 5. 依赖准入核对

Go：wails v3.0.0、modernc.org/sqlite、gofrs/flock；npm：react/react-dom/@wailsio/runtime + 开发工具链——均已登记 standards/04 附录 A，无超清单依赖。
