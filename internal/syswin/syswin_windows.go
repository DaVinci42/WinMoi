//go:build windows

package syswin

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	hwndBroadcast   = uintptr(0xffff)
	wmSettingChange = uintptr(0x001a)
	smtoAbortIfHung = uintptr(0x0002)

	fsctlSetReparsePoint = 0x000900A4
	fsctlGetReparsePoint = 0x000900A8
	ioReparseTagMountPt  = 0xA0000003
	ioReparseTagSymlink  = 0xA000000C
)

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	procSendMsgTimeoutW  = user32.NewProc("SendMessageTimeoutW")
)

func GetUserEnv(name string) (string, bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	defer k.Close()

	val, _, err := k.GetStringValue(name)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	return val, true, nil
}

func SetUserEnv(name, value string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, `Environment`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	if err := k.SetStringValue(name, value); err != nil {
		return err
	}
	return os.Setenv(name, value)
}

func BroadcastEnvChange() error {
	envPtr, err := windows.UTF16PtrFromString("Environment")
	if err != nil {
		return err
	}
	var result uintptr
	ret, _, callErr := procSendMsgTimeoutW.Call(
		hwndBroadcast,
		wmSettingChange,
		0,
		uintptr(unsafe.Pointer(envPtr)),
		smtoAbortIfHung,
		5000,
		uintptr(unsafe.Pointer(&result)),
	)
	if ret == 0 && callErr != nil && callErr != windows.ERROR_SUCCESS {
		return callErr
	}
	return nil
}

func IsDeveloperModeEnabled() (bool, error) {
	k, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows\CurrentVersion\AppModelUnlock`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	defer k.Close()

	val, _, err := k.GetIntegerValue("AllowDevelopmentWithoutDevLicense")
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return val == 1, nil
}

func CreateJunction(linkPath, targetPath string) error {
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(linkPath, 0o755); err != nil {
		return err
	}

	linkPtr, err := windows.UTF16PtrFromString(linkPath)
	if err != nil {
		_ = os.Remove(linkPath)
		return err
	}
	handle, err := windows.CreateFile(
		linkPtr,
		windows.GENERIC_WRITE,
		0,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		_ = os.Remove(linkPath)
		return err
	}
	defer windows.CloseHandle(handle)

	substName := `\??\` + absTarget
	printName := absTarget
	substUTF16 := utf16.Encode([]rune(substName))
	printUTF16 := utf16.Encode([]rune(printName))

	substBytesLen := len(substUTF16) * 2
	printBytesLen := len(printUTF16) * 2
	// MountPointReparseBuffer header (8 bytes) + subst + NUL + print + NUL
	reparseDataLen := 8 + substBytesLen + 2 + printBytesLen + 2
	buf := make([]byte, 8+reparseDataLen)

	binary.LittleEndian.PutUint32(buf[0:4], ioReparseTagMountPt)
	binary.LittleEndian.PutUint16(buf[4:6], uint16(reparseDataLen))
	binary.LittleEndian.PutUint16(buf[6:8], 0)
	binary.LittleEndian.PutUint16(buf[8:10], 0)
	binary.LittleEndian.PutUint16(buf[10:12], uint16(substBytesLen))
	binary.LittleEndian.PutUint16(buf[12:14], uint16(substBytesLen+2))
	binary.LittleEndian.PutUint16(buf[14:16], uint16(printBytesLen))

	offset := 16
	for _, u := range substUTF16 {
		binary.LittleEndian.PutUint16(buf[offset:offset+2], u)
		offset += 2
	}
	offset += 2 // NUL terminator
	for _, u := range printUTF16 {
		binary.LittleEndian.PutUint16(buf[offset:offset+2], u)
		offset += 2
	}

	var bytesReturned uint32
	if err := windows.DeviceIoControl(
		handle,
		fsctlSetReparsePoint,
		&buf[0],
		uint32(len(buf)),
		nil,
		0,
		&bytesReturned,
		nil,
	); err != nil {
		_ = os.Remove(linkPath)
		return fmt.Errorf("create junction %s -> %s: %w", linkPath, absTarget, err)
	}
	return nil
}

func CreateSymlink(linkPath, targetPath string) error {
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		return err
	}
	err := os.Symlink(targetPath, linkPath)
	if err != nil {
		var errno syscall.Errno
		if errors.As(err, &errno) && errno == windows.ERROR_PRIVILEGE_NOT_HELD {
			return fmt.Errorf("%w: %v", ErrSymlinkPrivilege, err)
		}
		return err
	}
	return nil
}

func IsLink(path string) (bool, string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, "", nil
		}
		return false, "", err
	}
	if info.Mode()&os.ModeSymlink == 0 && info.Mode()&os.ModeIrregular == 0 {
		// Check reparse attribute explicitly for directory junctions.
		pPtr, pErr := windows.UTF16PtrFromString(path)
		if pErr != nil {
			return false, "", nil
		}
		attrs, aErr := windows.GetFileAttributes(pPtr)
		if aErr != nil || attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
			return false, "", nil
		}
	}

	tag, err := getReparseTag(path)
	if err == nil {
		switch tag {
		case ioReparseTagMountPt:
			return true, "junction", nil
		case ioReparseTagSymlink:
			return true, "symlink", nil
		}
	}
	return true, "symlink", nil
}

func getReparseTag(path string) (uint32, error) {
	pPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	handle, err := windows.CreateFile(
		pPtr,
		0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(handle)

	buf := make([]byte, 16384)
	var bytesReturned uint32
	if err := windows.DeviceIoControl(
		handle,
		fsctlGetReparsePoint,
		nil,
		0,
		&buf[0],
		uint32(len(buf)),
		&bytesReturned,
		nil,
	); err != nil {
		return 0, err
	}
	if bytesReturned < 4 {
		return 0, errors.New("short reparse buffer")
	}
	return binary.LittleEndian.Uint32(buf[0:4]), nil
}

func ReadLinkTarget(path string) (string, error) {
	target, err := os.Readlink(path)
	if err != nil {
		return "", err
	}
	return StripWindowsExtendedPrefix(target), nil
}

func RemoveLink(path string) error {
	return os.Remove(path)
}

type windowsLock struct {
	file   *os.File
	handle windows.Handle
}

func (l *windowsLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	var ol windows.Overlapped
	_ = windows.UnlockFileEx(l.handle, 0, 1, 0, &ol)
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
	handle := windows.Handle(f.Fd())
	var ol windows.Overlapped
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY)
	if err := windows.LockFileEx(handle, flags, 0, 1, 0, &ol); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("%w (%s)", ErrLockHeld, lockPath)
	}
	return &windowsLock{file: f, handle: handle}, nil
}
