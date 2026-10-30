package tool

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/happywork/happywork/internal/workspace"
)

type fakeTool struct {
	name string
}

func (f fakeTool) Meta() Meta {
	return Meta{Name: f.name, Ref: "mcp://demo/" + f.name, Action: "unknown", Source: "mcp:demo", Trust: "untrusted"}
}
func (f fakeTool) Exec(context.Context, json.RawMessage) (any, error) { return nil, nil }

func TestRegistryExternalSnapshot(t *testing.T) {
	reg := NewRegistry()
	root, err := workspace.NewRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg.Register(NewReadFile(root))
	if _, ok := reg.Get("mcp__demo__read_note"); ok {
		t.Fatal("external tool visible before SetExternal")
	}
	reg.SetExternal(func() []Tool { return []Tool{fakeTool{name: "mcp__demo__read_note"}} })
	if _, ok := reg.Get("mcp__demo__read_note"); !ok {
		t.Fatal("external tool not resolved")
	}
	specs := reg.Specs()
	if len(specs) != 2 || specs[0].Name != "mcp__demo__read_note" || specs[1].Name != "read_file" {
		t.Fatalf("specs = %+v", specs)
	}
	reg.SetExternal(nil)
	if _, ok := reg.Get("mcp__demo__read_note"); ok {
		t.Fatal("external tool still visible after clearing")
	}
}

func TestWriteAttachment(t *testing.T) {
	dir := t.TempDir()
	large := []byte(strings.Repeat("y", MaxInlineResult+1))
	path, digest, err := WriteAttachment(dir, "sess1", large)
	if err != nil {
		t.Fatalf("WriteAttachment: %v", err)
	}
	if !strings.Contains(path, "attachments") || !strings.HasSuffix(path, digest+".txt") {
		t.Fatalf("path = %q", path)
	}
	body, err := os.ReadFile(path)
	if err != nil || len(body) != len(large) {
		t.Fatalf("attachment read: %v, %d bytes", err, len(body))
	}
	path2, digest2, err := WriteAttachment(dir, "sess1", large)
	if err != nil || path2 != path || digest2 != digest {
		t.Fatalf("identical attachment not reused: %q %q %v", path2, digest2, err)
	}
	if _, _, err := WriteAttachment("", "s", large); err == nil {
		t.Fatal("empty data dir should error")
	}
}

// Sanity: tool specs remain valid JSON schema.
func TestSpecsSchemas(t *testing.T) {
	root, _ := workspace.NewRoot(t.TempDir())
	reg := NewRegistry()
	reg.Register(NewReadFile(root))
	reg.Register(NewListDir(root))
	reg.Register(NewWriteFile(root, t.TempDir(), "s"))
	for _, spec := range reg.Specs() {
		var v any
		if err := json.Unmarshal(spec.Parameters, &v); err != nil {
			t.Fatalf("%s schema invalid: %v", spec.Name, err)
		}
	}
}
