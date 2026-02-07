package util

import (
	"os"
	"path/filepath"
)

// CopyDir recursively copies a directory tree, attempting to preserve permissions.
// Source directory must exist.
func CopyDir(src string, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		target := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		return os.WriteFile(target, data, info.Mode())
	})
}

// CopyFile copies a single file from src to dst.
func CopyFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	return os.WriteFile(dst, data, info.Mode())
}

// MakeWritableRecursive recursively makes all files and directories in the path writable by the user.
func MakeWritableRecursive(path string) error {
	var totalFiles, chmodCount int
	err := filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		totalFiles++
		if info.Mode().Perm()&0200 == 0 {
			chmodCount++
			return os.Chmod(path, info.Mode().Perm()|0200)
		}
		return nil
	})
	Debugf("MakeWritableRecursive: walked %d files, chmod'd %d", totalFiles, chmodCount)
	return err
}
