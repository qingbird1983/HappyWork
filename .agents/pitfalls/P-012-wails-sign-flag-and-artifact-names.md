# P-012-wails3-tool-sign 的 Linux 参数与产物命名假设错误

## 现象

按 `wails3 tool sign -help` 的说明（`-pgp-key string  Path to PGP private key file`）写发布流水线：`--pgp-key "$RUNNER_TEMP/signing.key"`。真实跑起来会失败或签出错误结果；同时按设计稿写 `bin/HappyWork.exe`、`bin/HappyWork*.deb` 这类路径全都找不到文件，而三平台 `wails3 build` 一直是绿的。

## 根因

1. **帮助文本与实现不一致**：Wails v3.0.0 `internal/commands/sign.go` 里 `signDEB` 把 `options.PGPKey` 直接当 `dpkg-sig -k <值>` 的参数用，即**密钥 ID**；`signRPM` 更是只用它做非空校验，真正签名靠 `rpmsign` + `~/.rpmmacros` 的 `%_gpg_name`。所以传文件路径毫无意义。
2. **`rpm --checksig` 的判定**：未签名包也返回 0，输出只是 `digests OK`；公钥不在 RPM 密钥环时报 `NO KEY`。因此"跑过命令"不等于"验签通过"，必须先导入公钥并断言 `digests signatures OK`。
3. **产物名由工具决定，不等于 `productName`**：`build/config.yml` 写 `productName: "HappyWork"`，而实际产物是小写项目名 —— `bin/happywork.exe`、NSIS 的 `bin/happywork-amd64-installer.exe`（`INFO_PROJECTNAME` + `ARCH`）、`bin/happywork.app`。另外 Wails 自带的 `windows:sign:installer` 任务硬编码 `build/windows/nsis/happywork-installer.exe`，与 `project.nsi` 的 `OutFile`（在 `bin/`）不一致，靠任务签名会签错文件。
4. `wails3 task <平台>:sign:*` 的可选参数走 go-task 变量（`{{if .PGP_KEY}}`），不是 CLI_ARGS；在 workflow 里用 `task x -- --flag` 传参不会生效。

## 修复

- 签名步骤改为显式调用 `wails3 tool sign --input <pkg> --pgp-key "$key_id" --role origin`，per-file 循环（不传 glob），并按平台补齐 `~/.rpmmacros`（`%_gpg_name`、`%__gpg`）与 `sudo rpm --import`。
- 所有产物路径改用 glob + `nullglob` + `nocaseglob`，计数为 0 直接失败（不允许"少一个包继续发"）。
- 安装包与 AppImage 一律用通配匹配；`project.nsi` 的 `OutFile` 为准，不用 Wails 的 `sign:installer` 任务。
- 文档 §3/§4.2/§6.2 记录真实名字与参数语义，避免下一轮再猜。

## 预防

- 写 CI/发布流水线前，用源码或 `--help` + 最小实跑校验参数语义；**help 文本不作为依据**（本项目已有一次 help 与实现冲突）。
- 验签类命令必须断言"成功输出特征"，不能只看退出码。
- workflow 里出现的每个文件路径，先在本机 `wails3 build`/打包一次，用 `ls` 确认实际名字。

##（如复发）再次发生记录

- **2026-10-31 同一坑的第二击，更严重**：本机跑 `wails3 tool sign --input bin/happywork.exe --certificate x.pfx --password ...` 直接报
  `ERROR built-in Windows signing not yet implemented - please use signtool.exe on Windows, or install osslsigncode`
  ——**Wails v3.0.0 的 `tool sign` 在 Windows 上根本没实现**（Linux 走 dpkg-sig/rpmsign、macOS 走 codesign 都实现了，唯独 Windows 没有）。发布流水线里所有 `wails3 tool sign` 的 Windows 调用都是死路，远端跑多少次都白跑。
  处置：`release.yml` 的 Windows 签名步骤改为直接调用 SDK 的 `signtool.exe sign /fd SHA256 /f <pfx> /p <口令> /tr <时间戳> /td SHA256`，并用 `scripts/windows-sign-check.ps1` 在本机把"临时证书 → 签名副本 → 验签（指纹/EKU）→ 主体篡改报 `0x80096010`"整条链路验证通过（零 Actions 分钟）。
  教训升级：**help 文本之外，"命令是否实现"也必须实测**。上次是参数语义错，这次是功能压根不存在——只看 `--help` 有这一项，不足以证明它能用。

## 归属

Phase 4 发布流水线（2026-10-31）/ `.github/workflows/release.yml`、`docs/design/phase-4/release-signing.md`。A6 远端 dry-run 仍需最终确认这些假设。
