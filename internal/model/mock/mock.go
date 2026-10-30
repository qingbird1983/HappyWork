// Package mock provides the deterministic scripted Provider used throughout
// Phase 1 (see docs/design/phase-1/design.md §7). Scripts are embedded JSON
// files; selection per turn is rule-based on the user text.
package mock

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/happywork/happywork/internal/model"
)

//go:embed scripts/*.json
var scriptFiles embed.FS

// Step kinds.
const (
	KindSay         = "say"
	KindCallTool    = "call_tool"
	KindSayToolEcho = "say_tool_echo"
	KindFail        = "fail"
)

// Step is one element of a script.
type Step struct {
	Kind       string          `json:"kind"`
	Text       string          `json:"text,omitempty"`
	ChunkWords int             `json:"chunk_words,omitempty"` // words per streamed chunk; 0 = whole text at once
	Repeat     int             `json:"repeat,omitempty"`      // repeat the text N times before chunking (long-output scripts)
	Name       string          `json:"name,omitempty"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
	Template   string          `json:"template,omitempty"` // say_tool_echo; {path} {content} {size_bytes} {truncated}
	Message    string          `json:"message,omitempty"`  // fail
}

// Script is a named sequence of steps.
type Script struct {
	Steps []Step `json:"steps"`
}

// Scripts maps script name to parsed definition.
type Scripts map[string]Script

// Provider replays scripts deterministically. Safe for concurrent use by
// multiple sessions: every Stream call replays the selected script from the
// beginning.
type Provider struct {
	scripts    Scripts
	selectFn   model.Selector
	chunkDelay time.Duration // delay between chunks; 0 = as fast as the consumer drains
}

// Load parses every scripts/*.json file. The file base name (without
// extension) is the script name.
func Load() (Scripts, error) {
	out := Scripts{}
	entries, err := scriptFiles.ReadDir("scripts")
	if err != nil {
		return nil, fmt.Errorf("mock: read scripts: %w", err)
	}
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".json")
		data, err := scriptFiles.ReadFile("scripts/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("mock: read %s: %w", e.Name(), err)
		}
		var s Script
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("mock: parse %s: %w", e.Name(), err)
		}
		out[name] = s
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("mock: no scripts embedded")
	}
	return out, nil
}

// New builds a Provider from loaded scripts. A nil selector falls back to
// DefaultSelector.
func New(s Scripts, sel model.Selector) *Provider {
	if sel == nil {
		sel = DefaultSelector
	}
	return &Provider{scripts: s, selectFn: sel}
}

// WithChunkDelay sets the artificial delay between streamed chunks.
func (p *Provider) WithChunkDelay(d time.Duration) *Provider {
	p.chunkDelay = d
	return p
}

// pathPattern extracts the first path-ish token (quoted string, or a token
// containing a path separator or a dot) from the user text.
var pathPattern = regexp.MustCompile(`"([^"\n]+)"|(\S*[\\/]\S*)|(\S*\.\S{1,8})\b`)

// DefaultSelector maps the user text to a script name:
//
//	text contains "boom"      -> boom   (mid-stream failure)
//	text contains "long"      -> long   (300 chunks, for flush validation)
//	text mentions read_file   -> read_file (tool-call flow; path taken from
//	                             the text or defaulted to README.md)
//	otherwise                 -> greet
func DefaultSelector(userText string, turn int) string {
	lower := strings.ToLower(userText)
	switch {
	case strings.Contains(lower, "boom"):
		return "boom"
	case strings.Contains(lower, "long"):
		return "long"
	case strings.Contains(lower, "read_file"), strings.Contains(lower, "读取"), strings.Contains(lower, "read "):
		return "read_file"
	default:
		return "greet"
	}
}

// ExtractPath finds a candidate path in the user text for the read_file
// script; falls back to "README.md".
func ExtractPath(userText string) string {
	m := pathPattern.FindStringSubmatch(userText)
	if m == nil {
		return "README.md"
	}
	for _, g := range []*string{&m[1], &m[2], &m[3]} {
		if g != nil && *g != "" {
			return *g
		}
	}
	return "README.md"
}

// Stream implements model.Provider.
func (p *Provider) Stream(ctx context.Context, msgs []model.Message, tools []model.ToolSpec) <-chan model.Chunk {
	ch := make(chan model.Chunk, 32)
	go p.play(ctx, msgs, ch)
	return ch
}

func (p *Provider) play(ctx context.Context, msgs []model.Message, ch chan<- model.Chunk) {
	defer close(ch)

	userText, turn := lastUserTurn(msgs)
	// A turn that answers a tool result must not replay the tool-call script
	// (that would loop tool calls until MaxTurns). The tool_result script
	// summarises the latest result and ends the run.
	name := "tool_result"
	if len(msgs) == 0 || msgs[len(msgs)-1].Role != model.RoleTool {
		name = p.selectFn(userText, turn)
	}
	script, ok := p.scripts[name]
	if !ok {
		ch <- model.Chunk{Err: fmt.Errorf("mock: unknown script %q", name)}
		ch <- model.Chunk{Done: true}
		return
	}

	for _, step := range script.Steps {
		switch step.Kind {
		case KindSay:
			if !p.say(ctx, ch, step) {
				return
			}
		case KindCallTool:
			args := substitutePath(step.Arguments, userText)
			select {
			case ch <- model.Chunk{ToolCall: &model.ToolCall{ID: "mock_call_1", Name: step.Name, Arguments: args}}:
			case <-ctx.Done():
				return
			}
		case KindSayToolEcho:
			tpl := step.Template
			if tpl == "" {
				tpl = "工具结果：{content}"
			}
			text := renderEcho(tpl, msgs)
			if !p.say(ctx, ch, Step{Kind: KindSay, Text: text, ChunkWords: step.ChunkWords}) {
				return
			}
		case KindFail:
			ch <- model.Chunk{Err: fmt.Errorf("%s", step.Message)}
			ch <- model.Chunk{Done: true}
			return
		default:
			ch <- model.Chunk{Err: fmt.Errorf("mock: unknown step kind %q", step.Kind)}
			ch <- model.Chunk{Done: true}
			return
		}
	}
	ch <- model.Chunk{Done: true}
}

// say streams step.Text (or the rendered tmpl) in word groups, honouring ctx.
// Returns false when the stream was cancelled.
func (p *Provider) say(ctx context.Context, ch chan<- model.Chunk, step Step) bool {
	text := step.Text
	if step.Repeat > 1 {
		text = strings.Repeat(text, step.Repeat)
	}
	words := strings.Fields(text)
	if step.ChunkWords <= 0 || len(words) <= 1 {
		select {
		case ch <- model.Chunk{Delta: text}:
		case <-ctx.Done():
			return false
		}
		return true
	}
	for i := 0; i < len(words); i += step.ChunkWords {
		end := min(i+step.ChunkWords, len(words))
		select {
		case ch <- model.Chunk{Delta: strings.Join(words[i:end], " ") + " "}:
		case <-ctx.Done():
			return false
		}
		if p.chunkDelay > 0 {
			select {
			case <-time.After(p.chunkDelay):
			case <-ctx.Done():
				return false
			}
		}
	}
	return true
}

// substitutePath rewrites a {"path": "..."} placeholder argument with the path
// extracted from the user text, so one read_file script serves every request.
func substitutePath(args json.RawMessage, userText string) json.RawMessage {
	var m map[string]any
	if err := json.Unmarshal(args, &m); err != nil {
		return args
	}
	if v, ok := m["path"].(string); ok && v == "$USER_PATH" {
		m["path"] = ExtractPath(userText)
		out, err := json.Marshal(m)
		if err != nil {
			return args
		}
		return out
	}
	return args
}

// renderEcho fills the template with the most recent tool result message.
func renderEcho(tpl string, msgs []model.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Role != model.RoleTool {
			continue
		}
		var res struct {
			Path      string `json:"path"`
			Content   string `json:"content"`
			SizeBytes int64  `json:"size_bytes"`
			Truncated bool   `json:"truncated"`
			Error     string `json:"error"`
		}
		_ = json.Unmarshal([]byte(m.Content), &res)
		if res.Error != "" {
			// Never render an error payload through a success template —
			// zero-value fields would fake a "0 bytes read" success.
			return "工具执行失败：" + res.Error
		}
		r := strings.NewReplacer(
			"{path}", res.Path,
			"{content}", res.Content,
			"{size_bytes}", fmt.Sprint(res.SizeBytes),
			"{truncated}", fmt.Sprint(res.Truncated),
		)
		return r.Replace(tpl)
	}
	return strings.ReplaceAll(tpl, "{content}", "(no tool result yet)")
}

func lastUserTurn(msgs []model.Message) (string, int) {
	text := ""
	turn := 0
	for _, m := range msgs {
		if m.Role == model.RoleUser {
			text = m.Content
			turn++
		}
	}
	return text, turn
}
