# ADR-0017：发布物三平台签名，`v*` 标签触发，真实凭据条件验收

- 状态：定稿（2026-10-30，随 Phase 4 立项放行）
- 日期：2026-10-30
- 关联：[architecture/04](../architecture/04-security-and-hooks.md) §应用更新；[phase-4-audit-rollback-signing.md](../roadmap/phase-4-audit-rollback-signing.md) A6/A7；[release-signing.md](../design/phase-4/release-signing.md)；[ADR-0001](ADR-0001-adopt-go-wails3.md)

## 背景

Wails 模板已提供 Windows `sign`、macOS `sign:notarize`、Linux `sign:deb/rpm` 任务，但仓库目前只有 CI 构建，没有任何发布流水线，也没有签名凭据。安全敏感的桌面应用需要可验证的补丁通道；未签名产物无法证明来源，macOS 还会被 Gatekeeper 拦截。另一方面，真实证书（Windows Authenticode、Apple Developer ID/公证、Linux GPG）需要所有者采购/注册，不能由代码会话代持。

## 决策

1. **只有 annotated tag `vX.Y.Z` 触发正式发布**；tag 必须等于 `build/config.yml` 的 `info.version`，否则失败。阶段标签 `phase-N.end` 不触发发布。
2. **平台签名**：Windows 对 exe 与 NSIS 安装包做 Authenticode 签名并 `signtool verify /pa /all`；macOS 用 Developer ID 签名、公证、staple，并 `codesign/spctl/stapler` 验证；Linux 用 GPG 签 `.deb/.rpm`、AppImage 与 `SHA256SUMS`，并 `gpg --verify` + `sha256sum -c`。
3. **凭据只存 GitHub Actions Secrets**：Windows PFX/密码、macOS 证书/钥匙串/Apple 公证凭据、Linux GPG 私钥/口令；不得入库、入日志、入产物。临时钥匙串/GNUPGHOME/证书文件在 job 结束删除。
4. **dry-run 验收**：`workflow_dispatch` 用临时自签证书（Windows）、ad-hoc 签名（macOS）、临时 GPG 密钥（Linux）验证流水线；不公证、不创建 Release，产物标记 preview。dry-run 不能替代真实平台信任链验证。
5. **fail-closed**：任一平台验签失败、缺少必需 secret、版本不一致或时间戳失败，整个 release 失败，不上传半套产物；禁止 `continue-on-error` 或"无签名也发布"。
6. **真实签名是条件验收 A7**：所有者提供凭据后跑一次正式 tag；未提供则记录遗留，不伪造证据。A6（dry-run）是阶段一票否决项。
7. **不做静默自动更新**：本阶段不实现应用内更新检查/下载器；发布说明提供手动下载与验签步骤。更新器若要做，另立 ADR 评估签名验证、回滚与网络面。
8. 不新增 Go 运行时依赖；使用 Wails 现有签名任务与平台工具。

## 理由

- 标签驱动 + 版本一致性校验，能防止"构建了 A、发布了 B"；阶段标签不触发，避免误发布。
- 三平台各自使用原生信任链：Authenticode、Apple 公证、GPG 是用户侧可独立验证的最小方案。
- 凭据与代码分离是供应链基本纪律；dry-run 让流水线在无证书时也能被测试，同时不冒充正式签名。
- 静默自动更新会把"用户知情"变成默认开启的后台通道，与产品原则冲突；先做签名与手工发布。

## 后果

- 积极：正式产物来源可验证；流水线可在无凭据时演练；发布失败不会留下半套产物；不引入运行时依赖。
- 代价：真实发布依赖所有者外部准备（证书、Apple 账号、GPG 密钥）；公证耗时与失败重试需要运维；证书到期/轮换需要更新 Secrets 与发布说明。
- 限制：不做 bit-for-bit 可复现构建；不提供 Windows EV/硬件令牌、Sigstore/透明日志、多签名身份；dry-run 产物不可作为正式发布。
- 重新评估条件：需要更高保障时，评估 Azure Trusted Signing、Apple API Key 公证、硬件令牌、Sigstore 或构建 provenance；引入前另立 ADR。
