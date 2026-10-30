# Phase 1：最小骨架

- 状态：定稿（2026-10-30）
- 日期：2026-10-30
- 关联：[00-roadmap.md](00-roadmap.md) §1；[ADR-0001](../adr/ADR-0001-adopt-go-wails3.md)；[architecture/01](../architecture/01-overview.md)、[02](../architecture/02-core-loop-and-data.md)

## 1. 目标

一个三平台可构建、可提交任务、流式显示、保存/恢复会话的最小应用壳：Wails v3 + SQLite + mock 模型 + 唯一一个只读工具，且桌面适配层与单实例锁从第一天就位。

## 2. 范围

### 做

- Wails v3 应用壳（单窗口）+ `internal/desktop/` 适配层（`Platform` 接口 + 30–50ms 事件批量 flush）
- SQLite 接入：sessions / messages / events 三表 + 幂等迁移框架
- mock Provider（确定性脚本化回复，支持流式分片），走统一 `model/provider.go` 接口
- 单工具 `builtin://read_file`（仅工作区内，硬编码最小权限检查——完整权限引擎在 Phase 3）
- 单实例锁（`os.UserConfigDir()` + gofrs/flock + 聚焦已有窗口）
- 前端壳：transcript 视图 + 输入框 + CSP 基线 meta
- `cmd/agent/main.go` 只装配

### 不做（明确排除）

- 真实模型 Provider（Phase 2）；审批流程与权限引擎（Phase 2/3）；MCP（Phase 2）；Hook（Phase 3）
- 备份回滚 / 沙箱 / 审计（Phase 4/5）；上下文折叠与压缩（Phase 2，mock 阶段无真实压力）
- 多窗口、多会话并行、设置界面（Backlog/后续阶段）

## 3. 开工前置条件

- [architecture/01](../architecture/01-overview.md)、[02](../architecture/02-core-loop-and-data.md) 转定稿（2026-10-30，所有者放行）
- `docs/design/phase-1/` 施工设计稿定稿（[design.md](../design/phase-1/design.md)，2026-10-30；前端框架已定 React + TS 见 [ADR-0007](../adr/ADR-0007-frontend-react.md)；依赖准入同步 standards/04 附录 A）
- 工具链就绪：Go 1.26+（本机 1.27 ✓）、wails3 CLI（2026-10-30 已装，对应 v3.0.0）、三平台构建环境（macOS 公证账号提前验证）

## 4. 交付物

| 类别 | 清单 |
|---|---|
| 代码 | `cmd/agent/`、`internal/{desktop,agent,model,tool,storage,singleinst}/`、`frontend/`、`migrations/` |
| 文档 | `docs/design/phase-1/` 设计稿；`report.md` 阶段进度报告 |
| 其他 | 三平台构建脚本；mock 剧本内嵌 `internal/model/mock/scripts/`（原定根级 fixtures/ 并入，见 design/phase-1 §3） |

## 5. 验收场景（逐条可执行；验收结果记入 [report.md](../design/phase-1/report.md)——7/7 通过）

- [x] A1：Windows / macOS / Linux 三平台 `wails3 build` 全部通过（macOS 含公证流程演练）【一票否决】
- [x] A2：提交任务 → mock 模型流式回复 → UI 流式显示（事件批量 flush，无逐 token 卡顿）
- [x] A3：退出重开应用 → 会话列表与 transcript 完整恢复（transcript 重放，不依赖 UI 缓存）
- [x] A4：启动第二实例 → 检测锁 → 第一实例窗口被聚焦 → 第二实例自动退出（Windows 实测）
- [x] A5：`read_file` 读工作区内文件成功；读工作区外文件被拒且留痕
- [x] A6：前端 CSP 生效——注入 `<script>` 的模型输出不被执行
- [x] A7：`go vet` / `golangci-lint` / `go test ./...` / `scripts/check-size` 全绿（CI 门禁四连）

## 6. 依赖与风险

| 依赖/风险 | 说明与缓解 |
|---|---|
| 前端工具链落地（React 已定，ADR-0007） | Phase 1 设计稿登记 eslint/prettier/tsc 配置与依赖准入 |
| Wails v3 API 变更 | 钉 minor 版本；所有调用走适配层（ADR-0001） |
| Linux webkitgtk 环境 | A1 首次验证即暴露；AppImage/deb 打包方式在本阶段定 |
| gofrs/flock 依赖准入 | 引入前登记 standards/04 附录 A |
