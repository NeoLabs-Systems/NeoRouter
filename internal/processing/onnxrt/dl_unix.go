//go:build !windows

package onnxrt

import "github.com/ebitengine/purego"

func openSharedLib(name string) (uintptr, error) {
	return purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
}
