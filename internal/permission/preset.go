package permission

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Preset is a named set of tighten-only overrides. Presets cannot widen a
// decision, scope or level, and cannot remove conditions (ADR-0002).
type Preset struct {
	Name              string
	Description       string
	PersistentAllowed bool
	Rules             []PresetRule
}

// PresetRule overrides one tool (or "mcp:*"). Empty fields inherit the base row.
type PresetRule struct {
	Tool       string
	Decision   string
	Scope      string
	Level      string
	Conditions []Condition
}

// PresetSet is the loaded preset catalog keyed by name.
type PresetSet map[string]Preset

// BuiltinPresets returns the shipped catalog. User presets may add names but
// cannot override these.
func BuiltinPresets() PresetSet {
	return PresetSet{
		"default": {
			Name: "default", Description: "默认：工作区读写 + 外部工具逐条/会话审批",
			PersistentAllowed: true,
		},
		"readonly": {
			Name: "readonly", Description: "只读：禁止本地写与外部工具",
			PersistentAllowed: false,
			Rules: []PresetRule{
				{Tool: "builtin://write_file", Decision: DecisionDeny},
				{Tool: "mcp:*", Decision: DecisionDeny},
			},
		},
		"cautious": {
			Name: "cautious", Description: "谨慎：写与外部工具每次审批，无持久规则",
			PersistentAllowed: false,
			Rules: []PresetRule{
				{Tool: "builtin://write_file", Decision: DecisionAsk, Scope: ScopeOnce},
				{Tool: "mcp:*", Decision: DecisionAsk, Scope: ScopeOnce},
			},
		},
	}
}

type presetFile struct {
	Name              string           `yaml:"name"`
	Description       string           `yaml:"description"`
	PersistentAllowed *bool            `yaml:"persistent_allowed"`
	Rules             []presetRuleFile `yaml:"rules"`
}

type presetRuleFile struct {
	Tool       string      `yaml:"tool"`
	Decision   string      `yaml:"decision"`
	Scope      string      `yaml:"scope"`
	Level      string      `yaml:"level"`
	Conditions []Condition `yaml:"conditions"`
}

// LoadPresets returns the builtins plus valid files from dir. Each invalid or
// duplicate file is skipped and reported; one bad file never widens anything.
func LoadPresets(dir string) (PresetSet, []error) {
	set := BuiltinPresets()
	if dir == "" {
		return set, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return set, nil
		}
		return set, []error{fmt.Errorf("presets: read %s: %w", dir, err)}
	}
	var errs []error
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		preset, err := loadPresetFile(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("presets: %s: %w", entry.Name(), err))
			continue
		}
		if _, exists := set[preset.Name]; exists {
			errs = append(errs, fmt.Errorf("presets: %s: name %q is already defined", entry.Name(), preset.Name))
			continue
		}
		set[preset.Name] = preset
	}
	return set, errs
}

func loadPresetFile(path string) (Preset, error) {
	f, err := os.Open(path)
	if err != nil {
		return Preset{}, err
	}
	defer func() { _ = f.Close() }()
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	var raw presetFile
	if err := dec.Decode(&raw); err != nil {
		return Preset{}, fmt.Errorf("decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return Preset{}, fmt.Errorf("decode: multiple YAML documents are not supported")
	}
	p := Preset{Name: raw.Name, Description: raw.Description, PersistentAllowed: true}
	if raw.PersistentAllowed != nil {
		p.PersistentAllowed = *raw.PersistentAllowed
	}
	for _, rule := range raw.Rules {
		p.Rules = append(p.Rules, PresetRule(rule))
	}
	if err := validatePreset(p); err != nil {
		return Preset{}, err
	}
	return p, nil
}

func validatePreset(p Preset) error {
	if !validPresetName(p.Name) {
		return fmt.Errorf("invalid preset name %q", p.Name)
	}
	seen := map[string]bool{}
	for i, rule := range p.Rules {
		if rule.Tool == "" {
			return fmt.Errorf("rules[%d]: tool is required", i)
		}
		if seen[rule.Tool] {
			return fmt.Errorf("rules[%d]: duplicate tool %q", i, rule.Tool)
		}
		seen[rule.Tool] = true
		base, ok := baseRowForPattern(rule.Tool)
		if !ok {
			return fmt.Errorf("rules[%d]: unknown tool %q", i, rule.Tool)
		}
		if rule.Decision == "" {
			return fmt.Errorf("rules[%d]: decision is required", i)
		}
		if decisionRank(rule.Decision) < decisionRank(base.Decision) {
			return fmt.Errorf("rules[%d]: decision %q would widen %q", i, rule.Decision, base.Decision)
		}
		if rule.Scope != "" {
			switch rule.Scope {
			case ScopeNone, ScopeOnce, ScopeSession, ScopePersistent:
			default:
				return fmt.Errorf("rules[%d]: unknown scope %q", i, rule.Scope)
			}
			if rule.Decision == DecisionAllow && rule.Scope != ScopeNone {
				return fmt.Errorf("rules[%d]: allow cannot carry scope %q", i, rule.Scope)
			}
			if rule.Decision == DecisionAsk {
				if base.Decision == DecisionAllow && rule.Scope != ScopeOnce {
					return fmt.Errorf("rules[%d]: tightening allow to ask only supports once", i)
				}
				if base.Decision == DecisionAsk && scopeRank(rule.Scope) > scopeRank(base.Scope) {
					return fmt.Errorf("rules[%d]: scope %q would widen %q", i, rule.Scope, base.Scope)
				}
			}
		}
		if rule.Level != "" {
			switch rule.Level {
			case LevelInfo, LevelConfirm, LevelBlock:
			default:
				return fmt.Errorf("rules[%d]: unknown level %q", i, rule.Level)
			}
			if levelRank(rule.Level) < levelRank(base.Level) {
				return fmt.Errorf("rules[%d]: level %q would weaken %q", i, rule.Level, base.Level)
			}
		}
		for j, cond := range rule.Conditions {
			if err := cond.Validate(); err != nil {
				return fmt.Errorf("rules[%d].conditions[%d]: %w", i, j, err)
			}
		}
	}
	return nil
}

func validPresetName(name string) bool {
	if len(name) == 0 || len(name) > 32 {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '-' && i > 0:
		default:
			return false
		}
	}
	return true
}

// baseRowForPattern resolves a preset target to its base row. "mcp:*" uses the
// read shape (the stricter of the two MCP rows).
func baseRowForPattern(pattern string) (Row, bool) {
	if pattern == "mcp:*" {
		return Row{
			Ref: "mcp:*", Action: ActionRead, Decision: DecisionAsk, Scope: ScopeSession,
			Level: LevelConfirm, Persistent: false, Reversible: true, Reason: "external read tool",
		}, true
	}
	row, ok := builtinRows[pattern]
	return row, ok
}

// matchPattern reports whether a preset rule applies to the tool ref.
func matchPattern(pattern, ref string) bool {
	return pattern == ref || (pattern == "mcp:*" && strings.HasPrefix(ref, "mcp:"))
}

// applyPresets tightens a base row with every matching rule in the preset.
func applyPresets(row Row, p Preset) Row {
	row.Persistent = row.Persistent && p.PersistentAllowed
	for _, rule := range p.Rules {
		if !matchPattern(rule.Tool, row.Ref) {
			continue
		}
		if decisionRank(rule.Decision) > decisionRank(row.Decision) {
			row.Decision = rule.Decision
			row.Scope = ScopeNone
			if rule.Decision == DecisionAsk {
				row.Scope = ScopeOnce
			}
		}
		if rule.Scope != "" && rule.Decision == row.Decision {
			if scopeRank(rule.Scope) < scopeRank(row.Scope) || row.Scope == "" {
				row.Scope = rule.Scope
			}
		}
		if levelRank(rule.Level) > levelRank(row.Level) {
			row.Level = rule.Level
		}
		row.Conditions = append(row.Conditions, rule.Conditions...)
		if row.Decision == DecisionDeny {
			row.Level = ""
			row.Persistent = false
		}
	}
	return row
}
