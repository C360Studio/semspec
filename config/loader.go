package config

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	// ProjectConfigFile is the name of the project-level config file
	ProjectConfigFile = "semspec.yaml"
	// UserConfigDir is the directory for user-level config
	UserConfigDir = ".config/semspec"
	// UserConfigFile is the name of the user-level config file
	UserConfigFile = "config.yaml"
)

// Loader handles configuration loading with layered precedence
type Loader struct {
	logger *slog.Logger
}

// NewLoader creates a new configuration loader
func NewLoader(logger *slog.Logger) *Loader {
	if logger == nil {
		logger = slog.Default()
	}
	return &Loader{logger: logger}
}

// Load loads configuration with layered precedence:
// 1. Default config
// 2. User config (~/.config/semspec/config.yaml)
// 3. Project config (semspec.yaml in current or parent directories)
// 4. Environment variables (future)
func (l *Loader) Load() (*Config, error) {
	// Start with defaults
	config := DefaultConfig()

	// Load user config
	userConfigPath := l.userConfigPath()
	if userConfig, err := LoadFromFile(userConfigPath); err == nil {
		l.logger.Debug("Loaded user config", slog.String("path", userConfigPath))
		config.Merge(userConfig)
	} else if !os.IsNotExist(err) {
		l.logger.Warn("Failed to load user config", slog.String("path", userConfigPath), slog.String("error", err.Error()))
	}

	// Load project config
	projectConfigPath := l.findProjectConfig()
	if projectConfigPath != "" {
		if projectConfig, err := LoadFromFile(projectConfigPath); err == nil {
			l.logger.Debug("Loaded project config", slog.String("path", projectConfigPath))
			config.Merge(projectConfig)
		} else {
			l.logger.Warn("Failed to load project config", slog.String("path", projectConfigPath), slog.String("error", err.Error()))
		}
	} else {
		l.logger.Debug("No project config found")
	}

	// Auto-detect repo path if not set
	if config.Repo.Path == "" {
		if gitRoot := l.detectGitRoot(); gitRoot != "" {
			config.Repo.Path = gitRoot
			l.logger.Debug("Auto-detected git root", slog.String("path", gitRoot))
		} else {
			// Fall back to current directory
			if cwd, err := os.Getwd(); err == nil {
				config.Repo.Path = cwd
				l.logger.Debug("Using current directory as repo root", slog.String("path", cwd))
			}
		}
	}

	// Validate final config
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return config, nil
}

// EnsureUserConfig creates the user config file with defaults if it doesn't exist
func (l *Loader) EnsureUserConfig() error {
	userConfigPath := l.userConfigPath()

	// Check if it already exists
	if _, err := os.Stat(userConfigPath); err == nil {
		return nil // Already exists
	}

	// Create default config
	config := DefaultConfig()
	if err := config.SaveToFile(userConfigPath); err != nil {
		return err
	}

	l.logger.Info("Created default user config", slog.String("path", userConfigPath))
	return nil
}

// userConfigPath returns the path to the user config file
func (l *Loader) userConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, UserConfigDir, UserConfigFile)
}

// findProjectConfig searches for semspec.yaml in current and parent directories
func (l *Loader) findProjectConfig() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	dir := cwd
	for {
		configPath := filepath.Join(dir, ProjectConfigFile)
		if _, err := os.Stat(configPath); err == nil {
			return configPath
		}

		// Move to parent directory
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached root
			break
		}
		dir = parent
	}

	return ""
}

// detectGitRoot finds the git repository root from current directory
func (l *Loader) detectGitRoot() string {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}
