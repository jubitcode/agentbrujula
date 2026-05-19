package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"

	"drun/internal/config/presets"
)

type Loader struct {
	projectDir string
	configPath string
	envLookup  func(string) (string, bool)
}

type LoaderOption func(*Loader)

func WithConfigPath(path string) LoaderOption {
	return func(l *Loader) {
		l.configPath = path
	}
}

func WithEnvLookup(fn func(string) (string, bool)) LoaderOption {
	return func(l *Loader) {
		l.envLookup = fn
	}
}

func NewLoader(projectDir string, opts ...LoaderOption) *Loader {
	l := &Loader{
		projectDir: projectDir,
		envLookup:  os.LookupEnv,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

func (l *Loader) Load() (*Config, error) {
	cfg := NewConfig()
	cfg.ProjectDir = l.projectDir

	detector := NewDetector(l.projectDir)
	lang, err := detector.Detect()
	if err == nil && lang != "" {
		presetData, err := presets.Load(string(lang))
		if err == nil {
			var preset Config
			if err := yaml.Unmarshal(presetData, &preset); err == nil {
				cfg = mergeConfigs(cfg, &preset)
				cfg.PresetName = string(lang)
			}
		}
	}

	projectCfg, err := l.loadProjectConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load project config: %w", err)
	}
	if projectCfg != nil {
		cfg = mergeConfigs(cfg, projectCfg)
	}

	l.expandEnvVars(cfg)

	cfg.ProjectDir = l.projectDir

	cfg.ApplyDefaults()

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (l *Loader) loadProjectConfig() (*Config, error) {
	configPath := l.configPath
	if configPath == "" {
		candidates := []string{
			filepath.Join(l.projectDir, ".drun.yaml"),
			filepath.Join(l.projectDir, ".drun.yml"),
			filepath.Join(l.projectDir, "drun.yaml"),
			filepath.Join(l.projectDir, "drun.yml"),
		}
		for _, path := range candidates {
			if _, err := os.Stat(path); err == nil {
				configPath = path
				break
			}
		}
	}

	if configPath == "" {
		return nil, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	return &cfg, nil
}

func (l *Loader) expandEnvVars(cfg *Config) {
	envPattern := regexp.MustCompile(`\$\{([^}]+)\}`)

	expand := func(s string) string {
		return envPattern.ReplaceAllStringFunc(s, func(match string) string {
			varName := match[2 : len(match)-1]
			if val, ok := l.envLookup(varName); ok {
				return val
			}
			return match
		})
	}

	for i, e := range cfg.Env {
		cfg.Env[i] = expand(e)
	}

	cfg.Image = expand(cfg.Image)

	for i := range cfg.Mounts {
		cfg.Mounts[i].Source = expand(cfg.Mounts[i].Source)
	}
}

func mergeConfigs(dst, src *Config) *Config {
	result := *dst

	if src.Image != "" {
		result.Image = src.Image
	}
	if src.Workdir != "" {
		result.Workdir = src.Workdir
	}
	if src.Dockerfile != "" {
		result.Dockerfile = src.Dockerfile
	}
	if src.DockerfileInline != "" {
		result.DockerfileInline = src.DockerfileInline
	}
	if src.Setup != nil {
		result.Setup = src.Setup
	}
	if len(src.Mounts) > 0 {
		result.Mounts = src.Mounts
	}
	if src.Network != nil {
		result.Network = src.Network
	}
	if len(src.Ports) > 0 {
		result.Ports = append(result.Ports, src.Ports...)
	}
	if len(src.Env) > 0 {
		result.Env = append(result.Env, src.Env...)
	}
	if src.Resources != nil {
		result.Resources = src.Resources
	}
	if src.Process != nil {
		result.Process = src.Process
	}
	if src.Isolation != nil {
		result.Isolation = src.Isolation
	}
	if src.IdleTimeout != "" {
		result.IdleTimeout = src.IdleTimeout
	}
	if len(src.Scripts) > 0 {
		if result.Scripts == nil {
			result.Scripts = make(map[string]Script)
		}
		for k, v := range src.Scripts {
			result.Scripts[k] = v
		}
	}
	if src.Sandbox != nil {
		result.Sandbox = src.Sandbox
	}
	if len(src.WatchFiles) > 0 {
		result.WatchFiles = src.WatchFiles
	}
	if src.Dev != nil {
		result.Dev = src.Dev
	}

	return &result
}
