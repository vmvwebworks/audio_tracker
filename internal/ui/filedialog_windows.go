package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	comdlg32             = windows.NewLazySystemDLL("comdlg32.dll")
	procGetOpenFileNameW = comdlg32.NewProc("GetOpenFileNameW")
)

type openFileName struct {
	structSize    uint32
	owner         uintptr
	instance      uintptr
	filter        *uint16
	customFilter  *uint16
	maxCustFilter uint32
	filterIndex   uint32
	file          *uint16
	maxFile       uint32
	fileTitle     *uint16
	maxFileTitle  uint32
	initialDir    *uint16
	title         *uint16
	flags         uint32
	fileOffset    uint16
	fileExtension uint16
	defExt        *uint16
	custData      uintptr
	hook          uintptr
	templateName  *uint16
	reserved      uintptr
	reserved2     uint32
	flagsEx       uint32
}

const (
	ofnFileMustExist = 0x00001000
	ofnPathMustExist = 0x00000800
	ofnExplorer      = 0x00080000
	ofnNoChangeDir   = 0x00000008
)

// openGPDialog shows the native "Open" dialog and returns the chosen path,
// or "" if cancelled. It blocks, so run it off the UI goroutine.
func openGPDialog(initial string) string {
	return openFileDialog(initial, "Abrir partitura de Guitar Pro", "Guitar Pro 7/8 (*.gp)|*.gp|Todos los archivos|*.*")
}

// openAudioDialog lets the user pick a WAV or MP3 file.
func openAudioDialog(initial string) string {
	return openFileDialog(initial, "Cargar el tema (audio exportado de Reaper)", "Audio (*.wav;*.mp3)|*.wav;*.mp3|Todos los archivos|*.*")
}

func openFileDialog(initial, dialogTitle, filters string) string {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	_ = windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED)
	defer windows.CoUninitialize()

	// The filter list is NUL-separated and double-NUL terminated.
	filter := utf16z(strings.ReplaceAll(filters, "|", string(rune(0))) + string(rune(0)) + string(rune(0)))
	buf := make([]uint16, 4096)
	title, _ := windows.UTF16PtrFromString(dialogTitle)
	var dir *uint16
	if initial != "" {
		dir, _ = windows.UTF16PtrFromString(filepath.Dir(initial))
	}
	ofn := openFileName{
		filter:     &filter[0],
		file:       &buf[0],
		maxFile:    uint32(len(buf)),
		initialDir: dir,
		title:      title,
		flags:      ofnFileMustExist | ofnPathMustExist | ofnExplorer | ofnNoChangeDir,
	}
	ofn.structSize = uint32(unsafe.Sizeof(ofn))
	r, _, _ := procGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if r == 0 {
		return ""
	}
	return windows.UTF16ToString(buf)
}

// utf16z encodes s (which carries its own NUL separators) as UTF-16.
func utf16z(s string) []uint16 {
	out := make([]uint16, 0, len(s))
	for _, r := range s {
		out = append(out, uint16(r))
	}
	return out
}

// showInExplorer opens Explorer on a folder, or on a file's folder with the
// file selected.
func showInExplorer(path string) error {
	args := `"` + path + `"`
	if st, err := os.Stat(path); err == nil && !st.IsDir() {
		args = `/select,"` + path + `"`
	}
	cmd := exec.Command("explorer.exe")
	// Explorer needs the quotes exactly as written, so bypass Go's quoting.
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: "explorer.exe " + args}
	return cmd.Start()
}

var (
	shell32              = windows.NewLazySystemDLL("shell32.dll")
	procSHFileOperationW = shell32.NewProc("SHFileOperationW")
)

type shFileOpStruct struct {
	hwnd                  uintptr
	wFunc                 uint32
	pFrom                 *uint16
	pTo                   *uint16
	fFlags                uint16
	fAnyOperationsAborted int32
	hNameMappings         uintptr
	lpszProgressTitle     *uint16
}

// moveToRecycleBin deletes a file by sending it to the Recycle Bin.
func moveToRecycleBin(path string) error {
	const (
		foDelete          = 3
		fofSilent         = 0x0004
		fofNoConfirmation = 0x0010
		fofAllowUndo      = 0x0040
		fofNoErrorUI      = 0x0400
	)
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	from, err := windows.UTF16FromString(abs)
	if err != nil {
		return err
	}
	from = append(from, 0) // the list is double-NUL terminated
	op := shFileOpStruct{
		wFunc:  foDelete,
		pFrom:  &from[0],
		fFlags: fofSilent | fofNoConfirmation | fofAllowUndo | fofNoErrorUI,
	}
	r, _, _ := procSHFileOperationW.Call(uintptr(unsafe.Pointer(&op)))
	if r != 0 {
		return fmt.Errorf("código %d", r)
	}
	if op.fAnyOperationsAborted != 0 {
		return errors.New("operación cancelada")
	}
	if _, err := os.Stat(path); err == nil {
		return errors.New("el archivo sigue en su sitio")
	}
	return nil
}
