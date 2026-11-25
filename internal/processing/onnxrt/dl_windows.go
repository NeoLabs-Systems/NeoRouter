//go:build windows

package onnxrt

import "syscall"

func openSharedLib(name string) (uintptr, error) {
	handle, err := syscall.LoadLibrary(name)
	return uintptr(handle), err
}
