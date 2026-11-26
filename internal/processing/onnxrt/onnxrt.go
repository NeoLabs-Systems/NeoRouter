// Package onnxrt loads ONNX Runtime through purego and runs one
// token-classification model: input ids and attention mask in, logits out.
//
// ONNX Runtime is not vendored: the library is looked for in the usual places and
// HasRuntime reports whether it is there at all, so the rest of the server works
// without it.
package onnxrt

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	apiVersion      = 14
	loggingError    = 3
	deviceAllocator = 0
	memTypeDefault  = 0
	typeInt64       = 7
	typeFloat       = 1
)

// API is the ONNX Runtime function table this package uses.
type API struct {
	getErrorMessage             func(status uintptr) *byte
	createEnv                   func(level int32, logID *byte, out *uintptr) uintptr
	createSession               func(env uintptr, modelPath *byte, opts uintptr, out *uintptr) uintptr
	run                         func(session, runOpts uintptr, inputNames **byte, inputs *uintptr, inputLen uintptr, outputNames **byte, outputLen uintptr, outputs *uintptr) uintptr
	createSessionOptions        func(out *uintptr) uintptr
	setGraphOptLevel            func(opts uintptr, level int32) uintptr
	setIntraOpNumThreads        func(opts uintptr, threads int32) uintptr
	sessionGetInputCount        func(session uintptr, out *uintptr) uintptr
	sessionGetOutputCount       func(session uintptr, out *uintptr) uintptr
	sessionGetInputName         func(session uintptr, idx, alloc uintptr, out **byte) uintptr
	sessionGetOutputName        func(session uintptr, idx, alloc uintptr, out **byte) uintptr
	createTensorWithData        func(memInfo uintptr, data unsafe.Pointer, dataBytes uintptr, shape *int64, shapeLen uintptr, elemType int32, out *uintptr) uintptr
	getTensorMutableData        func(val uintptr, out *unsafe.Pointer) uintptr
	getDimensionsCount          func(info uintptr, out *uintptr) uintptr
	getDimensions               func(info uintptr, dims *int64, len uintptr) uintptr
	getTensorShapeElementCount  func(info uintptr, out *uintptr) uintptr
	getTensorTypeAndShape       func(val uintptr, out *uintptr) uintptr
	createCpuMemoryInfo         func(allocType, memType int32, out *uintptr) uintptr
	allocatorFree               func(alloc uintptr, p unsafe.Pointer) uintptr
	getAllocatorWithDefaultOpts func(out *uintptr) uintptr
	releaseEnv                  func(uintptr)
	releaseStatus               func(uintptr)
	releaseMemoryInfo           func(uintptr)
	releaseSession              func(uintptr)
	releaseValue                func(uintptr)
	releaseTensorTypeAndShape   func(uintptr)
	releaseSessionOptions       func(uintptr)
}

var (
	mu     sync.Mutex
	loaded *API
)

func libName() string {
	switch runtime.GOOS {
	case "darwin":
		return "libonnxruntime.dylib"
	case "windows":
		return "onnxruntime.dll"
	default:
		return "libonnxruntime.so"
	}
}

// candidateDirs is where the shared library may sit: an explicit override, the
// data directory's runtime folder, next to the binary, and the usual system
// places (including node packages that ship it).
func candidateDirs() []string {
	dirs := []string{}
	if dir := os.Getenv("NEOROUTER_RUNTIME_DIR"); dir != "" {
		dirs = append(dirs, dir)
	}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe), filepath.Join(filepath.Dir(exe), "runtime"))
	}
	platform := runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOARCH == "amd64" {
		platform = runtime.GOOS + "-x64"
	}
	dirs = append(dirs,
		filepath.Join("node_modules", "onnxruntime-node", "bin", "napi-v3", runtime.GOOS, runtime.GOARCH),
		filepath.Join("node_modules", "onnxruntime-node", "bin", "napi-v3", runtime.GOOS, platform),
		"/usr/local/lib",
		"/usr/lib",
	)
	return dirs
}

// HasRuntime reports whether the ONNX Runtime library can be found.
func HasRuntime() bool {
	for _, dir := range candidateDirs() {
		if _, err := os.Stat(filepath.Join(dir, libName())); err == nil {
			return true
		}
	}
	return false
}

// Load opens the shared library and reads its function table (once).
func Load() (*API, error) {
	mu.Lock()
	defer mu.Unlock()
	if loaded != nil {
		return loaded, nil
	}
	name := libName()
	var handle uintptr
	var lastErr error
	for _, dir := range candidateDirs() {
		candidate := filepath.Join(dir, name)
		if _, err := os.Stat(candidate); err != nil {
			continue
		}
		opened, err := openSharedLib(candidate)
		if err != nil {
			lastErr = err
			continue
		}
		handle = opened
		break
	}
	if handle == 0 {
		opened, err := openSharedLib(name)
		if err != nil {
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, fmt.Errorf("onnxruntime library %s not found: %w", name, err)
		}
		handle = opened
	}
	var getAPIBase func() *[512]uintptr
	purego.RegisterLibFunc(&getAPIBase, handle, "OrtGetApiBase")
	base := getAPIBase()
	if base == nil {
		return nil, fmt.Errorf("OrtGetApiBase returned nil")
	}
	var getAPI func(version uint32) *[512]uintptr
	purego.RegisterFunc(&getAPI, base[0])
	slots := getAPI(apiVersion)
	if slots == nil {
		return nil, fmt.Errorf("OrtApiBase.GetApi(%d) returned nil", apiVersion)
	}
	api := &API{}
	purego.RegisterFunc(&api.getErrorMessage, slots[2])
	purego.RegisterFunc(&api.createEnv, slots[3])
	purego.RegisterFunc(&api.createSession, slots[7])
	purego.RegisterFunc(&api.run, slots[9])
	purego.RegisterFunc(&api.createSessionOptions, slots[10])
	purego.RegisterFunc(&api.setGraphOptLevel, slots[23])
	purego.RegisterFunc(&api.setIntraOpNumThreads, slots[24])
	purego.RegisterFunc(&api.sessionGetInputCount, slots[30])
	purego.RegisterFunc(&api.sessionGetOutputCount, slots[31])
	purego.RegisterFunc(&api.sessionGetInputName, slots[36])
	purego.RegisterFunc(&api.sessionGetOutputName, slots[37])
	purego.RegisterFunc(&api.createTensorWithData, slots[49])
	purego.RegisterFunc(&api.getTensorMutableData, slots[51])
	purego.RegisterFunc(&api.getDimensionsCount, slots[61])
	purego.RegisterFunc(&api.getDimensions, slots[62])
	purego.RegisterFunc(&api.getTensorShapeElementCount, slots[64])
	purego.RegisterFunc(&api.getTensorTypeAndShape, slots[65])
	purego.RegisterFunc(&api.createCpuMemoryInfo, slots[69])
	purego.RegisterFunc(&api.allocatorFree, slots[76])
	purego.RegisterFunc(&api.getAllocatorWithDefaultOpts, slots[78])
	purego.RegisterFunc(&api.releaseEnv, slots[92])
	purego.RegisterFunc(&api.releaseStatus, slots[93])
	purego.RegisterFunc(&api.releaseMemoryInfo, slots[94])
	purego.RegisterFunc(&api.releaseSession, slots[95])
	purego.RegisterFunc(&api.releaseValue, slots[96])
	purego.RegisterFunc(&api.releaseTensorTypeAndShape, slots[99])
	purego.RegisterFunc(&api.releaseSessionOptions, slots[100])
	loaded = api
	return api, nil
}

func (a *API) check(status uintptr, operation string) error {
	if status == 0 {
		return nil
	}
	defer a.releaseStatus(status)
	pointer := a.getErrorMessage(status)
	if pointer == nil {
		return fmt.Errorf("onnxruntime %s failed", operation)
	}
	return fmt.Errorf("onnxruntime %s: %s", operation, goString(pointer))
}

func goString(pointer *byte) string {
	if pointer == nil {
		return ""
	}
	bytes := make([]byte, 0, 64)
	for index := uintptr(0); index < 4096; index++ {
		value := *(*byte)(unsafe.Add(unsafe.Pointer(pointer), index))
		if value == 0 {
			break
		}
		bytes = append(bytes, value)
	}
	return string(bytes)
}

func cString(text string) []byte {
	buffer := make([]byte, len(text)+1)
	copy(buffer, text)
	return buffer
}

// Session is one loaded model.
type Session struct {
	api        *API
	env        uintptr
	session    uintptr
	memInfo    uintptr
	inputNames []string
	outputName string
	mu         sync.Mutex
}

// NewSession loads a model file.
func (a *API) NewSession(modelPath string) (*Session, error) {
	var env, options, session, memInfo, allocator uintptr
	logID := cString("neorouter-privacy")
	if err := a.check(a.createEnv(loggingError, &logID[0], &env), "CreateEnv"); err != nil {
		return nil, err
	}
	releaseEnv := true
	defer func() {
		if releaseEnv && env != 0 {
			a.releaseEnv(env)
		}
	}()
	if err := a.check(a.createSessionOptions(&options), "CreateSessionOptions"); err != nil {
		return nil, err
	}
	defer a.releaseSessionOptions(options)
	_ = a.check(a.setGraphOptLevel(options, 99), "SetGraphOptimizationLevel")
	_ = a.check(a.setIntraOpNumThreads(options, 2), "SetIntraOpNumThreads")
	path := cString(modelPath)
	if err := a.check(a.createSession(env, &path[0], options, &session), "CreateSession"); err != nil {
		return nil, err
	}
	if err := a.check(a.createCpuMemoryInfo(deviceAllocator, memTypeDefault, &memInfo), "CreateCpuMemoryInfo"); err != nil {
		a.releaseSession(session)
		return nil, err
	}
	if err := a.check(a.getAllocatorWithDefaultOpts(&allocator), "GetAllocatorWithDefaultOptions"); err != nil {
		a.releaseMemoryInfo(memInfo)
		a.releaseSession(session)
		return nil, err
	}
	var inputCount, outputCount uintptr
	if err := a.check(a.sessionGetInputCount(session, &inputCount), "SessionGetInputCount"); err != nil {
		a.releaseMemoryInfo(memInfo)
		a.releaseSession(session)
		return nil, err
	}
	if err := a.check(a.sessionGetOutputCount(session, &outputCount), "SessionGetOutputCount"); err != nil || outputCount == 0 {
		a.releaseMemoryInfo(memInfo)
		a.releaseSession(session)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("onnx model has no outputs")
	}
	inputs := make([]string, 0, int(inputCount))
	for index := uintptr(0); index < inputCount; index++ {
		var name *byte
		if err := a.check(a.sessionGetInputName(session, index, allocator, &name), "SessionGetInputName"); err != nil {
			a.releaseMemoryInfo(memInfo)
			a.releaseSession(session)
			return nil, err
		}
		inputs = append(inputs, goString(name))
		_ = a.check(a.allocatorFree(allocator, unsafe.Pointer(name)), "AllocatorFree")
	}
	var outputPointer *byte
	if err := a.check(a.sessionGetOutputName(session, 0, allocator, &outputPointer), "SessionGetOutputName"); err != nil {
		a.releaseMemoryInfo(memInfo)
		a.releaseSession(session)
		return nil, err
	}
	outputName := goString(outputPointer)
	_ = a.check(a.allocatorFree(allocator, unsafe.Pointer(outputPointer)), "AllocatorFree")
	releaseEnv = false
	return &Session{api: a, env: env, session: session, memInfo: memInfo, inputNames: inputs, outputName: outputName}, nil
}

// Close frees the session.
func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.memInfo != 0 {
		s.api.releaseMemoryInfo(s.memInfo)
		s.memInfo = 0
	}
	if s.session != 0 {
		s.api.releaseSession(s.session)
		s.session = 0
	}
	if s.env != 0 {
		s.api.releaseEnv(s.env)
		s.env = 0
	}
	return nil
}

// RunSequence feeds input ids and an attention mask of one sequence and returns
// the model's logits with their shape.
func (s *Session) RunSequence(inputIDs, attentionMask []int64) ([]float32, []int64, error) {
	if len(inputIDs) == 0 || len(inputIDs) != len(attentionMask) {
		return nil, nil, fmt.Errorf("invalid token buffer lengths")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session == 0 {
		return nil, nil, fmt.Errorf("onnx session closed")
	}
	shape := []int64{1, int64(len(inputIDs))}
	var nameBuffers [][]byte
	var namePointers []*byte
	var tensors []uintptr
	defer func() {
		for _, tensor := range tensors {
			if tensor != 0 {
				s.api.releaseValue(tensor)
			}
		}
	}()
	for _, name := range s.inputNames {
		var buffer []int64
		switch name {
		case "input_ids":
			buffer = inputIDs
		case "attention_mask":
			buffer = attentionMask
		default:
			buffer = make([]int64, len(inputIDs))
		}
		var tensor uintptr
		byteLength := uintptr(len(buffer)) * unsafe.Sizeof(int64(0))
		if err := s.api.check(s.api.createTensorWithData(s.memInfo, unsafe.Pointer(&buffer[0]), byteLength, &shape[0], 2, typeInt64, &tensor), "CreateTensorWithData"); err != nil {
			return nil, nil, err
		}
		bytes := cString(name)
		nameBuffers = append(nameBuffers, bytes)
		namePointers = append(namePointers, &bytes[0])
		tensors = append(tensors, tensor)
	}
	if len(tensors) == 0 {
		return nil, nil, fmt.Errorf("onnx model has no inputs")
	}
	outputBuffer := cString(s.outputName)
	outputPointer := &outputBuffer[0]
	var output uintptr
	if err := s.api.check(s.api.run(s.session, 0, &namePointers[0], &tensors[0], uintptr(len(tensors)), &outputPointer, 1, &output), "Run"); err != nil {
		return nil, nil, err
	}
	defer func() {
		if output != 0 {
			s.api.releaseValue(output)
		}
	}()
	var shapeInfo uintptr
	if err := s.api.check(s.api.getTensorTypeAndShape(output, &shapeInfo), "GetTensorTypeAndShape"); err != nil {
		return nil, nil, err
	}
	defer s.api.releaseTensorTypeAndShape(shapeInfo)
	var dimensions uintptr
	if err := s.api.check(s.api.getDimensionsCount(shapeInfo, &dimensions), "GetDimensionsCount"); err != nil {
		return nil, nil, err
	}
	dims := make([]int64, int(dimensions))
	if dimensions > 0 {
		if err := s.api.check(s.api.getDimensions(shapeInfo, &dims[0], dimensions), "GetDimensions"); err != nil {
			return nil, nil, err
		}
	}
	var total uintptr
	if err := s.api.check(s.api.getTensorShapeElementCount(shapeInfo, &total), "GetTensorShapeElementCount"); err != nil {
		return nil, nil, err
	}
	var data unsafe.Pointer
	if err := s.api.check(s.api.getTensorMutableData(output, &data), "GetTensorMutableData"); err != nil {
		return nil, nil, err
	}
	logits := make([]float32, int(total))
	copy(logits, unsafe.Slice((*float32)(data), int(total)))
	runtime.KeepAlive(inputIDs)
	runtime.KeepAlive(attentionMask)
	runtime.KeepAlive(nameBuffers)
	runtime.KeepAlive(outputBuffer)
	return logits, dims, nil
}
