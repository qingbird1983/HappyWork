# ADR-0007：前端采用 React + TypeScript

- 状态：定稿
- 日期：2026-10-30
- 关联：[ADR-0001](ADR-0001-adopt-go-wails3.md)；[architecture/01](../architecture/01-overview.md)、[04](../architecture/04-security-and-hooks.md) §渲染安全；README 待决策表（本决策后移除该行）

## 背景

Phase 1 开工前需定前端框架（候选：React / Vue / Svelte / 原生 TS）。本项目前端特性：纯视图层（业务逻辑全在 Go 控制面）；核心难点是安全渲染不可信内容（markdown 禁 raw HTML、外链拦截、来源标记、流式 transcript 虚拟滚动）；开发方式为 AI 会话接力（AI 生成代码的可靠度是首要工程变量）；桌面 WebView 本地加载，bundle 体积无实际代价。

## 决策

1. 前端采用 **React + TypeScript**（Vite 构建，npm 管理，lockfile 入库），版本随 Wails v3 官方 React 模板。
2. 前端工具链纳入门禁：prettier（格式化）+ eslint + tsc（类型检查），CI 在 `frontend/package.json` 存在时生效。
3. markdown 渲染必须走默认不渲染 raw HTML 的渲染路径（react-markdown 或同等默认安全方案），外链统一走拦截确认通道（架构 04 基线，不因框架选择放松）。

## 理由

- **AI 接力开发**：React 模式最收敛、训练语料最充分，AI 生成代码歧义最少、排错资料最多——本项目开发方式的权重高于常规项目。
- **安全渲染生态最成熟**：react-markdown 默认不渲染 raw HTML，remark/rehype 插件（外链、来源标记、高亮）经大规模实战；与渲染安全基线天然契合。
- **体积论点不适用**：桌面 WebView 本地加载，React 运行时无网络加载代价。
- 备选 **Vue**：差距小（SFC 适合此规模），但上述两条略逊；若所有者个人偏好 Vue 可另写 ADR 取代本篇。
- 备选 **Svelte**：安全向生态最弱 + Svelte 4→5 大版本动荡，长期安全项目不取。
- 备选 **原生 TS**：为流式 transcript + 三级弹窗 + diff 视图手写组件系统，代码量反超框架方案，伪节约。

## 后果

- 积极：渲染安全基线有现成成熟组件支撑；AI 会话产出可靠度最高；虚拟滚动等需求有成熟方案。
- 代价/义务：前端依赖树比 Svelte/原生大（依赖准入表须登记 react/react-dom 及渲染插件）；eslint/prettier 配置随 Phase 1 落地。
- 重新评估条件：渲染安全基线在 React 生态内无法满足，或团队（人）栈明确转向 Vue。
