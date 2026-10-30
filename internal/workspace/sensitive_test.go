package workspace

import (
	"path/filepath"
	"testing"
)

func TestSensitiveTable(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{".env", true},
		{".env.local", true},
		{"config/.env.production", true},
		{".env.example", false},
		{".env.local.sample", false},
		{".env.template", false},
		{".git/config", true},
		{"sub/.GIT/config", true},
		{"sub/.ssh/id_ed25519", true},
		{"id_rsa", true},
		{"nested/id_rsa", true},
		{"server.pem", true},
		{"tls/private.key", true},
		{"bundle.p12", true},
		{"cert.pfx", true},
		{".aws/credentials", true},
		{"team/.aws/credentials", true},
		{".aws/config", false},
		{".npmrc", true},
		{"home/.netrc", true},
		{".git-credentials", true},
		{"README.md", false},
		{"docs/.env-notes.md", false},
		{"src/main.go", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := Sensitive(tc.path); got != tc.want {
			t.Errorf("Sensitive(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestRootRel(t *testing.T) {
	root, err := NewRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	abs, err := root.Resolve("docs/a.md")
	if err != nil {
		t.Fatal(err)
	}
	rel, err := root.Rel(abs)
	if err != nil {
		t.Fatal(err)
	}
	if rel != filepath.Join("docs", "a.md") {
		t.Fatalf("rel = %q", rel)
	}
	if _, err := root.Rel(t.TempDir()); err == nil {
		t.Fatal("Rel outside workspace should fail")
	}
}
