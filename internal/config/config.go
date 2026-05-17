package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	NotionToken         string  `json:"notion_token" yaml:"notion_token"`
	NotionDatabaseID    string  `json:"notion_database_id" yaml:"notion_database_id"`
	ScanRoot            string  `json:"scan_root" yaml:"scan_root"`
	StateFile           string  `json:"state_file" yaml:"state_file"`
	GlobPattern         string  `json:"glob_pattern" yaml:"glob_pattern"`
	RPS                 float64 `json:"rps" yaml:"rps"`
	LogFormat           string  `json:"log_format" yaml:"log_format"`
	ClaudeLogDatabaseID string  `json:"claudelog_database_id" yaml:"claudelog_database_id"`
	ClaudeProjectsRoot  string  `json:"claude_projects_root" yaml:"claude_projects_root"`
}

type Flags struct {
	ConfigPath string
	DryRun     bool
	Verbose    bool
	Once       bool
	NoJitter   bool
	ScanRoot   string
	StateFile  string
}

const (
	defaultGlob      = "*/.serena/memories/*.md"
	defaultRPS       = 2.5
	defaultLogFormat = "text"
)

// DefaultScanRoot returns the canonical default scan root (~/Projects). It
// returns an empty string when HOME cannot be resolved so that downstream
// validation surfaces the missing-config error rather than silently using a
// cwd-relative "Projects" path.
func DefaultScanRoot() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, "Projects")
}

// DefaultClaudeProjectsRoot returns the canonical default Claude Code projects
// root (~/.claude/projects). Returns an empty string when HOME cannot be
// resolved so downstream validation surfaces the missing-config error rather
// than silently using a cwd-relative path.
func DefaultClaudeProjectsRoot() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

func Load(flags Flags) (*Config, error) {
	cfg := &Config{
		GlobPattern: defaultGlob,
		RPS:         defaultRPS,
		LogFormat:   defaultLogFormat,
	}

	if flags.ConfigPath != "" {
		if err := loadFile(flags.ConfigPath, cfg); err != nil {
			return nil, fmt.Errorf("config file: %w", err)
		}
	}

	applyEnv(cfg)

	if flags.ScanRoot != "" {
		cfg.ScanRoot = flags.ScanRoot
	}
	if flags.StateFile != "" {
		cfg.StateFile = flags.StateFile
	}

	if cfg.ScanRoot == "" {
		cfg.ScanRoot = DefaultScanRoot()
	}
	if cfg.StateFile == "" {
		if home, _ := os.UserHomeDir(); home != "" {
			cfg.StateFile = filepath.Join(home, ".cortex", "sync_state.json")
		}
	}

	if err := cfg.validate(flags.DryRun); err != nil {
		return nil, err
	}
	return cfg, nil
}

func loadFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	switch ext := strings.ToLower(filepath.Ext(path)); ext {
	case ".yaml", ".yml":
		return yaml.Unmarshal(data, cfg)
	case ".json":
		return json.Unmarshal(data, cfg)
	default:
		return fmt.Errorf("unsupported config extension %q", ext)
	}
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("CORTEX_NOTION_TOKEN"); v != "" {
		cfg.NotionToken = v
	}
	if v := os.Getenv("CORTEX_NOTION_DATABASE_ID"); v != "" {
		cfg.NotionDatabaseID = v
	}
	if v := os.Getenv("CORTEX_SCAN_ROOT"); v != "" {
		cfg.ScanRoot = v
	}
	if v := os.Getenv("CORTEX_STATE_FILE"); v != "" {
		cfg.StateFile = v
	}
	if v := os.Getenv("CORTEX_GLOB_PATTERN"); v != "" {
		cfg.GlobPattern = v
	}
	if v := os.Getenv("CORTEX_RPS"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			cfg.RPS = f
		}
	}
	if v := os.Getenv("CORTEX_LOG_FORMAT"); v != "" {
		cfg.LogFormat = strings.ToLower(v)
	}
	if v := os.Getenv("CORTEX_CLAUDELOG_DATABASE_ID"); v != "" {
		cfg.ClaudeLogDatabaseID = v
	}
	if v := os.Getenv("CORTEX_CLAUDE_PROJECTS_ROOT"); v != "" {
		cfg.ClaudeProjectsRoot = v
	}
}

func (c *Config) validate(dryRun bool) error {
	var missing []string
	if !dryRun {
		if c.NotionToken == "" {
			missing = append(missing, "CORTEX_NOTION_TOKEN")
		}
		if c.NotionDatabaseID == "" {
			missing = append(missing, "CORTEX_NOTION_DATABASE_ID")
		}
	}
	if c.ScanRoot == "" {
		missing = append(missing, "scan_root")
	}
	if c.StateFile == "" {
		missing = append(missing, "state_file")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required config: %s", strings.Join(missing, ", "))
	}
	if c.RPS <= 0 {
		return errors.New("rps must be > 0")
	}
	return nil
}

// LoadForClaudeLog builds a Config tailored for the claude-log subcommand.
// It shares file/env layering with Load but applies claude-log-specific
// defaults (Claude projects root, dedicated state file path) and validation
// (requires ClaudeLogDatabaseID + ClaudeProjectsRoot rather than the markdown
// sync DB ID and scan root). Load is intentionally untouched so the existing
// markdown sync path keeps its exact behaviour.
func LoadForClaudeLog(flags Flags) (*Config, error) {
	cfg := &Config{
		GlobPattern: defaultGlob,
		RPS:         defaultRPS,
		LogFormat:   defaultLogFormat,
	}

	if flags.ConfigPath != "" {
		if err := loadFile(flags.ConfigPath, cfg); err != nil {
			return nil, fmt.Errorf("config file: %w", err)
		}
	}

	applyEnv(cfg)

	if flags.StateFile != "" {
		cfg.StateFile = flags.StateFile
	}

	if cfg.ClaudeProjectsRoot == "" {
		cfg.ClaudeProjectsRoot = DefaultClaudeProjectsRoot()
	}
	if cfg.StateFile == "" {
		if home, _ := os.UserHomeDir(); home != "" {
			cfg.StateFile = filepath.Join(home, ".cortex", "claudelog_state.json")
		}
	}

	if err := cfg.validateForClaudeLog(flags.DryRun); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validateForClaudeLog(dryRun bool) error {
	var missing []string
	if !dryRun {
		if c.NotionToken == "" {
			missing = append(missing, "CORTEX_NOTION_TOKEN")
		}
		if c.ClaudeLogDatabaseID == "" {
			missing = append(missing, "CORTEX_CLAUDELOG_DATABASE_ID")
		}
	}
	if c.ClaudeProjectsRoot == "" {
		missing = append(missing, "claude_projects_root")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required config: %s", strings.Join(missing, ", "))
	}
	if c.RPS <= 0 {
		return errors.New("rps must be > 0")
	}
	return nil
}
