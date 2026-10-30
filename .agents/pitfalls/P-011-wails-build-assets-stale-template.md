# P-011-Wails 构建资产长期停留在 init 模板值

## 现象

Phase 4 要跑 Linux 打包时才发现 `build/linux/nfpm/nfpm.yaml` 仍是模板内容：包名 `happywork.exe`、`src: ./bin/happywork.exe`（Linux 产物其实叫 `bin/happywork`）、`vendor: "My Company"`、`homepage: https://wails.io`、版本 `0.1.0`；`build/windows/info.json`、`build/windows/nsis/wails_tools.nsh`、`build/darwin/Info.plist` 也各自写着 "My Company"/"My Product"/0.1.0，而 `build/config.yml` 是 0.0.1 + HappyWork。三平台 `wails3 build` 一直绿，所以没人发现——构建不读这些字段就不会报错。

## 根因

1. 这些文件由 `wails3 init` 生成，字段值来自模板而不是项目；`wails3 build` 不校验它们，只有打包/签名任务才用。
2. 版本门禁只比较 `build/config.yml` 与标签，没有覆盖"生成资产里的版本"，于是配置改了、资产没跟上也不会失败。

## 修复

- 运行 `wails3 task common:update:build-assets` 重新生成 Windows 属性/NSIS 定义/Linux 包元数据/macOS plist 并入库；`nfpm.yaml` 的 `homepage` 手工改为仓库地址（工具不管这个字段）。
- `scripts/check-version.sh` 从"标签 == 配置"扩展为"标签 == 配置 == 四个生成资产"，不一致就失败并提示重跑 update:build-assets；不自动改写文件。
- 发布设计的 §2/§9 补上这一步，避免所有者按老流程打标签后发布物自报旧版本。

## 预防

- 任何"由工具生成、但只有发布路径才消费"的文件，都要有一道显式一致性检查；不能指望构建绿灯代表它们正确。
- 新增平台任务（打包/签名/资产）时，先跑一次真实任务看它读哪些文件，再决定门禁覆盖范围。

##（如复发）再次发生记录

- （无）

## 归属

Phase 4 发布流水线（2026-10-31）/ `build/` 资产与 `scripts/check-version.sh`。
