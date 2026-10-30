# Phase 4 发布签名设计：三平台签名、公证与验签

- 状态：定稿（2026-10-30，所有者放行 D1~D6）
- 日期：2026-10-30
- 关联：[phase-4-audit-rollback-signing.md](../../roadmap/phase-4-audit-rollback-signing.md)（A6/A7）；[ADR-0017](../../adr/ADR-0017-release-signing.md)；[architecture/04](../../architecture/04-security-and-hooks.md) §应用更新；[design.md](design.md) §8

## 1. 目标与非目标

目标：`v*` 标签触发可重复的三平台发布流水线，产物带平台签名并可被用户/CI 验证；凭据不进入仓库、日志或构建产物。

> 所有者决定（2026-10-31）：**发布节奏不急**，UI 细设计与打磨完成前不打 `v*` 标签；**首发平台 Windows 优先**，macOS/Linux 的验收标准是「可构建 + 可签名 + 可验签」，不保证首发打磨度。因此三平台流水线仍然全部建好并保留 dry-run 能力，但在 Windows 版打磨到位前不进入正式发布；A7（真实凭据签名）随首发日期一起延后，属于主动排期而非缺陷遗留。

非目标：应用内更新检查/下载器、静默自动更新、MSIX、多签名身份、密钥轮换 UI、Sigstore/透明日志、bit-for-bit 可复现构建。真实证书采购与 Apple 账号由所有者准备（A7 条件项）。

## 2. 触发与版本

- **正式发布**：推送 annotated tag `vX.Y.Z`（如 `v0.1.0`），`release.yml` 触发；tag 去掉 `v` 必须等于 `build/config.yml` 的 `info.version`，否则 `prepare` 失败。
- **dry-run**：手动 `workflow_dispatch` 触发。没有标签就没有"正式"语义，因此 dispatch 一律是 dry-run：构建/临时凭据签名/验签，产物标签为 `dry-run-preview`，`release` job 不运行。用于 A6。
- **触发前提**：`workflow_dispatch` 只承认**默认分支上存在**的 workflow 文件，`--ref` 指向 feature 分支会返回 `404 workflow not found on the default branch`。所以 A6 的证据只能在 `release.yml` 合入 main 之后取得；在此之前本流水线的任何命令、任务名与产物名都只有本机依据，没有远端运行证据（P-015）。
- **阶段标签**（`phase-N.end`）不触发发布。
- 版本号变更流程：更新 `build/config.yml` + `CHANGELOG.md` → `wails3 task common:update:build-assets` 重新生成资产 → 提交 → 打 tag；tag 与配置文件不一致即失败，不做自动改写。
- 版本门禁由 `scripts/check-version.sh` 执行：除比较 `build/config.yml`，还要确认 `build/windows/info.json`、`build/windows/nsis/wails_tools.nsh`、`build/linux/nfpm/nfpm.yaml`、`build/darwin/Info.plist` 都写了同一版本。Phase 4 立项时这些生成物仍带着 `wails3 init` 模板值（`happywork.exe`、"My Company"、0.1.0），与配置里的 0.0.1 不一致；只校验配置会让发布物自报错误版本。
- 产物文件名由 Wails 决定，是小写项目名：`bin/happywork.exe`、`bin/happywork-amd64-installer.exe`、`bin/happywork.app`、`bin/happywork.dmg`、`bin/happywork_*.deb`、`bin/happywork-*.rpm`、AppImage 名由 linuxdeploy 生成；因此 workflow 对安装包与 AppImage 使用通配匹配，并在计数为 0 时失败而不是静默跳过。

## 3. 工作流结构

```yaml
name: Release
on:
  push:
    tags: ["v*"]
  workflow_dispatch:      # 一律 dry-run
permissions:
  contents: read
jobs:
  prepare:      # 版本一致性 + 全量门禁（Go/前端/体量/渲染安全）
  windows:      # build → sign exe → NSIS → sign installer → signtool verify
  macos:        # build → .app → sign(+notarize) → staple → dmg → verify
  linux:        # build → deb/rpm/appimage → dpkg-sig/rpmsign/gpg → verify
  release:      # 仅正式 tag；汇总 SHA256SUMS → 签名 → gh release create
```

- 产物按 `<平台>-<label>` 命名，`label` 为 `release` 或 `dry-run-preview`；`release` job 只下载 `*-release`，dry-run 产物因此不可能被误发布。
- 各构建 job 使用 `actions/upload-artifact`；`release` 使用 `actions/download-artifact`（`pattern: "*-release"`，`merge-multiple`）与 `gh release create`，`permissions: contents: write` 只在 `release` job 声明。
- 所有第三方 action 固定 major 版本；Go/Wails 版本与 CI 一致（Go `1.26`、`wails3@v3.0.0`），由 workflow 顶层 `env` 统一。
- 构建与打包一律走 `wails3 build` / `wails3 task <平台>:<任务>`；只有"签名 + 验签"这段用 `wails3 tool sign` 与平台原生命令，因为需要显式传入 runner 临时证书路径。
- `wails3 tool sign` 的参数以 `wails3 tool sign -help` 为准：`-certificate/-password/-timestamp/-thumbprint`（Windows）、`-identity/-hardened-runtime/-keychain-profile/-notarize/-entitlements`（macOS）、`-pgp-key/-pgp-password/-role`（Linux）。Linux 的 `-pgp-key` 是**密钥 ID**（内部调用 `dpkg-sig`/`rpmsign`），不是私钥文件路径。

## 4. Windows：Authenticode

### 4.1 凭据（GitHub Actions Secrets）

| 名称 | 内容 | 必需 |
|---|---|---|
| `WINDOWS_PFX_BASE64` | PKCS#12 证书 base64（含私钥） | 正式发布 |
| `WINDOWS_PFX_PASSWORD` | PFX 密码 | 正式发布 |
| `WINDOWS_TIMESTAMP_URL` | RFC 3161 时间戳 URL（如 DigiCert） | 可选；默认使用平台推荐值 |

证书文件只在 runner 临时目录解码（`0600` 等价权限），任务结束删除；不得 `echo` 证书/密码，不得上传证书文件。dry-run 使用 PowerShell 生成自签证书并导入当前用户 Trusted Root，仅用于验签。

### 4.2 步骤

1. `wails3 build` 生成 `bin/happywork.exe`；`makensis` 缺失时先 `choco install nsis`。
2. `wails3 tool sign --input bin/happywork.exe --certificate "$RUNNER_TEMP/happywork-signing.pfx" --password <口令> --timestamp <URL>`。
3. `wails3 task windows:create:nsis:installer` 生成 `bin/happywork-<arch>-installer.exe`。
4. 对安装包执行同一签名命令。
5. `signtool verify /pa /all bin\happywork.exe` 与安装包（`bin\happywork-*-installer.exe`）；失败即 job 失败。
6. 上传 exe 与安装包；SHA-256 清单由 `release` job 统一生成（见 §7），避免各平台各出一份再合并时对不上。

### 4.3 dry-run

- `New-SelfSignedCertificate -Type CodeSigningCert` 生成临时证书 → 导出 PFX → 导入 `Cert:\CurrentUser\Root` → 用同一 `wails3 tool sign` 签名 → `signtool verify /pa /all`。
- 临时证书只存在 runner；dry-run 产物标签为 `dry-run-preview`，`release` job 只下载 `*-release`，因此不可能被误发布。

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

1. `wails3 build` → `wails3 task darwin:create:app:bundle` 生成 `bin/happywork.app`（Wails 内部会先做一次 ad-hoc 签名，避免后续 codesign 冲突）。
2. 创建临时钥匙串（`$RUNNER_TEMP`，job 结束删除）→ 导入 .p12 → `security set-key-partition-list -S apple-tool:,apple:,codesign:` 允许 codesign 使用私钥，并把临时钥匙串置于 user 列表首位。
3. `xcrun notarytool store-credentials happywork-notary --apple-id ... --password ... --team-id ...` 创建临时 profile。
4. `wails3 tool sign --input bin/happywork.app --identity "$MACOS_SIGN_IDENTITY" --hardened-runtime --keychain-profile happywork-notary --notarize` 一步完成签名与公证。
5. `xcrun stapler staple bin/happywork.app`；`wails3 task darwin:create:dmg` 生成 `bin/happywork.dmg`，DMG 用 `codesign --force --timestamp --sign` 签名后再 `notarytool submit --wait` + `stapler staple`。
6. 验证：`codesign --verify --deep --strict --verbose=2`、`spctl --assess --type execute --verbose=4`、`xcrun stapler validate`；失败即 job 失败。dry-run 没有 Developer ID 身份，DMG 未签名，因此 DMG 与 `spctl`/`stapler` 校验只在正式标签路径执行。
7. 上传 `.dmg` 与 `ditto -c -k --keepParent` 生成的 `.app.zip`（保留签名与扩展属性）；临时 p12 与钥匙串在清理步骤中删除。

### 5.3 dry-run

- 不导入真实证书；生成 `.app` 后 `codesign --force --deep --sign - bin/happywork.app`（ad-hoc），`codesign --verify --deep --strict`。
- 不执行公证/staple；产物标签为 `dry-run-preview`。A7 的真实公证必须在有凭据时单独跑。

## 6. Linux：包签名与校验和

### 6.1 凭据（GitHub Actions Secrets）

| 名称 | 内容 | 必需 |
|---|---|---|
| `LINUX_GPG_PRIVATE_KEY` | ASCII-armored 私钥 | 正式发布 |
| `LINUX_GPG_PASSPHRASE` | 私钥口令（无口令时留空） | 可选 |
| `LINUX_GPG_KEY_ID` | 签名密钥指纹/ID | 正式发布 |

### 6.2 步骤

1. `wails3 build` → `wails3 task linux:create:deb` / `linux:create:rpm` / `linux:create:appimage` 生成 `bin/happywork*.deb`、`bin/happywork*.rpm` 与 `bin/*.AppImage`（AppImage 文件名由 linuxdeploy 依桌面项决定，故用通配匹配且大小写不敏感）；三者任一缺失即失败，不允许"少一个包也继续"。
2. 导入 GPG 私钥（临时 `GNUPGHOME`，`0700`）；口令写入 `0600` 临时文件后由 `--pgp-password` 传给 `wails3 tool sign`（不写进日志、不作为命令行回显）。
3. `wails3 tool sign --input <pkg> --pgp-key "$LINUX_GPG_KEY_ID" --role origin`（内部调用 `dpkg-sig`/`rpmsign`，`--pgp-key` 取密钥 ID）；rpm 验签需要 `~/.rpmmacros` 里写 `%_gpg_name` 与 `%__gpg`。
4. AppImage 无法内嵌签名：`gpg --local-user "$LINUX_GPG_KEY_ID" --detach-sign --armor` 生成 `.asc`。
5. 本平台生成 `linux-SHA256SUMS.txt` 并签名，作为流水线自验证据；最终 `SHA256SUMS` 由 `release` job 汇总三平台后生成。
6. 验证：`dpkg-sig --verify`、`rpm --checksig`（要求输出含 `digests signatures OK`，未签名包只会报 `digests OK`）、`gpg --verify *.asc`、`sha256sum -c`；失败即 job 失败。
7. 上传包、`.asc`、本平台校验和与导出的公钥 `happywork-release-public.asc`。

### 6.3 dry-run

- 在 runner 生成临时无口令 GPG 密钥（`gpg --quick-generate-key`），用同一套命令签名/验签；产物标记 `-unsigned-preview`，不发布。
- GPG 私钥/口令不落盘到仓库；`GNUPGHOME` 在 job 结束删除。

## 7. 统一校验与发布

- Linux job 只签本平台包与 AppImage；**最终 `SHA256SUMS` 在 release job 汇总三平台产物后生成**，避免校验和与实际上传文件不一致。
- 正式发布 job 仅在 tag 触发时运行（`workflow_dispatch` 永不进入）：下载三平台产物 → `sha256sum` 生成 `SHA256SUMS` → 在 release job 导入 GPG（临时 `GNUPGHOME`）签名 → `sha256sum -c` + `gpg --verify SHA256SUMS.asc` 自检 → `gh release create "$TAG" --title "$TAG" --notes-file release-notes.md ...`。
- Release 说明固定包含：版本、支持的平台、**验签命令**、未签名预览与正式产物的区别、无静默自动更新声明。
- 若任一平台验签失败或缺少必需 secret：整个 release 失败，不上传半套产物；不用 `continue-on-error` 掩盖。

## 8. 凭据卫生

- 所有证书/私钥/口令只存 GitHub Actions Secrets；仓库、文档、日志、产物中不得出现。
- 解码后的证书/私钥写入 runner 临时目录，权限收敛，`trap`/`finally` 删除；禁止在命令行 `echo`。
- dry-run 的临时凭据与正式凭据完全隔离；dry-run 产物不得发布。
- 轮换：证书到期前更新 Secret；旧证书吊销后补发 release notes；GPG 公钥指纹写入发布说明。
- 真实凭据缺失时，A7 记"条件未执行"；A6 的 dry-run 不得被表述为"已签名发布"。

## 9. 手工发布清单（所有者）

1. 确认 `build/config.yml` 版本与 `CHANGELOG.md` 一致；运行 `wails3 task common:update:build-assets` 让 Windows 属性、NSIS 定义、Linux 包元数据与 `Info.plist` 跟上该版本，并提交生成物；main CI 绿。
2. 本地预检：`bash scripts/check-version.sh vX.Y.Z`（同时校验配置与生成资产版本，不一致直接失败）。
3. 确认 GitHub Secrets 完整（Windows PFX/密码、Apple 证书/账号/Team ID、Linux GPG 私钥/ID）。
4. 先跑一次 dry-run（`workflow_dispatch`，`dry_run=true`）确认三平台签名与验签全绿，再打 annotated tag `vX.Y.Z` 推送；观察 release workflow 三平台 job 与 release job。
5. 下载产物，在干净机器执行一次验签命令（Windows `signtool`、macOS `spctl`/`stapler`、Linux `dpkg-sig`/`rpm --checksig`、`gpg --verify SHA256SUMS.asc`）。
6. 在 Release notes 记录签名指纹/证书主体与已知限制（GPG 公钥随产物发布）。

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
