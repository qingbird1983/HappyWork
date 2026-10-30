package mock

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/happywork/happywork/internal/model"
)

func loadForTest(t *testing.T) Scripts {
	t.Helper()
	s, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return s
}

func TestLoadEmbeddedScripts(t *testing.T) {
	s := loadForTest(t)
	for _, name := range []string{"greet", "read_file", "tool_result", "long", "boom"} {
		if _, ok := s[name]; !ok {
			t.Errorf("script %q missing", name)
		}
	}
}

func TestDefaultSelector(t *testing.T) {
	cases := map[string]string{
		"帮我 boom 一下":          "boom",
		"来一段 long 输出":         "long",
		"read_file README.md": "read_file",
		"读取 main.go":          "read_file",
		"read the file":       "read_file",
		"你好":                  "greet",
	}
	for in, want := range cases {
		if got := DefaultSelector(in, 1); got != want {
			t.Errorf("DefaultSelector(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExtractPath(t *testing.T) {
	cases := map[string]string{
		`读取 "docs/a b.md" 这个文件`: "docs/a b.md",
		"读取 src/main.go":        "src/main.go",
		"随便读点什么":                "README.md",
	}
	for in, want := range cases {
		if got := ExtractPath(in); got != want {
			t.Errorf("ExtractPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStreamGreetDeterministic(t *testing.T) {
	s := loadForTest(t)
	p := New(s, nil)
	msgs := []model.Message{{Role: model.RoleUser, Content: "你好"}}

	var runs [][]string
	for i := 0; i < 2; i++ {
		var text strings.Builder
		for c := range p.Stream(context.Background(), msgs, nil) {
			if c.Err != nil {
				t.Fatalf("unexpected err: %v", c.Err)
			}
			text.WriteString(c.Delta)
		}
		runs = append(runs, strings.Fields(text.String()))
	}
	if !reflect.DeepEqual(runs[0], runs[1]) {
		t.Fatal("greet script is not deterministic")
	}
	if len(runs[0]) == 0 {
		t.Fatal("empty greet output")
	}
}

func TestStreamReadFileSubstitutesPath(t *testing.T) {
	s := loadForTest(t)
	p := New(s, func(string, int) string { return "read_file" })
	msgs := []model.Message{{Role: model.RoleUser, Content: `读取 "pkg/a.go" 文件`}}

	var call *model.ToolCall
	for c := range p.Stream(context.Background(), msgs, nil) {
		if c.ToolCall != nil {
			call = c.ToolCall
		}
	}
	if call == nil {
		t.Fatal("read_file script produced no tool call")
	}
	if call.Name != "read_file" {
		t.Fatalf("tool name = %q", call.Name)
	}
	var args map[string]string
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		t.Fatalf("bad args json: %v", err)
	}
	if args["path"] != "pkg/a.go" {
		t.Fatalf("args path = %q, want pkg/a.go", args["path"])
	}
}

func TestStreamBoom(t *testing.T) {
	s := loadForTest(t)
	p := New(s, func(string, int) string { return "boom" })
	ch := p.Stream(context.Background(), []model.Message{{Role: model.RoleUser, Content: "boom"}}, nil)

	var sawErr bool
	for c := range ch {
		if c.Err != nil {
			sawErr = true
		}
	}
	if !sawErr {
		t.Fatal("boom script produced no error chunk")
	}
}

func TestStreamHonoursContext(t *testing.T) {
	s := loadForTest(t)
	p := New(s, nil).WithChunkDelay(50 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	msgs := []model.Message{{Role: model.RoleUser, Content: "long"}}

	chunks := 0
	for range p.Stream(ctx, msgs, nil) {
		chunks++
		if chunks == 2 {
			cancel()
		}
	}
	// After cancel at most the channel buffer (32) plus in-flight chunks may
	// drain; the unbounded script would produce ~900.
	if chunks >= 40 {
		t.Fatalf("context cancellation ignored: drained %d chunks", chunks)
	}
}
