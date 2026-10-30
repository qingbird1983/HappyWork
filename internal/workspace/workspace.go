// Package workspace implements the ADR-0004 path canonicalization pipeline and
// the workspace boundary check used by every filesystem-touching tool.
package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrOutsideWorkspace means the canonicalized path escaped the session root.
var ErrOutsideWorkspace = errors.New("path is outside the workspace")

// ErrInvalidPath means the input path form is rejected outright (UNC, drive-relative, 8.3 short name).
var ErrInvalidPath = errors.New("invalid path form")

// dosDriveRelative matches drive-relative paths like "C:foo" (no separator after the colon).
var dosDriveRelative = regexp.MustCompile(`^[A-Za-z]:[^\\/]`)

// shortName matches 8.3 short-name segments like "RUNNER~1" / "ABC123~2.HTM".
var shortName = regexp.MustCompile(`~[0-9]+(\.|$)`)

// Root is the canonical workspace root captured at session start (ADR-0004:
// the root is fixed for the lifetime of the session).
type Root struct {
	path string
}

// NewRoot canonicalizes and validates the workspace root itself.
func NewRoot(raw string) (Root, error) {
	if strings.TrimSpace(raw) == "" {
		return Root{}, fmt.Errorf("workspace: empty root")
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return Root{}, fmt.Errorf("workspace: %w", err)
	}
	if err := rejectInvalidForms(abs); err != nil {
		return Root{}, err
	}
	canonical, err := canonicalize(abs)
	if err != nil {
		return Root{}, fmt.Errorf("workspace root: %w", err)
	}
	return Root{path: canonical}, nil
}

// String returns the canonical root path.
func (r Root) String() string { return r.path }

// Resolve canonicalizes input (absolute or relative to the root) and enforces
// the workspace boundary. The returned path is safe to pass to os APIs.
func (r Root) Resolve(input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		return "", fmt.Errorf("workspace: empty path")
	}
	// Reject hostile forms on the raw input first: after joining with the root
	// a drive-relative or UNC prefix would no longer be detectable.
	if err := rejectInvalidForms(input); err != nil {
		return "", err
	}
	candidate := input
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(r.path, candidate)
	}
	canonical, err := canonicalize(candidate)
	if err != nil {
		return "", err
	}
	if !r.contains(canonical) {
		return "", fmt.Errorf("%q resolves to %q: %w", input, canonical, ErrOutsideWorkspace)
	}
	return canonical, nil
}

// contains reports whether canonical is the root itself or below it.
// Windows paths are case-insensitive, so the prefix check folds case there.
func (r Root) contains(canonical string) bool {
	root := r.path
	if isWindows() {
		root = strings.ToLower(root)
		canonical = strings.ToLower(canonical)
	}
	if canonical == root {
		return true
	}
	return strings.HasPrefix(canonical, root+string(filepath.Separator))
}

// rejectInvalidForms refuses path shapes that normalize poorly or smuggle
// aliases: UNC shares, drive-relative paths and 8.3 short names (per segment).
func rejectInvalidForms(p string) error {
	if strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, `//`) {
		return fmt.Errorf("%q: %w", p, ErrInvalidPath)
	}
	if dosDriveRelative.MatchString(p) {
		return fmt.Errorf("%q: %w", p, ErrInvalidPath)
	}
	for _, seg := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if shortName.MatchString(seg) {
			return fmt.Errorf("%q: 8.3 short name %q: %w", p, seg, ErrInvalidPath)
		}
	}
	return nil
}

// canonicalize runs Clean then EvalSymlinks. Symlink/junction traversal is
// resolved here; Resolve re-checks the boundary afterwards, so a link that
// points outside the workspace is caught. Paths whose tail does not exist yet
// canonicalize against the nearest existing ancestor (needed by Phase 2 write
// flows; read_file turns the residual not-exist into a normal tool error).
func canonicalize(p string) (string, error) {
	cleaned := filepath.Clean(p)
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err == nil {
		return resolved, nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("workspace: resolve %q: %w", cleaned, err)
	}
	dir := filepath.Dir(cleaned)
	tail := filepath.Base(cleaned)
	for dir != filepath.Dir(dir) { // stop at the volume root
		resolved, err := filepath.EvalSymlinks(dir)
		if err == nil {
			return filepath.Join(resolved, tail), nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("workspace: resolve %q: %w", dir, err)
		}
		tail = filepath.Join(filepath.Base(dir), tail)
		dir = filepath.Dir(dir)
	}
	return "", fmt.Errorf("workspace: resolve %q: no existing ancestor: %w", cleaned, err)
}

func isWindows() bool { return filepath.Separator == '\\' }
