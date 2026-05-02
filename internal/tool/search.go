package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type SearchCode struct{}

func (s *SearchCode) Name() string        { return "search_code" }
func (s *SearchCode) Description() string { return "Search for a regex pattern across files in the project" }
func (s *SearchCode) Safety() SafetyLevel { return Safe }
func (s *SearchCode) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"regex pattern"},"path":{"type":"string","description":"directory to search (default: project root)"},"include":{"type":"string","description":"file glob filter, e.g. *.go"}},"required":["pattern"]}`)
}

var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true,
	"dist": true, "build": true, ".cache": true, "target": true,
}

func (s *SearchCode) Execute(ctx context.Context, params json.RawMessage) (string, error) {
	var p struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
		Include string `json:"include"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return "", err
	}

	re, err := regexp.Compile(p.Pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex: %w", err)
	}

	root := p.Path
	if root == "" {
		root = "."
	}
	root = expandHome(root)

	var results strings.Builder
	count := 0
	maxResults := 50

	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || count >= maxResults {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if p.Include != "" {
			if matched, _ := filepath.Match(p.Include, d.Name()); !matched {
				return nil
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil // skip unreadable files
		}
		// Skip binary
		for _, b := range data[:min(len(data), 512)] {
			if b == 0 {
				return nil
			}
		}

		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if re.MatchString(line) {
				fmt.Fprintf(&results, "%s:%d: %s\n", path, i+1, strings.TrimSpace(line))
				count++
				if count >= maxResults {
					return nil
				}
			}
		}
		return nil
	})

	if err != nil {
		return "", err
	}
	if count == 0 {
		return "no matches found", nil
	}
	return results.String(), nil
}
