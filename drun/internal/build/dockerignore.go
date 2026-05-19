package build

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

func ParseDockerignore(projectDir string) ([]string, error) {
	ignorePath := filepath.Join(projectDir, ".dockerignore")

	file, err := os.Open(ignorePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	defer file.Close()

	var patterns []string
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()

		line = strings.TrimSpace(line)

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		patterns = append(patterns, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return patterns, nil
}

func MatchesPattern(path string, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}

	matched := false

	for _, pattern := range patterns {
		isNegation := strings.HasPrefix(pattern, "!")
		if isNegation {
			pattern = pattern[1:]
		}

		if matchSinglePattern(path, pattern) {
			if isNegation {
				matched = false
			} else {
				matched = true
			}
		}
	}

	return matched
}

func matchSinglePattern(path, pattern string) bool {
	pattern = strings.TrimPrefix(pattern, "./")
	pattern = strings.TrimSuffix(pattern, "/")

	path = strings.TrimSuffix(path, "/")

	if strings.HasPrefix(pattern, "**/") {
		suffix := pattern[3:]
		if matchWithGlob(path, suffix) {
			return true
		}
		if strings.HasSuffix(path, "/"+suffix) || strings.Contains(path, "/"+suffix+"/") {
			return true
		}
		parts := strings.Split(path, "/")
		for i := range parts {
			subpath := strings.Join(parts[i:], "/")
			if matchWithGlob(subpath, suffix) {
				return true
			}
		}
		return false
	}

	if strings.Contains(pattern, "/**/") {
		parts := strings.SplitN(pattern, "/**/", 2)
		prefix, suffix := parts[0], parts[1]
		if !strings.HasPrefix(path, prefix+"/") && path != prefix {
			return false
		}
		rest := strings.TrimPrefix(path, prefix+"/")
		return matchSinglePattern(rest, "**/"+suffix)
	}

	if strings.HasSuffix(pattern, "/**") {
		prefix := pattern[:len(pattern)-3]
		return strings.HasPrefix(path, prefix+"/") || path == prefix
	}

	if !strings.Contains(pattern, "/") && !strings.Contains(pattern, "*") && !strings.Contains(pattern, "?") {
		pathParts := strings.Split(path, "/")
		for _, part := range pathParts {
			if part == pattern {
				return true
			}
		}
	}

	if matchWithGlob(path, pattern) {
		return true
	}

	if !strings.Contains(pattern, "/") {
		parts := strings.Split(path, "/")
		for _, part := range parts {
			if matchWithGlob(part, pattern) {
				return true
			}
		}
	}

	return false
}

func matchWithGlob(path, pattern string) bool {
	matched, err := filepath.Match(pattern, path)
	if err != nil {
		return false
	}
	if matched {
		return true
	}

	if !strings.Contains(pattern, "/") {
		baseName := filepath.Base(path)
		matched, err = filepath.Match(pattern, baseName)
		if err != nil {
			return false
		}
		return matched
	}

	return false
}
