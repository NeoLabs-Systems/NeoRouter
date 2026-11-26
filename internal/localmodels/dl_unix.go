//go:build !windows

package localmodels

import "github.com/ebitengine/purego"

// openSharedLib loads a shared library by path (the ONNX Runtime dylib/so).
func openSharedLib(path string) (uintptr, error) {
	return purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
}
