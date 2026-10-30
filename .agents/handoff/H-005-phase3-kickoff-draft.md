# H-005 Phase 3 立项草案

- 日期：2026-10-30
- 主题：权限引擎、TOCTOU/execHash、编译期内置 Hook 的范围与设计草案，待所有者过目

## 本轮做了什么

- 起草 `roadmap/phase-3-permission-engine-and-hooks.md`：目标、做/不做、A1~A8、风险与前后阶段边界。
- 起草 `design/phase-3/design.md`：引擎映射表、预设、持久规则、DIM4、JCS execHash/TOCTOU、迁移 0003、Agent/桌面/前端接线、测试策略。
- 起草 `design/phase-3/hooks.md`：五个 Hook 点、Go API、严格 YAML、优先级/超时/panic/失败策略、改参白名单、内置 secret-scan。
- 落 ADR 草案：ADR-0012（JCS 参考实现）、ADR-0013（Hook 仅编译期内置，签名延后）、ADR-0014（内容信任级为条件，不并入 matchKey）。
- 调研 JCS 实现并在临时模块验证：`cyberphone/json-canonicalization`（Apache-2.0，伪版本 v0.0.0-20261011152517-32a48195b9b2）；YAML 选 `gopkg.in/yaml.v3`（MIT/Apache-2.0）。未进入 go.mod。
- 更新 docs/README、roadmap（草案评审中）、CHANGELOG 0.1.010、CONTEXT。

## 验证

- `bash scripts/check-size` 通过（docs 42 篇，.agents 15/40）。
- 未写实现代码；未跑实现门禁（按文档先行纪律）。

## 遗留与下一步

1. 所有者过目 6 项决策（JCS、Hook 信任、内容信任级、持久规则、预设、限额/范围）。
2. 批准后：文档转定稿、roadmap 改进行中、登记两项依赖、在 `feat/phase3-permission-engine` 开工。
3. 实现顺序与验收映射见 CONTEXT「下一步」；A4/A8 为一票否决。

## 已提取的沉淀

- 复用 P-005~P-008；本轮无新踩坑（纯文档与依赖调研）。
