# ADR-0010：SQLite 驱动采用 modernc.org/sqlite（纯 Go，免 CGO）

- 状态：定稿
- 日期：2026-10-30
- 关联：[ADR-0001](ADR-0001-adopt-go-wails3.md)；[architecture/02](../architecture/02-core-loop-and-data.md) §数据模型；roadmap Phase 1 验收 A1

## 背景

Phase 1 引入 SQLite 持久化（sessions / messages / tool_calls / events 等表）。Go 生态两个主流驱动：

- `mattn/go-sqlite3`：事实标准、功能完整，但基于 CGO。
- `modernc.org/sqlite`：SQLite C 源码机器转译为纯 Go，免 CGO，`database/sql` 兼容。

决策触发点：Phase 1 验收 **A1 要求三平台 `wails3 build` 全部通过（一票否决）**。CGO 工具链（Windows mingw、macOS 交叉/公证链、CI 缓存）是三平台构建矩阵的主要不确定项。

## 决策

采用 `modernc.org/sqlite`（v0.x 主线）。约束：

1. 驱动引入点隔离在 `internal/storage/` 单文件（`sql.Register` 处），业务代码只依赖 `database/sql` 标准接口。
2. 全项目维持 `CGO_ENABLED=0` 可构建——禁止任何包引入 CGO 依赖（含 Wails 的 Windows 后端，其 v3.0.0 在 Windows 本就无 CGO）。
3. 启用 WAL + `busy_timeout`，单写者模式（架构 02 既定）。

## 理由

1. **A1 是硬门槛**：免 CGO 直接消除三平台各自的 C 工具链配置与交叉编译问题，把最大的构建风险清零。
2. **性能足够**：桌面单用户、单写者 + WAL，写入压力远低于两版驱动的差异区间。
3. **一致性**：wails v3 Windows 后端无 CGO，全项目可保持纯 Go 构建，`go build` 缓存与 CI 都受益。

## 后果

- 正面：三平台构建与 CI 简化；交叉编译可行；驱动替换点收敛在一个文件。
- 负面：纯 Go 转译版写吞吐低于 CGO 版（本场景不敏感）；二进制体积增加数 MB；个别高级特性（自定义 collation、扩展加载）不可用——本项目表结构只用基础特性，不受影响。
- 风险对冲：若未来确需 CGO 版（如性能实测不达标），替换点在 `internal/storage`，业务层零改动。

## 备选与否决

- `mattn/go-sqlite3`：否决——CGO 工具链在三平台 CI 的配置与维护成本与 A1 目标直接冲突。
- 嵌入式 KV（bbolt 等）：否决——架构 02 已定关系模型（多表 + transcript 重放查询），换存储属于重新设计而非选型。

## 更新记录

- 2026-10-30：定稿。
