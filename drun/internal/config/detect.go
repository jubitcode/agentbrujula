package config

import (
	"os"
	"path/filepath"
)

type Language string

const (
	LangGo     Language = "go"
	LangNode   Language = "node"
	LangPython Language = "python"
	LangRust   Language = "rust"
	LangOdin   Language = "odin"
)

type Detector struct {
	projectDir string
}

func NewDetector(projectDir string) *Detector {
	return &Detector{
		projectDir: projectDir,
	}
}

func (d *Detector) Detect() (Language, error) {
	checks := []struct {
		lang  Language
		files []string
	}{
		{LangGo, []string{"go.mod", "go.sum"}},
		{LangRust, []string{"Cargo.toml", "Cargo.lock"}},
		{LangNode, []string{"package.json", "package-lock.json", "yarn.lock", "pnpm-lock.yaml"}},
		{LangPython, []string{"pyproject.toml", "requirements.txt", "setup.py", "Pipfile"}},
	}

	for _, check := range checks {
		for _, file := range check.files {
			if d.fileExists(filepath.Join(d.projectDir, file)) {
				return check.lang, nil
			}
		}
	}

	if d.hasOdinFiles() {
		return LangOdin, nil
	}

	return "", nil
}

func (d *Detector) DetectAll() ([]Language, error) {
	var langs []Language

	if d.fileExists(filepath.Join(d.projectDir, "go.mod")) ||
		d.fileExists(filepath.Join(d.projectDir, "go.sum")) {
		langs = append(langs, LangGo)
	}

	if d.fileExists(filepath.Join(d.projectDir, "Cargo.toml")) ||
		d.fileExists(filepath.Join(d.projectDir, "Cargo.lock")) {
		langs = append(langs, LangRust)
	}

	if d.fileExists(filepath.Join(d.projectDir, "package.json")) ||
		d.fileExists(filepath.Join(d.projectDir, "package-lock.json")) ||
		d.fileExists(filepath.Join(d.projectDir, "yarn.lock")) ||
		d.fileExists(filepath.Join(d.projectDir, "pnpm-lock.yaml")) {
		langs = append(langs, LangNode)
	}

	if d.fileExists(filepath.Join(d.projectDir, "pyproject.toml")) ||
		d.fileExists(filepath.Join(d.projectDir, "requirements.txt")) ||
		d.fileExists(filepath.Join(d.projectDir, "setup.py")) ||
		d.fileExists(filepath.Join(d.projectDir, "Pipfile")) {
		langs = append(langs, LangPython)
	}

	if d.hasOdinFiles() {
		langs = append(langs, LangOdin)
	}

	return langs, nil
}

func (d *Detector) fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (d *Detector) hasOdinFiles() bool {
	matches, err := filepath.Glob(filepath.Join(d.projectDir, "*.odin"))
	if err == nil && len(matches) > 0 {
		return true
	}

	matches, err = filepath.Glob(filepath.Join(d.projectDir, "src", "*.odin"))
	if err == nil && len(matches) > 0 {
		return true
	}

	return false
}
