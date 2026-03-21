package buildfile

import (
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/gobwas/glob"
)

// matchFiles walks srcDir and returns files matching the include/exclude patterns.
// If includes is empty, all files are included. Excludes are applied after includes.
// Patterns use jib-style globs where ** matches zero or more path segments.
func matchFiles(srcDir string, includes, excludes []string) ([]string, error) {
	includeGlobs, err := compilePatterns(includes)
	if err != nil {
		return nil, err
	}
	excludeGlobs, err := compilePatterns(excludes)
	if err != nil {
		return nil, err
	}

	var matches []string
	err = filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		// Check includes
		if len(includeGlobs) > 0 {
			matched := false
			for _, g := range includeGlobs {
				if g.Match(rel) {
					matched = true
					break
				}
			}
			if !matched {
				return nil
			}
		}

		// Check excludes
		for _, g := range excludeGlobs {
			if g.Match(rel) {
				return nil
			}
		}

		matches = append(matches, path)
		return nil
	})

	return matches, err
}

// compilePatterns compiles glob patterns for matching.
// For patterns starting with **/, also compiles a version without the prefix
// so that files at the root level are matched (gobwas/glob ** requires 1+ segments).
func compilePatterns(patterns []string) ([]glob.Glob, error) {
	var globs []glob.Glob
	for _, pattern := range patterns {
		g, err := glob.Compile(pattern)
		if err != nil {
			return nil, err
		}
		globs = append(globs, g)

		// Also compile without leading **/ to match root-level files
		if strings.HasPrefix(pattern, "**/") {
			stripped := strings.TrimPrefix(pattern, "**/")
			g2, err := glob.Compile(stripped)
			if err != nil {
				return nil, err
			}
			globs = append(globs, g2)
		}
	}
	return globs, nil
}

// isSingleFile returns true if src refers to a single file (not a directory).
func isSingleFile(src string) bool {
	return !strings.HasSuffix(src, "/") && !strings.HasSuffix(src, string(filepath.Separator))
}
