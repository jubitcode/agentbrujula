package build

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

var defaultExclusions = []string{
	".git",
	".drun",
	".docker",
	".svn",
	".hg",
}

func CreateBuildContext(dockerfile string, projectDir string) (io.Reader, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	if err := addDockerfile(tw, dockerfile); err != nil {
		return nil, err
	}

	if projectDir != "" {
		patterns, err := ParseDockerignore(projectDir)
		if err != nil {
			return nil, fmt.Errorf("failed to parse .dockerignore: %w", err)
		}

		if err := addDirectoryToTar(tw, projectDir, patterns); err != nil {
			return nil, fmt.Errorf("failed to add project directory: %w", err)
		}
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("failed to close tar: %w", err)
	}

	return bytes.NewReader(buf.Bytes()), nil
}

func addDockerfile(tw *tar.Writer, content string) error {
	dockerfileBytes := []byte(content)
	header := &tar.Header{
		Name:    "Dockerfile",
		Mode:    0644,
		Size:    int64(len(dockerfileBytes)),
		ModTime: time.Now(),
	}

	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("failed to write Dockerfile header: %w", err)
	}

	if _, err := tw.Write(dockerfileBytes); err != nil {
		return fmt.Errorf("failed to write Dockerfile: %w", err)
	}

	return nil
}

func addDirectoryToTar(tw *tar.Writer, dir string, patterns []string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if path == dir {
			return nil
		}

		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}

		if relPath == "Dockerfile" {
			return nil
		}

		if isDefaultExclusion(relPath) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if MatchesPattern(relPath, patterns) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if info.Mode()&os.ModeSymlink != 0 {
			return addSymlinkToTar(tw, path, relPath)
		}

		if info.IsDir() {
			return nil
		}

		return addFileToTar(tw, path, relPath, info)
	})
}

func isDefaultExclusion(path string) bool {
	baseName := filepath.Base(path)
	for _, ex := range defaultExclusions {
		if baseName == ex {
			return true
		}
	}

	parts := filepath.SplitList(path)
	if len(parts) == 0 {
		parts = []string{path}
	}
	for _, part := range parts {
		for _, ex := range defaultExclusions {
			if part == ex {
				return true
			}
		}
	}

	return false
}

func addFileToTar(tw *tar.Writer, path, relPath string, info os.FileInfo) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", relPath, err)
	}
	defer file.Close()

	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return fmt.Errorf("failed to create header for %s: %w", relPath, err)
	}
	header.Name = relPath

	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("failed to write header for %s: %w", relPath, err)
	}

	if _, err := io.Copy(tw, file); err != nil {
		return fmt.Errorf("failed to write content for %s: %w", relPath, err)
	}

	return nil
}

func addSymlinkToTar(tw *tar.Writer, path, relPath string) error {
	target, err := os.Readlink(path)
	if err != nil {
		return fmt.Errorf("failed to read symlink %s: %w", relPath, err)
	}

	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("failed to stat symlink %s: %w", relPath, err)
	}

	header, err := tar.FileInfoHeader(info, target)
	if err != nil {
		return fmt.Errorf("failed to create header for symlink %s: %w", relPath, err)
	}
	header.Name = relPath

	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("failed to write symlink header for %s: %w", relPath, err)
	}

	return nil
}
