package localmodels

// fit.go is server/services/local_models/fit.js: whether a catalog model can run
// on this machine, and how well, in words an admin can act on. A model's memory
// is what it used on average while it ran here, or until then an estimate from
// the download size (weights plus room for the conversation). The room left for
// models is the machine's memory minus what everything else uses on average
// (memory.go), never less than a fixed reserve.

// Fit thresholds, unchanged from fit.js.
const (
	// ReserveMB is always kept free for the system and NeoRouter itself.
	ReserveMB = 2048
	// HeadroomMB is spare room on top of what everything else uses on average,
	// for its peaks.
	HeadroomMB = 1024
	// WeightFactor is the size of the weights as a multiple of the download.
	WeightFactor = 1.15
	// DiskMarginMB is spare disk space a download must leave.
	DiskMarginMB = 1024
	// CPUComfortMB is the size models up to which, without a graphics card,
	// still answer at reading speed.
	CPUComfortMB = 4500
	// SmallMB is where a model is small enough to run anywhere.
	SmallMB = 1500
)

// overheadMB is the room for the conversation (context) and working buffers,
// per engine.
var overheadMB = map[string]float64{EngineLlamaCpp: 1024, EngineTransformers: 384}

// Fit ratings, the words the dashboard shows.
const (
	RatingGreat       = "great"
	RatingGood        = "good"
	RatingSlow        = "slow"
	RatingTight       = "tight"
	RatingTooBig      = "too_big"
	RatingUnsupported = "unsupported"
)

// Need is what one model takes from the machine.
type Need struct {
	DownloadMB float64 `json:"downloadMb"`
	MemoryMB   int64   `json:"memoryMb"`
	// Measured marks a memory figure this server measured, not an estimate.
	Measured bool `json:"measured"`
}

// Needs works out what a model takes. measuredMB is the model's average while
// it ran here, if known (nil until then).
func Needs(entry *Entry, measuredMB *int64) Need {
	memoryMB := int64(0)
	measured := measuredMB != nil
	switch {
	case measuredMB != nil:
		memoryMB = *measuredMB
	case entry.MemoryMB > 0:
		memoryMB = int64(roundHalfUp(entry.MemoryMB))
	default:
		memoryMB = int64(roundHalfUp(entry.DownloadMB*WeightFactor + overheadMB[entry.Engine]))
	}
	return Need{DownloadMB: entry.DownloadMB, MemoryMB: memoryMB, Measured: measured}
}

// CapacityMB is the most memory models could ever have here (everything else at
// a minimum).
func CapacityMB(machine Machine) int64 {
	if machine.Memory.TotalMB-ReserveMB < 0 {
		return 0
	}
	return machine.Memory.TotalMB - ReserveMB
}

// RoomMB is the memory models can use beside what everything else uses on
// average.
func RoomMB(machine Machine) int64 {
	kept := int64(ReserveMB)
	if machine.Memory.OthersMB != nil {
		if used := *machine.Memory.OthersMB + HeadroomMB; used > kept {
			kept = used
		}
	}
	if machine.Memory.TotalMB-kept < 0 {
		return 0
	}
	return machine.Memory.TotalMB - kept
}

// Speed rates how well a model of memoryMB runs here: great runs fully on the
// graphics card (or is small), good is fine on the processor, slow answers
// slowly.
func Speed(entry *Entry, machine Machine, memoryMB int64) string {
	if entry.Engine == EngineTransformers {
		if memoryMB <= SmallMB {
			return RatingGreat
		}
		return RatingGood
	}
	gpu := machine.GPU
	if gpu != nil && memoryMB <= gpu.MemoryMB {
		return RatingGreat
	}
	if gpu != nil && gpu.Type == "cuda" && float64(memoryMB) <= float64(gpu.MemoryMB)*1.5 {
		return RatingGood
	}
	if memoryMB <= SmallMB {
		return RatingGreat
	}
	if memoryMB <= CPUComfortMB {
		return RatingGood
	}
	return RatingSlow
}

// Assessment is fit.assess's answer: the rating, what the model takes and why.
// Field order is the JSON order of the dashboard response.
type Assessment struct {
	MemoryMB   int64   `json:"memoryMb"`
	Measured   bool    `json:"measured"`
	DownloadMB float64 `json:"downloadMb"`
	DiskOK     bool    `json:"diskOk"`
	FitsNow    bool    `json:"fitsNow"`
	Rating     string  `json:"rating"`
	Reason     *string `json:"reason"`
}

// AssessOptions carries what the caller already knows.
type AssessOptions struct {
	// UsedMB is memory already taken by local models that are running.
	UsedMB int64
	// MeasuredMB is what this model used on average while it ran here.
	MeasuredMB *int64
	// Support reports whether an engine runs on this machine. Nil uses the
	// built-in engines (llama.cpp build selection and the ONNX runtime).
	Support func(engine string, machine Machine) Support
}

// Assess is fit.assess: the rating, the memory figure and whether the model
// fits beside what is already running.
func Assess(entry *Entry, machine Machine, o AssessOptions) Assessment {
	need := Needs(entry, o.MeasuredMB)
	room := RoomMB(machine)
	support := o.Support
	if support == nil {
		support = DefaultSupport
	}
	engine := support(entry.Engine, machine)
	diskOK := machine.Disk.FreeMB == nil || need.DownloadMB+float64(DiskMarginMB) <= float64(*machine.Disk.FreeMB)
	base := Assessment{
		MemoryMB:   need.MemoryMB,
		Measured:   need.Measured,
		DownloadMB: need.DownloadMB,
		DiskOK:     diskOK,
		FitsNow:    need.MemoryMB <= room-o.UsedMB,
	}
	reason := func(text string) *string { return &text }
	switch {
	case !engine.OK:
		base.Rating = RatingUnsupported
		base.FitsNow = false
		base.Reason = engine.Reason
	case need.MemoryMB > CapacityMB(machine):
		base.Rating = RatingTooBig
		base.FitsNow = false
		base.Reason = reason("not_enough_memory")
	case need.MemoryMB > room:
		base.Rating = RatingTight
		base.Reason = reason("memory_in_use")
	default:
		base.Rating = Speed(entry, machine, need.MemoryMB)
	}
	return base
}

// roundHalfUp is JavaScript's Math.round for the non-negative figures here.
func roundHalfUp(value float64) float64 {
	if value < 0 {
		return -roundHalfUp(-value)
	}
	return float64(int64(value + 0.5))
}

// floor diverts to the arithmetic the JS uses (Math.floor) for negative and
// fractional memory figures.
func floor(value float64) float64 {
	whole := float64(int64(value))
	if value < 0 && value != whole {
		return whole - 1
	}
	return whole
}
