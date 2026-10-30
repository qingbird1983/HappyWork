package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/happywork/happywork/internal/tool"
	"github.com/happywork/happywork/internal/workspace"
)

// described is the human-readable part of an approval request.
type described struct {
	title   string
	summary string
	fields  []ApprovalField
	preview string
}

// describeCall builds the dialog content. For built-in tools args.path has
// already been canonicalized into normalizedPath by the caller.
func describeCall(meta tool.Meta, args map[string]any, normalizedPath string, root workspace.Root, source string) described {
	if strings.HasPrefix(meta.Source, "mcp:") {
		server := strings.TrimPrefix(meta.Source, "mcp:")
		params := compactArgs(args, 2000)
		return described{
			title:   "调用外部工具",
			summary: fmt.Sprintf("%s · %s", server, meta.Name),
			fields: []ApprovalField{
				{Label: "来源", Value: "MCP: " + server + "（内容不可信）"},
				{Label: "工具", Value: meta.Name},
				{Label: "可逆性", Value: "未知（第三方工具）"},
				{Label: "参数", Value: params},
			},
		}
	}

	rel := relativeDisplay(root, normalizedPath)
	switch meta.Ref {
	case "builtin://write_file":
		content, _ := args["content"].(string)
		existing, err := os.Stat(normalizedPath)
		created := os.IsNotExist(err)
		action := "新建"
		summary := fmt.Sprintf("新建 %s", rel)
		fields := []ApprovalField{
			{Label: "目标路径", Value: rel},
			{Label: "操作", Value: action},
			{Label: "影响目录", Value: relativeDisplay(root, parentDir(normalizedPath))},
			{Label: "来源", Value: "内置"},
			{Label: "写入大小", Value: formatBytes(int64(len(content)))},
			{Label: "可逆性", Value: "是（覆盖前保留副本）"},
		}
		if !created && err == nil {
			action = "覆盖"
			summary = fmt.Sprintf("覆盖 %s（原 %s）", rel, formatBytes(existing.Size()))
			fields[1].Value = action
			fields = append(fields, ApprovalField{
				Label: "原文件",
				Value: fmt.Sprintf("%s / %d 行", formatBytes(existing.Size()), countFileLines(normalizedPath)),
			})
		}
		return described{
			title:   "写入文件",
			summary: summary,
			fields:  fields,
			preview: previewText(content, 40, 4096),
		}
	default:
		return described{
			title:   "工具调用",
			summary: fmt.Sprintf("%s %s", meta.Name, compactArgs(args, 200)),
			fields: []ApprovalField{
				{Label: "工具", Value: meta.Name},
				{Label: "来源", Value: "内置"},
			},
			preview: compactArgs(args, 4096),
		}
	}
}

// argsSummary is the short tool-block description (never the full content).
func argsSummary(args map[string]any) string {
	return compactArgs(args, 200)
}

func compactArgs(args map[string]any, max int) string {
	if len(args) == 0 {
		return "{}"
	}
	data, err := json.Marshal(args)
	if err != nil {
		return "(无法序列化参数)"
	}
	s := string(data)
	if len(s) > max {
		s = strings.ToValidUTF8(s[:max], "") + "…"
	}
	return s
}

func relativeDisplay(root workspace.Root, path string) string {
	if path == "" {
		return ""
	}
	if rel, err := root.Rel(path); err == nil {
		return strings.ReplaceAll(rel, `\`, "/")
	}
	return path
}

func parentDir(path string) string {
	if i := strings.LastIndexAny(path, `/\`); i > 0 {
		return path[:i]
	}
	return path
}

func previewText(content string, maxLines, maxBytes int) string {
	if len(content) > maxBytes {
		content = strings.ToValidUTF8(content[:maxBytes], "")
	}
	lines := strings.Split(content, "\n")
	truncated := false
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		truncated = true
	}
	out := strings.Join(lines, "\n")
	if truncated {
		out += "\n…（预览截断）"
	}
	return out
}

func countFileLines(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, 2<<20)
	n, _ := f.Read(buf)
	if n == 0 {
		return 0
	}
	return strings.Count(string(buf[:n]), "\n") + 1
}

func formatBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
