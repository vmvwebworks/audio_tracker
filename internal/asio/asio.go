//go:build windows && amd64

// Package asio is a minimal pure-Go host for Steinberg ASIO drivers on 64-bit
// Windows. ASIO drivers are in-process COM objects registered under
// HKLM\SOFTWARE\ASIO; we instantiate them with CoCreateInstance and call the
// IASIO vtable directly. On amd64 there is a single calling convention, so no
// cgo or thiscall shims are required.
//
// ASIO only allows one driver to be loaded per process at a time.
package asio

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
	user32               = windows.NewLazySystemDLL("user32.dll")
	procGetDesktopWindow = user32.NewProc("GetDesktopWindow")
)

// IASIO vtable slots (0-2 are IUnknown).
const (
	vtRelease         = 2
	vtInit            = 3
	vtGetDriverName   = 4
	vtGetErrorMessage = 6
	vtStart           = 7
	vtStop            = 8
	vtGetChannels     = 9
	vtGetLatencies    = 10
	vtGetBufferSize   = 11
	vtCanSampleRate   = 12
	vtGetSampleRate   = 13
	vtSetSampleRate   = 14
	vtGetChannelInfo  = 18
	vtCreateBuffers   = 19
	vtDisposeBuffers  = 20
	vtControlPanel    = 21
	vtOutputReady     = 23
)

const (
	aseOK      = 0
	aseSuccess = 0x3f4847a0
)

// Host message selectors (asioMessage callback).
const (
	selSelectorSupported = 1
	selEngineVersion     = 2
	selResetRequest      = 3
	selBufferSizeChange  = 4
	selResyncRequest     = 5
	selLatenciesChanged  = 6
	selSupportsTimeInfo  = 7
)

// SampleType is the driver's native sample format for a channel.
type SampleType int32

const (
	Int16LSB   SampleType = 16
	Int24LSB   SampleType = 17
	Int32LSB   SampleType = 18
	Float32LSB SampleType = 19
	Float64LSB SampleType = 20
	Int32LSB16 SampleType = 24
	Int32LSB18 SampleType = 25
	Int32LSB20 SampleType = 26
	Int32LSB24 SampleType = 27
)

func (t SampleType) supported() bool {
	switch t {
	case Int16LSB, Int24LSB, Int32LSB, Float32LSB, Float64LSB,
		Int32LSB16, Int32LSB18, Int32LSB20, Int32LSB24:
		return true
	}
	return false
}

// DriverInfo identifies an installed ASIO driver.
type DriverInfo struct {
	Name  string
	CLSID windows.GUID
}

// ListDrivers returns the ASIO drivers registered on this machine.
func ListDrivers() ([]DriverInfo, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\ASIO`, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil, fmt.Errorf("no hay drivers ASIO instalados: %w", err)
	}
	defer k.Close()
	names, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return nil, err
	}
	var out []DriverInfo
	for _, n := range names {
		sk, err := registry.OpenKey(k, n, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		s, _, err := sk.GetStringValue("CLSID")
		sk.Close()
		if err != nil {
			continue
		}
		g, err := windows.GUIDFromString(s)
		if err != nil {
			continue
		}
		out = append(out, DriverInfo{Name: n, CLSID: g})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Processor is called from the driver's audio thread once per buffer.
// in and out hold one slice per active channel, each of the buffer length.
// It must not block or allocate heavily.
type Processor func(in, out [][]float32)

type bufferInfo struct {
	IsInput    int32
	ChannelNum int32
	Buffers    [2]unsafe.Pointer
}

type channelInfo struct {
	Channel      int32
	IsInput      int32
	IsActive     int32
	ChannelGroup int32
	Type         SampleType
	Name         [32]byte
}

type callbacks struct {
	bufferSwitch         uintptr
	sampleRateDidChange  uintptr
	asioMessage          uintptr
	bufferSwitchTimeInfo uintptr
}

// Driver is a loaded ASIO driver. All methods must be called from a single
// goroutine (or externally serialised); the audio callback runs on the
// driver's own thread.
type Driver struct {
	info DriverInfo
	obj  unsafe.Pointer
	com  *comThread

	numIn, numOut int

	// Streaming state, valid between Start and Stop.
	running        bool
	bufSize        int
	infos          []bufferInfo
	inTypes        []SampleType
	outTypes       []SampleType
	inBuf, outBuf  [][]float32
	proc           Processor
	hasOutputReady bool

	resetRequested atomic.Bool
}

var (
	cbOnce  sync.Once
	cbs     callbacks
	current atomic.Pointer[Driver]
)

func initCallbacks() {
	cbs.bufferSwitch = syscall.NewCallback(func(index, direct uintptr) uintptr {
		if d := current.Load(); d != nil {
			d.process(int(int32(index)) & 1)
		}
		return 0
	})
	cbs.sampleRateDidChange = syscall.NewCallback(func(rate uintptr) uintptr {
		if d := current.Load(); d != nil {
			d.resetRequested.Store(true)
		}
		return 0
	})
	cbs.asioMessage = syscall.NewCallback(func(selector, value, message, opt uintptr) uintptr {
		sel, val := int32(selector), int32(value)
		switch sel {
		case selSelectorSupported:
			switch val {
			case selEngineVersion, selResetRequest, selBufferSizeChange, selResyncRequest, selLatenciesChanged:
				return 1
			}
			return 0
		case selEngineVersion:
			return 2
		case selResetRequest, selBufferSizeChange:
			if d := current.Load(); d != nil {
				d.resetRequested.Store(true)
			}
			return 1
		case selResyncRequest, selLatenciesChanged:
			return 1
		case selSupportsTimeInfo:
			return 0
		}
		return 0
	})
	cbs.bufferSwitchTimeInfo = syscall.NewCallback(func(params, index, direct uintptr) uintptr {
		if d := current.Load(); d != nil {
			d.process(int(int32(index)) & 1)
		}
		return params
	})
}

// Open loads and initialises a driver.
func Open(info DriverInfo) (*Driver, error) {
	if current.Load() != nil {
		return nil, errors.New("ya hay un driver ASIO abierto")
	}
	cbOnce.Do(initCallbacks)
	d := &Driver{info: info, com: newComThread()}
	var err error
	d.com.do(func() {
		var obj unsafe.Pointer
		hr, _, _ := procCoCreateInstance.Call(
			uintptr(unsafe.Pointer(&info.CLSID)), 0, 1, // CLSCTX_INPROC_SERVER
			uintptr(unsafe.Pointer(&info.CLSID)), uintptr(unsafe.Pointer(&obj)))
		if int32(hr) != 0 || obj == nil {
			err = fmt.Errorf("no se pudo cargar %q (HRESULT 0x%08x)", info.Name, uint32(hr))
			return
		}
		d.obj = obj
		hwnd, _, _ := procGetDesktopWindow.Call()
		if int32(d.call(vtInit, hwnd)) == 0 {
			err = fmt.Errorf("el driver %q no se pudo inicializar: %s", info.Name, d.errorMessage())
			d.call(vtRelease)
			d.obj = nil
			return
		}
		var in, out int32
		if e := int32(d.call(vtGetChannels, uintptr(unsafe.Pointer(&in)), uintptr(unsafe.Pointer(&out)))); e != aseOK {
			err = fmt.Errorf("getChannels: %s", d.errorMessage())
			return
		}
		d.numIn, d.numOut = int(in), int(out)
	})
	if err != nil {
		if d.obj != nil {
			d.com.do(func() { d.call(vtRelease) })
		}
		d.com.close()
		return nil, err
	}
	current.Store(d)
	return d, nil
}

func (d *Driver) call(slot int, args ...uintptr) uintptr {
	vtbl := *(*unsafe.Pointer)(d.obj)
	fn := *(*uintptr)(unsafe.Add(vtbl, slot*int(unsafe.Sizeof(uintptr(0)))))
	all := make([]uintptr, 0, len(args)+1)
	all = append(all, uintptr(d.obj))
	all = append(all, args...)
	r, _, _ := syscall.SyscallN(fn, all...)
	return r
}

func (d *Driver) errorMessage() string {
	var buf [124]byte
	d.call(vtGetErrorMessage, uintptr(unsafe.Pointer(&buf[0])))
	return cstr(buf[:])
}

func cstr(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// Info returns the registry entry this driver was loaded from.
func (d *Driver) Info() DriverInfo { return d.info }

// Channels returns the number of hardware input and output channels.
func (d *Driver) Channels() (in, out int) { return d.numIn, d.numOut }

// ChannelNames returns the driver's names for the input or output channels.
func (d *Driver) ChannelNames(input bool) []string {
	n := d.numOut
	if input {
		n = d.numIn
	}
	names := make([]string, n)
	d.com.do(func() {
		for i := range names {
			ci := channelInfo{Channel: int32(i)}
			if input {
				ci.IsInput = 1
			}
			if int32(d.call(vtGetChannelInfo, uintptr(unsafe.Pointer(&ci)))) == aseOK {
				names[i] = cstr(ci.Name[:])
			}
			if names[i] == "" {
				names[i] = fmt.Sprintf("Canal %d", i+1)
			}
		}
	})
	return names
}

// SampleRate returns the driver's current sample rate.
func (d *Driver) SampleRate() float64 {
	var sr float64
	d.com.do(func() { d.call(vtGetSampleRate, uintptr(unsafe.Pointer(&sr))) })
	return sr
}

// SetSampleRate asks the driver to switch sample rate.
func (d *Driver) SetSampleRate(sr float64) error {
	var err error
	d.com.do(func() {
		bits := uintptr(math.Float64bits(sr))
		if int32(d.call(vtCanSampleRate, bits)) != aseOK {
			err = fmt.Errorf("el driver no admite %.0f Hz", sr)
			return
		}
		if int32(d.call(vtSetSampleRate, bits)) != aseOK {
			err = fmt.Errorf("setSampleRate: %s", d.errorMessage())
		}
	})
	return err
}

// BufferSizes returns the driver's buffer size constraints in samples.
func (d *Driver) BufferSizes() (min, max, preferred, granularity int) {
	var mn, mx, pr, gr int32
	d.com.do(func() {
		d.call(vtGetBufferSize, uintptr(unsafe.Pointer(&mn)), uintptr(unsafe.Pointer(&mx)),
			uintptr(unsafe.Pointer(&pr)), uintptr(unsafe.Pointer(&gr)))
	})
	return int(mn), int(mx), int(pr), int(gr)
}

// Latencies returns the input and output latencies in samples reported by
// the driver (valid after Start).
func (d *Driver) Latencies() (in, out int) {
	var li, lo int32
	d.com.do(func() {
		d.call(vtGetLatencies, uintptr(unsafe.Pointer(&li)), uintptr(unsafe.Pointer(&lo)))
	})
	return int(li), int(lo)
}

// BufferSize returns the active buffer size (valid after Start).
func (d *Driver) BufferSize() int { return d.bufSize }

// ResetRequested reports (and clears) whether the driver asked the host to
// restart, e.g. after the user changed settings in its control panel.
func (d *Driver) ResetRequested() bool { return d.resetRequested.Swap(false) }

// ControlPanel opens the driver's settings dialog. It may block until the
// dialog is closed.
func (d *Driver) ControlPanel() {
	d.com.do(func() { d.call(vtControlPanel) })
}

// Start creates buffers for the given input and output channel indices and
// starts streaming, calling proc for every buffer.
func (d *Driver) Start(inCh, outCh []int, proc Processor) error {
	if d.running {
		return errors.New("el driver ya está en marcha")
	}
	var err error
	d.com.do(func() {
		_, _, pref, _ := d.bufferSizesLocked()
		d.bufSize = pref
		d.proc = proc
		d.infos = make([]bufferInfo, 0, len(inCh)+len(outCh))
		d.inTypes = d.inTypes[:0]
		d.outTypes = d.outTypes[:0]
		for _, c := range inCh {
			d.infos = append(d.infos, bufferInfo{IsInput: 1, ChannelNum: int32(c)})
		}
		for _, c := range outCh {
			d.infos = append(d.infos, bufferInfo{IsInput: 0, ChannelNum: int32(c)})
		}
		for _, bi := range d.infos {
			ci := channelInfo{Channel: bi.ChannelNum, IsInput: bi.IsInput}
			if int32(d.call(vtGetChannelInfo, uintptr(unsafe.Pointer(&ci)))) != aseOK {
				err = fmt.Errorf("getChannelInfo(%d): %s", bi.ChannelNum, d.errorMessage())
				return
			}
			if !ci.Type.supported() {
				err = fmt.Errorf("formato de muestra no soportado: %d", ci.Type)
				return
			}
			if bi.IsInput == 1 {
				d.inTypes = append(d.inTypes, ci.Type)
			} else {
				d.outTypes = append(d.outTypes, ci.Type)
			}
		}
		d.inBuf = makeBufs(len(inCh), d.bufSize)
		d.outBuf = makeBufs(len(outCh), d.bufSize)
		if e := int32(d.call(vtCreateBuffers, uintptr(unsafe.Pointer(&d.infos[0])),
			uintptr(len(d.infos)), uintptr(d.bufSize), uintptr(unsafe.Pointer(&cbs)))); e != aseOK {
			err = fmt.Errorf("createBuffers: %s", d.errorMessage())
			return
		}
		d.hasOutputReady = int32(d.call(vtOutputReady)) == aseOK
		if e := int32(d.call(vtStart)); e != aseOK {
			d.call(vtDisposeBuffers)
			err = fmt.Errorf("start: %s", d.errorMessage())
			return
		}
		d.running = true
	})
	return err
}

func (d *Driver) bufferSizesLocked() (min, max, preferred, granularity int) {
	var mn, mx, pr, gr int32
	d.call(vtGetBufferSize, uintptr(unsafe.Pointer(&mn)), uintptr(unsafe.Pointer(&mx)),
		uintptr(unsafe.Pointer(&pr)), uintptr(unsafe.Pointer(&gr)))
	return int(mn), int(mx), int(pr), int(gr)
}

func makeBufs(n, size int) [][]float32 {
	b := make([][]float32, n)
	for i := range b {
		b[i] = make([]float32, size)
	}
	return b
}

// Stop halts streaming and releases the driver buffers.
func (d *Driver) Stop() {
	if !d.running {
		return
	}
	d.com.do(func() {
		d.call(vtStop)
		d.call(vtDisposeBuffers)
	})
	d.running = false
}

// Close stops streaming and unloads the driver.
func (d *Driver) Close() {
	d.Stop()
	d.com.do(func() { d.call(vtRelease) })
	d.com.close()
	current.CompareAndSwap(d, nil)
}

// process runs on the driver's audio thread.
func (d *Driver) process(idx int) {
	if !d.running && d.proc == nil {
		return
	}
	nIn := len(d.inTypes)
	for i := range nIn {
		toFloat(d.inTypes[i], d.infos[i].Buffers[idx], d.inBuf[i])
	}
	for _, b := range d.outBuf {
		clear(b)
	}
	d.proc(d.inBuf, d.outBuf)
	for i, t := range d.outTypes {
		fromFloat(t, d.infos[nIn+i].Buffers[idx], d.outBuf[i])
	}
	if d.hasOutputReady {
		d.call(vtOutputReady)
	}
}

func toFloat(t SampleType, p unsafe.Pointer, dst []float32) {
	n := len(dst)
	switch t {
	case Int16LSB:
		for i, v := range unsafe.Slice((*int16)(p), n) {
			dst[i] = float32(v) / (1 << 15)
		}
	case Int24LSB:
		b := unsafe.Slice((*byte)(p), n*3)
		for i := range dst {
			v := int32(uint32(b[3*i])<<8 | uint32(b[3*i+1])<<16 | uint32(b[3*i+2])<<24)
			dst[i] = float32(v) / (1 << 31)
		}
	case Int32LSB:
		for i, v := range unsafe.Slice((*int32)(p), n) {
			dst[i] = float32(v) / (1 << 31)
		}
	case Float32LSB:
		copy(dst, unsafe.Slice((*float32)(p), n))
	case Float64LSB:
		for i, v := range unsafe.Slice((*float64)(p), n) {
			dst[i] = float32(v)
		}
	case Int32LSB16, Int32LSB18, Int32LSB20, Int32LSB24:
		scale := float32(int32(1) << (int(t-Int32LSB16)*2 + 15))
		for i, v := range unsafe.Slice((*int32)(p), n) {
			dst[i] = float32(v) / scale
		}
	}
}

func clip(v float32) float32 {
	if v > 1 {
		return 1
	}
	if v < -1 {
		return -1
	}
	return v
}

func fromFloat(t SampleType, p unsafe.Pointer, src []float32) {
	n := len(src)
	switch t {
	case Int16LSB:
		s := unsafe.Slice((*int16)(p), n)
		for i, v := range src {
			s[i] = int16(clip(v) * 32767)
		}
	case Int24LSB:
		b := unsafe.Slice((*byte)(p), n*3)
		for i, v := range src {
			x := int32(clip(v) * 8388607)
			b[3*i], b[3*i+1], b[3*i+2] = byte(x), byte(x>>8), byte(x>>16)
		}
	case Int32LSB:
		s := unsafe.Slice((*int32)(p), n)
		for i, v := range src {
			s[i] = int32(float64(clip(v)) * 2147483647)
		}
	case Float32LSB:
		s := unsafe.Slice((*float32)(p), n)
		for i, v := range src {
			s[i] = clip(v)
		}
	case Float64LSB:
		s := unsafe.Slice((*float64)(p), n)
		for i, v := range src {
			s[i] = float64(clip(v))
		}
	case Int32LSB16, Int32LSB18, Int32LSB20, Int32LSB24:
		scale := float32(int32(1)<<(int(t-Int32LSB16)*2+15) - 1)
		s := unsafe.Slice((*int32)(p), n)
		for i, v := range src {
			s[i] = int32(clip(v) * scale)
		}
	}
}

// comThread runs functions on a single OS thread initialised as a COM STA,
// which is what ASIO drivers expect for their control calls.
type comThread struct {
	ch   chan func()
	done chan struct{}
}

func newComThread() *comThread {
	t := &comThread{ch: make(chan func()), done: make(chan struct{})}
	ready := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		_ = windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED)
		close(ready)
		for f := range t.ch {
			f()
		}
		windows.CoUninitialize()
		close(t.done)
	}()
	<-ready
	return t
}

func (t *comThread) do(f func()) {
	done := make(chan struct{})
	t.ch <- func() { f(); close(done) }
	<-done
}

func (t *comThread) close() {
	close(t.ch)
	<-t.done
}
