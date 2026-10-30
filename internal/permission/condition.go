package permission

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Condition kinds are a closed enum; unknown kinds are rejected at load time.
const (
	ConditionPathPrefix   = "path_prefix"
	ConditionTimeWindow   = "time_window"
	ConditionDomainPrefix = "domain_prefix"
)

// Condition is one DIM3 predicate. Conditions only narrow a decision.
type Condition struct {
	Kind  string `json:"kind" yaml:"kind"`
	Value string `json:"value" yaml:"value"`
}

// Validate rejects unknown or malformed conditions.
func (c Condition) Validate() error {
	switch c.Kind {
	case ConditionPathPrefix:
		if strings.TrimSpace(c.Value) == "" {
			return fmt.Errorf("path_prefix requires a value")
		}
		clean := filepath.Clean(c.Value)
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("path_prefix %q must stay inside the workspace", c.Value)
		}
	case ConditionTimeWindow:
		if _, _, err := parseWindow(c.Value); err != nil {
			return err
		}
	case ConditionDomainPrefix:
		d := normalizeDomain(c.Value)
		if d == "" || strings.Contains(d, "/") || strings.Contains(d, ":") {
			return fmt.Errorf("domain_prefix %q is not a domain", c.Value)
		}
	default:
		return fmt.Errorf("unknown condition kind %q", c.Kind)
	}
	return nil
}

// Satisfied evaluates the condition against a normalized request.
func (c Condition) Satisfied(req Request) bool {
	switch c.Kind {
	case ConditionPathPrefix:
		return pathHasPrefix(req.Root, req.Path, c.Value)
	case ConditionTimeWindow:
		start, end, err := parseWindow(c.Value)
		if err != nil {
			return false
		}
		now := req.Now
		if now.IsZero() {
			now = time.Now()
		}
		minute := now.Hour()*60 + now.Minute()
		if start <= end {
			return minute >= start && minute < end
		}
		return minute >= start || minute < end // overnight window
	case ConditionDomainPrefix:
		d := normalizeDomain(req.Domain)
		p := normalizeDomain(c.Value)
		return d == p || strings.HasSuffix(d, "."+p)
	}
	return false
}

func pathHasPrefix(root, path, prefix string) bool {
	if root == "" || path == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	rel = filepath.Clean(rel)
	prefix = filepath.Clean(prefix)
	if isWindows() {
		rel = strings.ToLower(rel)
		prefix = strings.ToLower(prefix)
	}
	if prefix == "." {
		return true
	}
	return rel == prefix || strings.HasPrefix(rel, prefix+string(filepath.Separator))
}

func parseWindow(value string) (start, end int, err error) {
	parts := strings.Split(strings.TrimSpace(value), "-")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("time_window %q must be HH:MM-HH:MM", value)
	}
	start, err = parseClock(parts[0])
	if err != nil {
		return 0, 0, err
	}
	end, err = parseClock(parts[1])
	if err != nil {
		return 0, 0, err
	}
	if start == end {
		return 0, 0, fmt.Errorf("time_window %q has an empty range", value)
	}
	return start, end, nil
}

func parseClock(s string) (int, error) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("time %q must be HH:MM", s)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, fmt.Errorf("time %q has an invalid hour", s)
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, fmt.Errorf("time %q has an invalid minute", s)
	}
	return h*60 + m, nil
}

func normalizeDomain(d string) string {
	d = strings.ToLower(strings.TrimSpace(d))
	d = strings.TrimSuffix(d, ".")
	return d
}
