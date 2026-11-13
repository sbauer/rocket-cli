//go:build windows

package cli

import (
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"
)

func platformReplayDirectories(_ string) ([]string, error) {
	// FOLDERID_Documents resolves redirected Documents folders (including OneDrive).
	documents := syscall.GUID{Data1: 0xfdd39ad0, Data2: 0x238f, Data3: 0x46af,
		Data4: [8]byte{0xad, 0xb4, 0x6c, 0x85, 0x48, 0x03, 0x69, 0xc7}}
	var path *uint16
	shell := syscall.NewLazyDLL("shell32.dll")
	result, _, _ := shell.NewProc("SHGetKnownFolderPath").Call(
		uintptr(unsafe.Pointer(&documents)), 0, 0, uintptr(unsafe.Pointer(&path)))
	if result != 0 {
		return nil, fmt.Errorf("resolve Windows Documents (HRESULT 0x%x); use --dir or ROCKET_REPLAY_DIR", result)
	}
	defer syscall.NewLazyDLL("ole32.dll").NewProc("CoTaskMemFree").Call(uintptr(unsafe.Pointer(path)))
	var units []uint16
	for offset := uintptr(0); ; offset += 2 {
		unit := *(*uint16)(unsafe.Add(unsafe.Pointer(path), offset))
		if unit == 0 {
			break
		}
		units = append(units, unit)
	}
	return []string{filepath.Join(syscall.UTF16ToString(units), "My Games", "Rocket League", "TAGame", "Demos")}, nil
}
