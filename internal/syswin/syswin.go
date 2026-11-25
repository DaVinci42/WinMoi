package syswin

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	ErrSymlinkPrivilege = errors.New("insufficient privilege to create symlink (enable Windows Developer Mode or configure fallback)")
	ErrLockHeld         = errors.New("another winmoi instance is already running")
)

type Lock interface {
	Release() error
}

func CreateHardlink(linkPath, targetPath string) error {
	return os.Link(targetPath, linkPath)
}

func IsSameVolume(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.VolumeName(absA), filepath.VolumeName(absB))
	}
	return true
}

func CopyPath(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(src, dst, info.Mode())
	}
	return copyFile(src, dst, info.Mode())
}

func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func copyDir(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(dst, mode); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if err := CopyPath(srcPath, dstPath); err != nil {
			return fmt.Errorf("copy %s: %w", srcPath, err)
		}
	}
	return nil
}

func StripWindowsExtendedPrefix(p string) string {
	switch {
	case strings.HasPrefix(p, `\??\`):
		return p[4:]
	case strings.HasPrefix(p, `\\?\`):
		return p[4:]
	default:
		return p
	}
}
