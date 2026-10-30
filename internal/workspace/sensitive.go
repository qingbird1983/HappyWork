package workspace

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrSensitivePath means the path matched the hard-deny table. Approval cannot
// override it: the Phase 2 model runs in the cloud, so reading the content is
// already an irreversible disclosure (design.md §6.4).
var ErrSensitivePath = errors.New("sensitive path is blocked")

// Sensitive reports whether a workspace-relative path hits the hard-deny table.
// Comparison folds case on every platform: blocking an oddly-cased `.GIT` on
// Linux is safer than letting the alias through.
func Sensitive(rel string) bool {
	rel = strings.ReplaceAll(rel, `\`, "/")
	rel = strings.Trim(rel, "/")
	if rel == "" || rel == "." {
		return false
	}
	segments := strings.Split(rel, "/")
	for _, seg := range segments {
		s := strings.ToLower(seg)
		if s == ".git" || s == ".ssh" {
			return true
		}
	}
	base := strings.ToLower(segments[len(segments)-1])
	if isEnvFile(base) {
		return true
	}
	if isKeyName(base) {
		return true
	}
	switch base {
	case ".npmrc", ".netrc", ".git-credentials":
		return true
	}
	for i := 0; i+1 < len(segments); i++ {
		if strings.EqualFold(segments[i], ".aws") && strings.EqualFold(segments[i+1], "credentials") {
			return true
		}
	}
	return false
}

// isEnvFile blocks .env and .env.* except committed example variants.
func isEnvFile(base string) bool {
	if base != ".env" && !strings.HasPrefix(base, ".env.") {
		return false
	}
	for _, suffix := range []string{".example", ".sample", ".template"} {
		if strings.HasSuffix(base, suffix) {
			return false
		}
	}
	return true
}

// isKeyName blocks private-key and certificate material by name or extension.
func isKeyName(base string) bool {
	switch base {
	case "id_rsa", "id_ed25519", "id_ecdsa", "id_dsa":
		return true
	}
	switch strings.ToLower(filepath.Ext(base)) {
	case ".pem", ".key", ".p12", ".pfx":
		return true
	}
	return false
}

// Rel returns the path of a canonical workspace member relative to the root.
// Tools use it with Sensitive before touching content.
func (r Root) Rel(canonical string) (string, error) {
	if !r.contains(canonical) {
		return "", fmt.Errorf("%q: %w", canonical, ErrOutsideWorkspace)
	}
	rel, err := filepath.Rel(r.path, canonical)
	if err != nil {
		return "", fmt.Errorf("workspace: rel %q: %w", canonical, err)
	}
	return rel, nil
}
