// Package fspath holds the path helpers that look at the filesystem: resolving
// symlinks, comparing locations and shortening a path for display.
package fspath

import (
	"os"
	"path/filepath"
	"strings"
)

// Compact shortens a path for display by writing the home directory as ~. A
// path outside the home directory is only cleaned.
func Compact(path string, homeDir string) string {
	cleanPath := filepath.Clean(path)
	cleanHome := filepath.Clean(homeDir)
	relPath := cleanPath
	relHome := cleanHome
	if resolved, err := filepath.EvalSymlinks(cleanPath); err == nil {
		relPath = filepath.Clean(resolved)
	}
	if resolved, err := filepath.EvalSymlinks(cleanHome); err == nil {
		relHome = filepath.Clean(resolved)
	}

	if relPath == relHome || cleanPath == cleanHome {
		return "~"
	}
	if rel, err := filepath.Rel(relHome, relPath); err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return filepath.ToSlash(filepath.Join("~", rel))
	}
	if rel, err := filepath.Rel(cleanHome, cleanPath); err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return filepath.ToSlash(filepath.Join("~", rel))
	}
	return cleanPath
}

// Inside reports whether path is dir or lies below it, after resolving
// symlinks on both.
func Inside(path string, dir string) bool {
	return InsideCanonical(Canonical(path), Canonical(dir))
}

// InsideCanonical is Inside for paths that already went through Canonical.
func InsideCanonical(cleanPath string, cleanDir string) bool {
	if cleanPath == cleanDir {
		return true
	}
	rel, err := filepath.Rel(cleanDir, cleanPath)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// Canonical resolves symlinks in path. A path that does not exist is made
// absolute instead.
func Canonical(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	if abs, err := filepath.Abs(path); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(path)
}
