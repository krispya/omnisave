package client

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// Config holds client configuration.
type Config struct {
	ServerURL         string `yaml:"server_url"`
	Token             string `yaml:"token"`
	ClientID          string `yaml:"client_id"`
	RetroarchSavesDir string `yaml:"retroarch_saves_dir"`

	path string
}

// LoadConfig reads config from path, creating it with defaults if absent.
func LoadConfig(path string) (*Config, error) {
	cfg := &Config{path: path}

	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err == nil {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
	}

	changed := false

	if cfg.ClientID == "" {
		cfg.ClientID = uuid.New().String()
		changed = true
	}

	if cfg.RetroarchSavesDir == "" {
		if dir, err := detectRetroarchSavesDir(); err == nil {
			cfg.RetroarchSavesDir = dir
			changed = true
		}
	}

	if changed {
		if err := cfg.Save(); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

// Save writes the config back to its file.
func (c *Config) Save() error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0755); err != nil {
		return err
	}
	return os.WriteFile(c.path, data, 0600)
}

// detectRetroarchSavesDir finds the Retroarch savefile_directory by reading
// retroarch.cfg from known locations in order.
func detectRetroarchSavesDir() (string, error) {
	cfgPaths := retroarchCfgPaths()
	for _, cfgFile := range cfgPaths {
		dir, err := parseSavefileDirectory(cfgFile)
		if err == nil && dir != "" {
			return dir, nil
		}
	}
	return "", fmt.Errorf("retroarch config not found")
}

// retroarchCfgPaths returns candidate retroarch.cfg file paths for the current OS.
func retroarchCfgPaths() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "linux":
		return []string{
			filepath.Join(home, ".var/app/org.libretro.RetroArch/config/retroarch/retroarch.cfg"),
			filepath.Join(home, ".config/retroarch/retroarch.cfg"),
			filepath.Join(home, "snap/retroarch/current/.config/retroarch/retroarch.cfg"),
		}
	case "darwin":
		return []string{
			filepath.Join(home, "Library/Application Support/RetroArch/retroarch.cfg"),
		}
	case "windows":
		appdata := os.Getenv("APPDATA")
		return []string{
			filepath.Join(appdata, "RetroArch/retroarch.cfg"),
		}
	}
	return nil
}

// parseSavefileDirectory reads a retroarch.cfg file and returns the savefile_directory value.
// Falls back to {cfgDir}/saves/ if the key is absent.
func parseSavefileDirectory(cfgFile string) (string, error) {
	f, err := os.Open(cfgFile)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "savefile_directory") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				val := strings.TrimSpace(parts[1])
				val = strings.Trim(val, `"`)
				if val != "" && val != "default" {
					return val, nil
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}

	// Key absent — use {cfgDir}/saves/
	return filepath.Join(filepath.Dir(cfgFile), "saves"), nil
}
