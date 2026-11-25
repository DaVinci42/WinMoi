//go:build !windows

package syswin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

var (
	stubEnvMu sync.RWMutex
	stubEnv   = map[string]string{}
)

func ResetStubEnv() {
	stubEnvMu.Lock()
	defer stubEnvMu.Unlock()
	stubEnv = map[string]string{}
}

func GetUserEnv(name string) (string, bool, error) {
	stubEnvMu.RLock()
	if v, ok := stubEnv[name]; ok {
		stubEnvMu.RUnlock()
		return v, true, nil
	}
	stubEnvMu.RUnlock()
	v, ok := os.LookupEnv(name)
	return v, ok, nil
}

func SetUserEnv(name, value string) error {
	stubEnvMu.Lock()
	stubEnv[name] = value
	stubEnvMu.Unlock()
	return os.Setenv(name, value)
}

func BroadcastEnvChange() error {
	return nil
}

func IsDeveloperModeEnabled() (bool, error) {
	return true, nil
}

func CreateJunction(linkPath, targetPath string) error {
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		return err
	}
	return os.Symlink(absTarget, linkPath)
}

func CreateSymlink(linkPath, targetPath string) error {
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		return err
	}
	return os.Symlink(targetPath, linkPath)
}

func IsLink(path string) (bool, string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, "", nil
		}
		return false, "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return true, "symlink", nil
	}
	return false, "", nil
}

func ReadLinkTarget(path string) (string, error) {
	return os.Readlink(path)
}

func RemoveLink(path string) error {
	return os.Remove(path)
}

type unixLock struct {
	file *os.File
}

func (l *unixLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	_ = syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	err := l.file.Close()
	l.file = nil
	return err
}

func AcquireLock(lockPath string) (Lock, error) {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("%w (%s)", ErrLockHeld, lockPath)
	}
	return &unixLock{file: f}, nil
}
