//go:build windows

package localmodels

import "syscall"

// openSharedLib loads a shared library by path (the ONNX Runtime dll).
func openSharedLib(path string) (uintptr, error) {
	h, err := syscall.LoadLibrary(path)
	return uintptr(h), err
}
