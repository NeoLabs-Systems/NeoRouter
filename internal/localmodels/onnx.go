package localmodels

// onnx.go runs ONNX models in this process through ONNX Runtime, loaded with
// purego — the same approach refs/NeoRecall/internal/embeddings uses. The Rust
// and Node runtimes both link the C API at run time, so the library is found on
// the machine (the ORT copy Node's onnxruntime ships, or the system one) and the
// api table is read from OrtGetApiBase.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// TensorType is the element type of a tensor: the two the speech models use.
type TensorType int

// Element types.
const (
	TensorFloat TensorType = 1
	TensorInt64 TensorType = 7
)

// Tensor is one input or output of a session: its shape and its data, either
// float32 or int64.
type Tensor struct {
	Shape  []int64
	Type   TensorType
	Floats []float32
	Ints   []int64
}

// FloatTensor builds a float32 tensor.
func FloatTensor(shape []int64, data []float32) *Tensor {
	return &Tensor{Shape: shape, Type: TensorFloat, Floats: data}
}

// Int64Tensor builds an int64 tensor.
func Int64Tensor(shape []int64, data []int64) *Tensor {
	return &Tensor{Shape: shape, Type: TensorInt64, Ints: data}
}

// Session is one loaded ONNX model.
type Session interface {
	// InputNames and OutputNames are the model's own names.
	InputNames() []string
	OutputNames() []string
	// Run feeds the named inputs and returns every output.
	Run(inputs map[string]*Tensor) (map[string]*Tensor, error)
	// Close releases the session.
	Close()
}

// ortSession wraps one ONNX Runtime session handle.
type ortSession struct {
	api         *ortAPI
	env         uintptr
	session     uintptr
	memInfo     uintptr
	inputNames  []string
	outputNames []string
	mu          sync.Mutex
}

// ORTLibraryPath finds libonnxruntime on this machine: an explicit NEOROUTER_ORT_DIR
// or NEOROUTER_ORT_LIBRARY first, then the copies an installed Node dependency
// ships, then the system library.
func ORTLibraryPath() (string, bool) {
	if explicit := os.Getenv("NEOROUTER_ORT_LIBRARY"); explicit != "" {
		if _, err := os.Stat(explicit); err == nil {
			return explicit, true
		}
	}
	names := ortLibraryNames()
	dirs := []string{}
	if dir := os.Getenv("NEOROUTER_ORT_DIR"); dir != "" {
		dirs = append(dirs, dir)
	}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe), filepath.Join(filepath.Dir(exe), "runtime"))
	}
	platform := runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOARCH == "amd64" {
		platform = runtime.GOOS + "-x64"
	}
	for _, version := range []string{"napi-v6", "napi-v3"} {
		dirs = append(dirs,
			filepath.Join("node_modules", "onnxruntime-node", "bin", version, runtime.GOOS, runtime.GOARCH),
			filepath.Join("..", "..", "node_modules", "onnxruntime-node", "bin", version, runtime.GOOS, runtime.GOARCH),
			filepath.Join("node_modules", "onnxruntime-node", "bin", version, runtime.GOOS, platform),
			filepath.Join("..", "..", "node_modules", "onnxruntime-node", "bin", version, runtime.GOOS, platform),
		)
	}
	dirs = append(dirs, "/usr/local/lib", "/usr/lib")
	for _, dir := range dirs {
		for _, name := range names {
			candidate := filepath.Join(dir, name)
			if _, err := os.Stat(candidate); err == nil {
				return candidate, true
			}
		}
	}
	for _, name := range names {
		if _, err := os.Stat(name); err == nil {
			return name, true
		}
	}
	return "", false
}

// ortLibraryNames are the file names the runtime uses per platform.
func ortLibraryNames() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"libonnxruntime.dylib", "libonnxruntime.1.dylib"}
	case "windows":
		return []string{"onnxruntime.dll"}
	default:
		return []string{"libonnxruntime.so", "libonnxruntime.so.1"}
	}
}

// ORTAvailable reports whether ONNX Runtime can be loaded here.
func ORTAvailable() bool {
	_, ok := ORTLibraryPath()
	return ok
}

// ortAPI is the part of ONNX Runtime's C API the speech models need.
type ortAPI struct {
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

// ortAPIVersion is the C API version the slot table is read for.
const ortAPIVersion = 14

const (
	ortLoggingError    = 3
	ortDeviceAllocator = 0
	ortMemTypeDefault  = 0
)

var (
	ortMu     sync.Mutex
	ortLoaded *ortAPI
)

// loadORT loads ONNX Runtime once per process.
func loadORT() (*ortAPI, error) {
	ortMu.Lock()
	defer ortMu.Unlock()
	if ortLoaded != nil {
		return ortLoaded, nil
	}
	path, ok := ORTLibraryPath()
	if !ok {
		return nil, fmt.Errorf("onnxruntime library not found")
	}
	handle, err := openSharedLib(path)
	if err != nil {
		return nil, fmt.Errorf("onnxruntime library %s: %w", path, err)
	}
	var getAPIBase func() *[512]uintptr
	purego.RegisterLibFunc(&getAPIBase, handle, "OrtGetApiBase")
	baseTable := getAPIBase()
	if baseTable == nil {
		return nil, fmt.Errorf("OrtGetApiBase returned nil")
	}
	var getAPI func(version uint32) *[512]uintptr
	purego.RegisterFunc(&getAPI, baseTable[0])
	slots := getAPI(ortAPIVersion)
	if slots == nil {
		return nil, fmt.Errorf("OrtApiBase.GetApi(%d) returned nil", ortAPIVersion)
	}
	api := &ortAPI{}
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
	ortLoaded = api
	return api, nil
}

// check turns a status handle into an error.
func (a *ortAPI) check(status uintptr, op string) error {
	if status == 0 {
		return nil
	}
	defer a.releaseStatus(status)
	message := goString(a.getErrorMessage(status))
	if message == "" {
		return fmt.Errorf("onnxruntime %s failed", op)
	}
	return fmt.Errorf("onnxruntime %s: %s", op, message)
}

// goString reads a C string.
func goString(p *byte) string {
	if p == nil {
		return ""
	}
	var out []byte
	for index := uintptr(0); index < 4096; index++ {
		b := *(*byte)(unsafe.Add(unsafe.Pointer(p), index))
		if b == 0 {
			break
		}
		out = append(out, b)
	}
	return string(out)
}

// cString copies a Go string into a NUL-terminated buffer.
func cString(value string) []byte {
	buffer := make([]byte, len(value)+1)
	copy(buffer, value)
	return buffer
}

// loadONNXSession opens an ONNX model file as a session.
func loadONNXSession(modelPath string) (Session, error) {
	api, err := loadORT()
	if err != nil {
		return nil, err
	}
	var env, opts, session, memInfo, allocator uintptr
	logID := cString("neorouter-localmodels")
	if err := api.check(api.createEnv(ortLoggingError, &logID[0], &env), "CreateEnv"); err != nil {
		return nil, err
	}
	releaseEnv := true
	defer func() {
		if releaseEnv && env != 0 {
			api.releaseEnv(env)
		}
	}()
	if err := api.check(api.createSessionOptions(&opts), "CreateSessionOptions"); err != nil {
		return nil, err
	}
	defer api.releaseSessionOptions(opts)
	_ = api.check(api.setGraphOptLevel(opts, 99), "SetGraphOptimizationLevel")
	_ = api.check(api.setIntraOpNumThreads(opts, 2), "SetIntraOpNumThreads")

	path := cString(modelPath)
	if err := api.check(api.createSession(env, &path[0], opts, &session), "CreateSession"); err != nil {
		return nil, err
	}
	cleanup := func() {
		if session != 0 {
			api.releaseSession(session)
		}
	}
	if err := api.check(api.createCpuMemoryInfo(ortDeviceAllocator, ortMemTypeDefault, &memInfo), "CreateCpuMemoryInfo"); err != nil {
		cleanup()
		return nil, err
	}
	if err := api.check(api.getAllocatorWithDefaultOpts(&allocator), "GetAllocatorWithDefaultOptions"); err != nil {
		api.releaseMemoryInfo(memInfo)
		cleanup()
		return nil, err
	}
	session2 := &ortSession{api: api, env: env, session: session, memInfo: memInfo}
	inputs, err := api.ioNames(api.sessionGetInputName, api.sessionGetInputCount, session, allocator)
	if err != nil {
		session2.Close()
		return nil, err
	}
	outputs, err := api.ioNames(api.sessionGetOutputName, api.sessionGetOutputCount, session, allocator)
	if err != nil {
		session2.Close()
		return nil, err
	}
	session2.inputNames = inputs
	session2.outputNames = outputs
	releaseEnv = false
	return session2, nil
}

// ioNames reads every input or output name of a session.
func (a *ortAPI) ioNames(get func(session uintptr, idx, alloc uintptr, out **byte) uintptr, count func(session uintptr, out *uintptr) uintptr, session, allocator uintptr) ([]string, error) {
	var total uintptr
	if err := a.check(count(session, &total), "GetIoCount"); err != nil {
		return nil, err
	}
	names := make([]string, 0, int(total))
	for index := uintptr(0); index < total; index++ {
		var name *byte
		if err := a.check(get(session, index, allocator, &name), "GetIoName"); err != nil {
			return nil, err
		}
		names = append(names, goString(name))
		if name != nil {
			_ = a.check(a.allocatorFree(allocator, unsafe.Pointer(name)), "AllocatorFree")
		}
	}
	return names, nil
}

// InputNames are the model's inputs, in its own order.
func (s *ortSession) InputNames() []string { return append([]string{}, s.inputNames...) }

// OutputNames are the model's outputs, in its own order.
func (s *ortSession) OutputNames() []string { return append([]string{}, s.outputNames...) }

// Run feeds the inputs and returns every output the session names.
func (s *ortSession) Run(inputs map[string]*Tensor) (map[string]*Tensor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session == 0 {
		return nil, fmt.Errorf("onnx session is closed")
	}
	var keepAlive [][]float32
	var keepAliveInts [][]int64
	values := make([]uintptr, 0, len(s.inputNames))
	nameBuffers := make([][]byte, 0, len(s.inputNames))
	namePointers := make([]*byte, 0, len(s.inputNames))
	defer func() {
		for _, value := range values {
			if value != 0 {
				s.api.releaseValue(value)
			}
		}
		runtime.KeepAlive(keepAlive)
		runtime.KeepAlive(keepAliveInts)
		runtime.KeepAlive(nameBuffers)
	}()
	for _, name := range s.inputNames {
		tensor, ok := inputs[name]
		if !ok {
			return nil, fmt.Errorf("onnx model needs input %s", name)
		}
		shape := make([]int64, len(tensor.Shape))
		copy(shape, tensor.Shape)
		var value uintptr
		var elementType int32
		switch tensor.Type {
		case TensorFloat:
			data := tensor.Floats
			keepAlive = append(keepAlive, data)
			elementType = int32(TensorFloat)
			if err := s.api.check(s.api.createTensorWithData(s.memInfo, unsafe.Pointer(&data[0]),
				uintptr(len(data))*unsafe.Sizeof(float32(0)), &shape[0], uintptr(len(shape)), elementType, &value), "CreateTensorWithData"); err != nil {
				return nil, err
			}
		case TensorInt64:
			data := tensor.Ints
			keepAliveInts = append(keepAliveInts, data)
			elementType = int32(TensorInt64)
			if err := s.api.check(s.api.createTensorWithData(s.memInfo, unsafe.Pointer(&data[0]),
				uintptr(len(data))*unsafe.Sizeof(int64(0)), &shape[0], uintptr(len(shape)), elementType, &value), "CreateTensorWithData"); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("onnx input %s has an unsupported element type", name)
		}
		buffer := cString(name)
		nameBuffers = append(nameBuffers, buffer)
		namePointers = append(namePointers, &buffer[0])
		values = append(values, value)
	}
	outputBuffers := make([][]byte, len(s.outputNames))
	outputPointers := make([]*byte, len(s.outputNames))
	for index, name := range s.outputNames {
		outputBuffers[index] = cString(name)
		outputPointers[index] = &outputBuffers[index][0]
	}
	outputs := make([]uintptr, len(s.outputNames))
	if err := s.api.check(s.api.run(s.session, 0, &namePointers[0], &values[0], uintptr(len(values)),
		&outputPointers[0], uintptr(len(outputPointers)), &outputs[0]), "Run"); err != nil {
		return nil, err
	}
	runtime.KeepAlive(outputBuffers)
	result := make(map[string]*Tensor, len(outputs))
	for index, value := range outputs {
		if value == 0 {
			continue
		}
		tensor, err := s.readTensor(value)
		s.api.releaseValue(value)
		if err != nil {
			return nil, err
		}
		result[s.outputNames[index]] = tensor
	}
	return result, nil
}

// readTensor copies one output tensor out of the runtime.
func (s *ortSession) readTensor(value uintptr) (*Tensor, error) {
	var shapeInfo uintptr
	if err := s.api.check(s.api.getTensorTypeAndShape(value, &shapeInfo), "GetTensorTypeAndShape"); err != nil {
		return nil, err
	}
	defer s.api.releaseTensorTypeAndShape(shapeInfo)
	var dimensions uintptr
	if err := s.api.check(s.api.getDimensionsCount(shapeInfo, &dimensions), "GetDimensionsCount"); err != nil {
		return nil, err
	}
	shape := make([]int64, int(dimensions))
	if dimensions > 0 {
		if err := s.api.check(s.api.getDimensions(shapeInfo, &shape[0], dimensions), "GetDimensions"); err != nil {
			return nil, err
		}
	}
	var total uintptr
	if err := s.api.check(s.api.getTensorShapeElementCount(shapeInfo, &total), "GetTensorShapeElementCount"); err != nil {
		return nil, err
	}
	var data unsafe.Pointer
	if err := s.api.check(s.api.getTensorMutableData(value, &data), "GetTensorMutableData"); err != nil {
		return nil, err
	}
	floats := make([]float32, int(total))
	copy(floats, unsafe.Slice((*float32)(data), int(total)))
	return &Tensor{Shape: shape, Type: TensorFloat, Floats: floats}, nil
}

// Close releases the session and its environment.
func (s *ortSession) Close() {
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
}

// openSharedLib loads a shared library by path.
func openSharedLib(path string) (uintptr, error) {
	return purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
}

// modelFile finds a file in a model directory, preferring an exact name.
func modelFile(dir string, names ...string) (string, bool) {
	for _, name := range names {
		candidate := filepath.Join(dir, filepath.FromSlash(name))
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}
