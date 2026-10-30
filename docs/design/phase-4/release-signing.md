# Phase 4 发布签名设计：三平台签名、公证与验签

- 状态：定稿（2026-10-30，所有者放行 D1~D6）
- 日期：2026-10-30
- 关联：[phase-4-audit-rollback-signing.md](../../roadmap/phase-4-audit-rollback-signing.md)（A6/A7）；[ADR-0017](../../adr/ADR-0017-release-signing.md)；[architecture/04](../../architecture/04-security-and-hooks.md) §应用更新；[design.md](design.md) §8

## 1. 目标与非目标

目标：`v*` 标签触发可重复的三平台发布流水线，产物带平台签名并可被用户/CI 验证；凭据不进入仓库、日志或构建产物。

非目标：应用内更新检查/下载器、静默自动更新、MSIX、多签名身份、密钥轮换 UI、Sigstore/透明日志、bit-for-bit 可复现构建。真实证书采购与 Apple 账号由所有者准备（A7 条件项）。

## 2. 触发与版本

- **正式发布**：推送 annotated tag `vX.Y.Z`（如 `v0.1.0`），`release.yml` 触发；tag 去掉 `v` 必须等于 `build/config.yml` 的 `info.version`，否则 `prepare` 失败。
- **dry-run**：`workflow_dispatch` 输入 `dry_run=true`（默认 true），构建/签名/验签但不创建 Release、不推送产物到公开 Release；用于 A6。
- **阶段标签**（`phase-N.end`）不触发发布。
- 版本号变更流程：更新 `build/config.yml` + `CHANGELOG.md` → 提交 → 打 tag；tag 与配置文件不一致即失败，不做自动改写。

## 3. 工作流结构

```yaml
name: Release
on:
  push:
    tags: ["v*"]
  workflow_dispatch:
    inputs:
      dry_run:
        type: boolean
        default: true
permissions:
  contents: read
jobs:
  prepare:      # 版本一致性 + 全量测试 + 生成校验清单
  windows:      # build → sign exe → NSIS → sign installer → verify
  macos:        # build → sign app → notarize → staple → dmg → verify
  linux:        # build → deb/rpm/appimage → gpg sign → SHA256SUMS → verify
  release:      # 仅正式 tag；下载产物、创建 GitHub Release、上传
```

- 各构建 job 使用 `actions/upload-artifact`；`release` 使用 `actions/download-artifact` 与 `gh release create`，`permissions: contents: write` 只在 `release` job 声明。
- 所有第三方 action 固定 major 版本；Go/Wails 版本与 CI 一致（Go `>=1.26`、`wails3@v3.0.0`）。
- 构建命令与本地一致：`wails3 build` / `wails3 task ...`；不在 workflow 里复制一套构建逻辑。

## 4. Windows：Authenticode

### 4.1 凭据（GitHub Actions Secrets）

| 名称 | 内容 | 必需 |
|---|---|---|
| `WINDOWS_PFX_BASE64` | PKCS#12 证书 base64（含私钥） | 正式发布 |
| `WINDOWS_PFX_PASSWORD` | PFX 密码 | 正式发布 |
| `WINDOWS_TIMESTAMP_URL` | RFC 3161 时间戳 URL（如 DigiCert） | 可选；默认使用平台推荐值 |

证书文件只在 runner 临时目录解码（`0600` 等价权限），任务结束删除；不得 `echo` 证书/密码，不得上传证书文件。dry-run 使用 PowerShell 生成自签证书并导入当前用户 Trusted Root，仅用于验签。

### 4.2 步骤

1. `wails3 build` 生成 `bin/HappyWork.exe`。
2. `wails3 tool sign --input bin/HappyWork.exe --certificate "$env:TEMP\happywork.pfx" --password "$env:WINDOWS_PFX_PASSWORD" --timestamp "$env:WINDOWS_TIMESTAMP_URL"`。
3. `wails3 task windows:create:nsis:installer` 生成安装包。
4. 对安装包执行同一签名命令。
5. `signtool verify /pa /all bin/HappyWork.exe` 与安装包；失败即 job 失败。
6. 上传 exe、安装包与 SHA-256 清单。

### 4.3 dry-run

- `New-SelfSignedCertificate -Type CodeSigningCert` 生成临时证书 → 导出 PFX → 导入 `Cert:\CurrentUser\Root` → 用同一 `wails3 tool sign` 签名 → `signtool verify /pa /all`。
- 临时证书只存在 runner；dry-run 产物标记 `-unsigned-preview`，不得作为正式发布。

## 5. macOS：Developer ID + 公证

### 5.1 凭据（GitHub Actions Secrets）

| 名称 | 内容 | 必需 |
|---|---|---|
| `MACOS_CERT_P12_BASE64` | Developer ID Application 证书 + 私钥（.p12） | 正式发布 |
| `MACOS_CERT_PASSWORD` | .p12 密码 | 正式发布 |
| `MACOS_KEYCHAIN_PASSWORD` | 临时钥匙串密码 | 正式发布 |
| `MACOS_SIGN_IDENTITY` | 如 `Developer ID Application: The HappyWork Authors (TEAMID)` | 正式发布 |
| `APPLE_ID` | Apple ID | 正式发布 |
| `APPLE_APP_PASSWORD` | App 专用密码 | 正式发布 |
| `APPLE_TEAM_ID` | Team ID | 正式发布 |

### 5.2 步骤

1. 创建临时钥匙串 → 导入 .p12 → `security set-key-partition-list` 允许 codesign 使用私钥。
2. `wails3 build` → `wails3 task darwin:package` 生成 `.app`。
3. 用 `wails3 tool sign --identity "$MACOS_SIGN_IDENTITY" --hardened-runtime --input bin/HappyWork.app` 签名。
4. `xcrun notarytool store-credentials happywork-notary --apple-id "$APPLE_ID" --password "$APPLE_APP_PASSWORD" --team-id "$APPLE_TEAM_ID"` 创建临时 profile。
5. `wails3 tool sign --input bin/HappyWork.app --identity "$MACOS_SIGN_IDENTITY" --keychain-profile happywork-notary --notarize`；或对已签名 app 单独提交公证。
6. `xcrun stapler staple bin/HappyWork.app`；生成 DMG（`wails3 task darwin:create:dmg`），对 DMG 做 `codesign`（可选公证）。
7. 验证：`codesign --verify --deep --strict --verbose=2`、`spctl --assess --type execute --verbose=4`、`xcrun stapler validate`；失败即 job 失败。
8. 上传 `.dmg`、`.app.zip`（保留签名）与 SHA-256 清单；临时钥匙串在 finally 中删除。

### 5.3 dry-run

- 不导入真实证书；`wails3 build` 后 `codesign --force --deep --sign - bin/HappyWork.app`（ad-hoc），`codesign --verify --deep --strict`。
- 不执行公证/staple；产物标记 `-unsigned-preview`。A7 的真实公证必须在有凭据时单独跑。

## 6. Linux：包签名与校验和

### 6.1 凭据（GitHub Actions Secrets）

| 名称 | 内容 | 必需 |
|---|---|---|
| `LINUX_GPG_PRIVATE_KEY` | ASCII-armored 私钥 | 正式发布 |
| `LINUX_GPG_PASSPHRASE` | 私钥口令（无口令时留空） | 可选 |
| `LINUX_GPG_KEY_ID` | 签名密钥指纹/ID | 正式发布 |

### 6.2 步骤

1. `wails3 build` → `wails3 task linux:package` 生成 `.deb`、`.rpm`、AppImage。
2. 导入 GPG 私钥（临时 `GNUPGHOME`，`0700`）。
3. `wails3 tool sign --input "bin/HappyWork*.deb" --pgp-key key.asc --pgp-password "$LINUX_GPG_PASSPHRASE" --role origin`；rpm 同法。
4. AppImage：`gpg --local-user "$LINUX_GPG_KEY_ID" --detach-sign --armor` 生成 `.asc`。
5. 生成 `SHA256SUMS`（所有产物）→ `gpg --detach-sign --armor SHA256SUMS`。
6. 验证：包签名用平台工具（`dpkg-sig --verify` / `rpm --checksig`；若工具产出 detached `.asc` 则 `gpg --verify`），`SHA256SUMS.asc` 用 `gpg --verify`，并 `sha256sum -c SHA256SUMS`；失败即 job 失败。
7. 上传包、`.asc`、`SHA256SUMS`、`SHA256SUMS.asc`。

### 6.3 dry-run

- 在 runner 生成临时无口令 GPG 密钥（`gpg --quick-generate-key`），用同一套命令签名/验签；产物标记 `-unsigned-preview`，不发布。
- GPG 私钥/口令不落盘到仓库；`GNUPGHOME` 在 job 结束删除。

## 7. 统一校验与发布

- Linux job 只签本平台包与 AppImage；**最终 `SHA256SUMS` 在 release job 汇总三平台产物后生成**，避免校验和与实际上传文件不一致。
- 正式发布 job 仅在 tag 触发且 `dry_run=false` 时运行：下载三平台产物 → `sha256sum` 生成 `SHA256SUMS` → 在 release job 导入 GPG（临时 `GNUPGHOME`）签名 → `sha256sum -c` + `gpg --verify SHA256SUMS.asc` 自检 → `gh release create "$TAG" --title "$TAG" --notes-file release-notes.md ...`。
- Release 说明固定包含：版本、支持的平台、**验签命令**、未签名预览与正式产物的区别、无静默自动更新声明。
- 若任一平台验签失败或缺少必需 secret：整个 release 失败，不上传半套产物；不用 `continue-on-error` 掩盖。

## 8. 凭据卫生

- 所有证书/私钥/口令只存 GitHub Actions Secrets；仓库、文档、日志、产物中不得出现。
- 解码后的证书/私钥写入 runner 临时目录，权限收敛，`trap`/`finally` 删除；禁止在命令行 `echo`。
- dry-run 的临时凭据与正式凭据完全隔离；dry-run 产物不得发布。
- 轮换：证书到期前更新 Secret；旧证书吊销后补发 release notes；GPG 公钥指纹写入发布说明。
- 真实凭据缺失时，A7 记"条件未执行"；A6 的 dry-run 不得被表述为"已签名发布"。

## 9. 手工发布清单（所有者）

1. 确认 `build/config.yml` 版本与 `CHANGELOG.md` 一致，main CI 绿。
2. 确认 GitHub Secrets 完整（Windows PFX/密码、Apple 证书/账号/Team ID、Linux GPG 私钥/ID）。
3. 打 annotated tag `vX.Y.Z` 并推送；观察 release workflow 三平台 job。
4. 下载产物，在干净机器执行一次验签命令（Windows `signtool`、macOS `spctl/stapler`、Linux `gpg`）。
5. 在 Release notes 记录签名指纹/证书主体与已知限制。

## 10. 失败模式与验收映射

| 失败模式 | 行为 |
|---|---|
| tag 与配置版本不一致 | prepare 失败，不构建 |
| Windows 证书缺失/密码错误 | windows job 失败，不发布 |
| 时间戳服务不可用 | 重试一次；仍失败则 job 失败（不发布未时间戳签名） |
| Apple 公证超时/拒绝 | macos job 失败；保留日志与 notarytool 输出（不含凭据） |
| Linux GPG 口令错误 | linux job 失败，不发布 |
| dry-run 缺临时工具 | job 失败；不静默跳过签名 |

- **A6**：dry-run 三平台签名/验签全绿。
- **A7**：所有者提供真实凭据后，正式 tag 产物通过各平台验签；否则记录遗留，不伪造证据。
- **A8**：workflow 语法检查、dry-run、三平台构建纳入阶段门禁。
