# H-002-架构定稿与 Phase 1 骨架

- 日期：2026-10-30
- 范围：治理文档定稿批次（main）；Phase 1 最小骨架（feat/phase1-skeleton）

## 本轮做了什么

1. 定稿：architecture/01~04、roadmap/00、phase-1 合同转定稿；新增 `docs/design/phase-1/design.md`（施工设计）；ADR-0010（SQLite 驱动 = modernc.org/sqlite）；standards/04 附录 A 补齐前端依赖；main @ `0ce4134`
2. Phase 1 骨架（feat 分支 @ `f49a04c`）：wails3 v3.0.0 壳（Taskfile 适配 cmd/agent 布局）、SQLite 存储 + 幂等迁移、mock Provider（5 个内嵌剧本）、read_file + 工作区边界（越界审计）、单实例锁 + focus.req 聚焦、React 灰壳（CSP、textContent 渲染）
3. ci.yml 升级：golangci-lint 门禁 + windows/macos/linux 三平台 `wails3 build` 矩阵（A1；linux 允许首期红）
4. 工具链安装：wails3 CLI、golangci-lint 2.15（均进 GOPATH/bin）
5. 偏差回改：聚焦通道改请求文件+轮询（P-002）、messages 表补 tool 列、mock 剧本内嵌 + tool_result 防死循环分支、check-size 剪除 node_modules/bindings/dist

## 验证

- `gofmt`/`go vet`/`golangci-lint run`（0 issues）/`go test ./...`（6 包全绿，含边界/取消/批量/审计/锁等场景）
- 前端 `prettier`/`eslint`/`tsc --noEmit`/`vite build` 全绿
- `wails3 build` → `bin/happywork.exe`；`--help` flag 冒烟通过（GUI 未开）
- `scripts/check-size` 全绿（node_modules 误报已修）

## 遗留与下一步

- 无远端仓库：A1 的 CI 矩阵待所有者建库后首次运行
- GUI 真机验收（A2/A3/A4/A6）与 report.md 未做——见 CONTEXT.md 下一步
- macOS 构建/公证演练所有者侧

## 已提取的沉淀

- P-002（stdlib 不支持 Windows 命名管道监听 → 聚焦通道改请求文件+轮询）
