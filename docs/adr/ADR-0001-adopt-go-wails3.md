# ADR-0001：采用 Go + Wails v3 构建桌面端 Agent

- 状态：定稿
- 日期：2026-10-30
- 关联：[research/01](../research/01-v2-design-review.md)；[architecture/01](../architecture/01-overview.md)

## 背景

桌面端 Agent 需要：权限/审批/审计等安全控制面用确定性代码实现；本地进程与文件系统操作能力强；三平台（Windows/macOS/Linux）可分发。2026-10-30 核实：Wails v3.0.0 已于 2026-09 转正（桌面三平台 stable、移动端实验性），要求 Go 1.26+。

## 决策

1. 技术栈：**Go 1.26+ + Wails v3（桌面）+ WebView 前端**。
2. **钉版本**：Wails 锁定 minor 版本线，升级视为常规变更但须经适配层回归测试。
3. 所有 Wails API 调用收敛在 `internal/desktop/` 适配层（`Platform` 接口），框架升级/回退只改这一层。
4. 移动端不在承诺范围（Wails v3 移动端为实验性）。

## 理由

- 安全控制面（权限引擎、TOCTOU 管道、Actor）用 Go 实现是本方案的立身之本；前端只做视图。
- 备选 **Electron/Tauri**：Electron 捆绑 Chromium 体积大且控制面需 Node 侧实现，与"控制面不可被 GUI 绕过"冲突；Tauri 用 Rust，团队栈不匹配。
- 备选 **纯 Go CLI**：不满足"桌面用户、审批弹窗、transcript 可视化"的产品定位。
- Wails v3 已 stable，且官方发布政策明确支持钉版本策略，框架风险可控。

## 后果

- 积极：单一 Go 二进制 + 系统 WebView，包体小；TS 绑定自动生成；三平台构建管线明确。
- 代价/义务：三平台 WebView 差异（WebView2/WKWebView/webkitgtk）要求渲染安全基线三平台一致验证；Linux 分发需携带 webkitgtk 依赖；CI 需三平台矩阵（Phase 4 建立）。
- 重新评估条件：Wails v3 桌面 API 出现破坏性变更且适配层无法吸收时。
