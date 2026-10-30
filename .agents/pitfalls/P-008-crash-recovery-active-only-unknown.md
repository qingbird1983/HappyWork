# P-008-崩溃恢复只标活动会话，运行中工具记 unknown

## 现象

第一版重启恢复把**所有**非终态会话都改成 `interrupted`，连从未运行的 `idle` 会话也被标成"上次中断"，用户看到虚假的中断横幅。另一个隐患是运行中的工具调用被当作失败处理。

## 根因

重启时只按"不是终态"筛选，没有区分"有运行在途"与"只是存在"；同时工具结果只有成功/失败两种语义，没有"未知"。

## 修复

- `MarkInterruptedNonTerminal` 只翻转 `thinking/running_tool/persisting/awaiting_approval/paused`，idle 保持 idle。
- `MarkRunningToolCallsUnknown` 把 `running` 改为 `unknown`；恢复时向模型注入"执行结果未知；副作用是否发生不确定"，不得声称失败或成功。
- 回归：`TestMarkInterruptedNonTerminal`、`TestCrashDuringRunningToolMarksUnknownThenResume`、`TestRecoveryResumeInjectsUnknown`。

## 预防

- 崩溃恢复按"状态 × 在途资源"两维判断；终态集合不是活动状态集合。
- 未知是第三种结果：涉及外部副作用的工具，崩溃后只能报告不确定，不能猜。

##（如复发）再次发生记录

- （无）

## 归属

Phase 2 实现/真机验收（2026-10-30）/ 恢复语义。
