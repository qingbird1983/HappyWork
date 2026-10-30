package hook

import (
	"context"
	"fmt"
	"regexp"
	"sort"
)

// secret-scan is the reference hook (hooks.md §6): it flags credential-shaped
// strings in tool arguments without ever recording the matched text. It is
// disabled unless hooks.d selects it.
type secretScan struct {
	mode string // warn | block
}

func newSecretScan(params map[string]any) (Hook, error) {
	mode := "warn"
	for key, value := range params {
		switch key {
		case "mode":
			s, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("secret-scan: mode must be a string")
			}
			mode = s
		case "patterns":
			s, ok := value.(string)
			if !ok || s != "default" {
				return nil, fmt.Errorf("secret-scan: patterns only supports %q", "default")
			}
		default:
			return nil, fmt.Errorf("secret-scan: unknown param %q", key)
		}
	}
	if mode != "warn" && mode != "block" {
		return nil, fmt.Errorf("secret-scan: mode must be warn or block")
	}
	return &secretScan{mode: mode}, nil
}

func (h *secretScan) Name() string { return "secret-scan" }

func (h *secretScan) Points() []Point { return []Point{BeforeToolCall, BeforeApproval} }

func (h *secretScan) Priority() int { return 100 }

type secretPattern struct {
	name string
	re   *regexp.Regexp
}

var secretPatterns = []secretPattern{
	{"pem_private_key", regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`)},
	{"aws_access_key", regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
	{"github_token", regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}`)},
	{"openai_key", regexp.MustCompile(`sk-[A-Za-z0-9_-]{20,}`)},
	{"slack_token", regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`)},
	{"jwt", regexp.MustCompile(`eyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}`)},
}

type secretHit struct {
	pattern string
	field   string
}

func (h *secretScan) Handle(_ context.Context, ev *Event) (Outcome, error) {
	if ev.Point != BeforeToolCall && ev.Point != BeforeApproval {
		return Outcome{}, nil
	}
	hits := scanSecrets("", ev.Args, nil)
	if len(hits) == 0 {
		return Outcome{}, nil
	}
	var out Outcome
	for _, hit := range hits {
		out.Warnings = append(out.Warnings, fmt.Sprintf(
			"secret-scan: 字段 %s 疑似包含 %s，请确认未泄漏凭据", hit.field, hit.pattern))
	}
	if h.mode == "block" {
		if ev.Point == BeforeApproval {
			out.EscalateBlock = true
		} else {
			out.DenyReason = "secret-scan: 参数包含疑似凭据，已阻止执行"
		}
	}
	return out, nil
}

// scanSecrets walks maps and arrays deterministically. It returns at most ten
// hits and never retains the matched substring.
func scanSecrets(prefix string, value any, hits []secretHit) []secretHit {
	if len(hits) >= 10 {
		return hits
	}
	switch v := value.(type) {
	case string:
		for _, p := range secretPatterns {
			if p.re.MatchString(v) {
				field := prefix
				if field == "" {
					field = "(root)"
				}
				hits = append(hits, secretHit{pattern: p.name, field: field})
				break
			}
		}
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			child := k
			if prefix != "" {
				child = prefix + "." + k
			}
			hits = scanSecrets(child, v[k], hits)
		}
	case []any:
		for i, item := range v {
			hits = scanSecrets(fmt.Sprintf("%s[%d]", prefix, i), item, hits)
		}
	}
	return hits
}
