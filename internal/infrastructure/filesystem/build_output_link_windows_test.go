package filesystem

import (
	"encoding/binary"
	"fmt"
	"os"
	"syscall"
)

const (
	fsctlSetReparsePoint   = 0x000900A4
	ioReparseTagMountPoint = 0xA0000003
)

func createDirectoryRedirect(target, link string) error {
	if err := os.Symlink(target, link); err == nil {
		return nil
	} else if junctionErr := createDirectoryJunction(target, link); junctionErr != nil {
		return fmt.Errorf("symlink: %v; junction: %w", err, junctionErr)
	}
	return nil
}

// createDirectoryJunction mirrors the minimal mount-point reparse construction
// used by the Go standard library's Windows os tests. Junction creation does
// not require the symbolic-link privilege on supported local filesystems.
func createDirectoryJunction(target, link string) (err error) {
	substitute, err := syscall.UTF16FromString(`\??\` + target)
	if err != nil {
		return err
	}
	printName, err := syscall.UTF16FromString(target)
	if err != nil {
		return err
	}
	pathBuffer := append(substitute, printName...)
	pathBytes := len(pathBuffer) * 2
	reparseDataLength := 8 + pathBytes
	buffer := make([]byte, 8+reparseDataLength)
	binary.LittleEndian.PutUint32(buffer[0:4], ioReparseTagMountPoint)
	binary.LittleEndian.PutUint16(buffer[4:6], uint16(reparseDataLength))
	binary.LittleEndian.PutUint16(buffer[8:10], 0)
	binary.LittleEndian.PutUint16(buffer[10:12], uint16((len(substitute)-1)*2))
	binary.LittleEndian.PutUint16(buffer[12:14], uint16(len(substitute)*2))
	binary.LittleEndian.PutUint16(buffer[14:16], uint16((len(printName)-1)*2))
	for i, value := range pathBuffer {
		binary.LittleEndian.PutUint16(buffer[16+i*2:], value)
	}

	if err := os.Mkdir(link, 0o700); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(link)
		}
	}()
	linkPointer, err := syscall.UTF16PtrFromString(link)
	if err != nil {
		return err
	}
	handle, err := syscall.CreateFile(
		linkPointer,
		syscall.GENERIC_WRITE,
		0,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_OPEN_REPARSE_POINT|syscall.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(handle)
	var returned uint32
	return syscall.DeviceIoControl(
		handle,
		fsctlSetReparsePoint,
		&buffer[0],
		uint32(len(buffer)),
		nil,
		0,
		&returned,
		nil,
	)
}
