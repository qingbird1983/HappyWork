# P-013 审计证据写入绝对路径，本地测试因平台差异全绿

## 现象

本地 `go test ./...`（Windows）全绿，推送后 CI 的 ubuntu runner 直接红：

```
--- FAIL: TestPhase4ExportAndVerifyChain
    phase4_backups_test.go:266: export leaked workspace path or file content
```

审计导出（JSONL）里出现 `matchKey":"builtin://write_file|write|/tmp/TestPhase4.../001"`，即**用户工作区的绝对路径**。三平台 `wails3 build`、前端六道门禁、`-race` 全部通过，只有这一条在 Linux 上炸。

## 根因

1. **源头就带绝对路径**：`permission.WriteMatchKey(filepath.Dir(normalizedPath))` 的输入是工作区解析后的绝对目录；`exec.go`/`approval.go` 把 `matchKey` 原样塞进审计 `detail`，一处还把 `matchKey` 当 `reason` 用。设计 D2 要求"只存元数据"，实现把目录结构也存了。
2. **测试比较方式掩盖了它**：断言写成 `strings.Contains(jsonl, root.String())`，字节精确匹配。Windows 上 `WriteMatchKey` 会 `strings.ToLower(dir)`，大小写不一致 → 不匹配；Linux 无大小写折叠 → 匹配 → 失败。**同一份缺陷代码，只在一个平台可测**。
3. **转义层级**：即使大小写对齐也还不够——`detail` 是字符串字段里的 JSON，路径里的 `\` 在导出文本中被写成 `\\\\`，一次 `Contains` 仍会漏。

## 修复

- 新增 `Session.auditKey`/`auditPath`/`relPath`（`internal/agent/audit.go`）：写进审计的目录一律工作区相对（根为 `.`、子目录为 `src/cmd`、根外为 `<outside>`），无路径的键（MCP toolRef）原样透传；覆盖 `tool_call`/`approval`/`permission_rule` 三类记录与 `backup` 记录的路径字段。
- 引擎真正用于匹配的绝对 `match_key` 只留在 `permission_rules` 表，**证据链与匹配键分离**，匹配行为不变。
- 测试改为"去掉 `\` 与 `/` + 忽略大小写"再比较，本机（Windows）先复现为红，再修到绿；补 `TestPhase4AuditKeyKeepsEvidenceWorkspaceRelative` 固化大小写/分隔符/根外三种情形。

## 预防

- 任何"证明给用户看"的产物（审计导出、诊断包、日志、崩溃报告）只写工作区相对路径；绝对路径要在**产生处**消掉，不靠导出时打码。
- 涉及路径不变量的测试，比较前先归一化（去分隔符、忽略大小写），或直接断言"字段值不含分隔符"；写 `Contains(rawPath)` 这种平台敏感比较视为缺陷信号。
- 本地全绿不等于跨平台全绿：路径大小写、分隔符、`TempDir` 形态、JSON 转义层级都是常见差异源。Phase 4 的 CI 首跑就是证据，验收 A8 必须看远端结果而不是只看本机。

##（如复发）再次发生记录

- （无）

## 归属

Phase 4 审计接线（2026-10-31）/ `internal/agent/audit.go`、`internal/agent/exec.go`、`internal/agent/approval.go`、`internal/agent/phase4_test.go`；对应 CI run `41094526014` 与 `docs/design/phase-4/report.md` §4.8。
