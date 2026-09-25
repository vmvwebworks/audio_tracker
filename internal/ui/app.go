// Package ui is the Gio user interface.
package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/vmvwebworks/audio_tracker/internal/engine"
	"github.com/vmvwebworks/audio_tracker/internal/gp"
)

var (
	colBg     = rgb(0x1e2025)
	colPanel  = rgb(0x272a30)
	colSel    = rgb(0x343945)
	colText   = rgb(0xe6e6e6)
	colDim    = rgb(0x9aa0a6)
	colBtn    = rgb(0x3a3e47)
	colAccent = rgb(0x3d7be0)
	colRec    = rgb(0xd8403a)
	colGreen  = rgb(0x3aa55d)
	colAmber  = rgb(0xd99a1e)
	colWhite  = rgb(0xffffff)
)

type (
	C = layout.Context
	D = layout.Dimensions
)

type trackUI struct {
	sel, mute, solo widget.Clickable
	muted, soloed   bool
	vol             widget.Float
	lastVol         float32
}

// App is the main window.
type App struct {
	w   *app.Window
	th  *material.Theme
	eng *engine.Engine
	cfg *Config

	song     *gp.Song
	tl       *gp.Timeline
	path     string
	tracks   []trackUI
	selTrack int
	tab      *TabView

	btnOpen, btnPlay, btnStop, btnRec, btnMetro, btnMon, btnTake, btnFollow widget.Clickable
	btnArm, btnIn2, btnFolder                                               widget.Clickable
	gain                                                                    widget.Float
	lastGain                                                                float32
	btnDriver, btnPanel, btnIn, btnOut, btnLatMinus, btnLatPlus             widget.Clickable

	recArm, takeOn        bool
	hasTake               bool
	panelOpen, dialogOpen bool

	inNames, outNames []string
	status            string
	statusErr         bool
	inLevel, outLevel float32
	markPos           float64
	lastTake          string
	takes             []*takeItem
	previewing        string // take being auditioned alone
	songTake          string // take heard with the song
	renaming          string // path of the take being renamed
	confirmDel        string // path of the take awaiting delete confirmation
	renameEd          widget.Editor
	onlyFavs          bool
	btnFavFilter      widget.Clickable
	bk                backingUI
	focused           bool
	suspended         bool // audio device released while in the background

	bg        chan func()
	trackList layout.List
}

// Run opens the window and blocks until it is closed.
func Run(eng *engine.Engine, cfg *Config) error {
	w := new(app.Window)
	w.Option(app.Title("Audio Tracker"), app.Size(1280, 820), app.MinSize(900, 560))
	th := material.NewTheme()
	th.Palette = material.Palette{Bg: colBg, Fg: colText, ContrastBg: colAccent, ContrastFg: colWhite}
	a := &App{
		w: w, th: th, eng: eng, cfg: cfg,
		tab:       NewTabView(th),
		bg:        make(chan func(), 16),
		trackList: layout.List{Axis: layout.Vertical},
	}
	a.focused = true
	eng.SetMetronome(cfg.Metronome)
	eng.SetInputGain(cfg.InputGainDb)
	a.gain.Value = float32(cfg.InputGainDb / maxGainDb)
	a.lastGain = a.gain.Value
	a.openDriver(cfg.Driver)
	if cfg.LastFile != "" {
		if _, err := os.Stat(cfg.LastFile); err == nil {
			a.loadSong(cfg.LastFile)
		}
	}

	var ops op.Ops
	for {
		switch e := w.Event().(type) {
		case app.ConfigEvent:
			a.focused = e.Config.Focused
			w.Invalidate()
		case app.DestroyEvent:
			a.shutdown()
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			a.frame(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

func (a *App) post(f func()) {
	a.bg <- f
	a.w.Invalidate()
}

func (a *App) setStatus(err bool, format string, args ...any) {
	a.status = fmt.Sprintf(format, args...)
	a.statusErr = err
}

func (a *App) shutdown() {
	if a.eng.Playing() {
		res, err := a.eng.Pause()
		a.reportTake(res, err)
	}
	a.eng.CloseDriver()
	_ = a.cfg.Save()
}

// --- actions -------------------------------------------------------------

func (a *App) openDriver(name string) {
	if a.eng.Recording() {
		return
	}
	_ = a.eng.SetInput(a.cfg.Input)
	_ = a.eng.SetOutputs(a.cfg.OutL, a.cfg.OutR)
	if err := a.eng.OpenDriver(name); err != nil {
		a.setStatus(true, "Error de audio: %v", err)
	} else {
		a.setStatus(false, "Driver %s listo", a.eng.DriverName())
	}
	a.afterStreamChange()
}

func (a *App) afterStreamChange() {
	a.inNames = a.eng.ChannelNames(true)
	a.outNames = a.eng.ChannelNames(false)
	if n := a.eng.DriverName(); n != "" {
		a.cfg.Driver = n
	}
	a.cfg.Input = a.eng.Input()
	a.cfg.OutL, a.cfg.OutR = a.eng.Outputs()
	a.applyLatencyAdjust()
	_ = a.cfg.Save()
	// The backing track is decoded at the stream's sample rate.
	if a.song != nil && a.eng.Running() && !a.bk.loading && a.bk.proj.Backing != "" &&
		(a.bk.loaded == nil || a.bk.sr != a.eng.SampleRate()) {
		a.openBacking()
	}
}

func (a *App) applyLatencyAdjust() {
	a.eng.LatencyAdjust = int(math.Round(a.cfg.LatencyAdjustMs * a.eng.SampleRate() / 1000))
}

func (a *App) loadSong(path string) {
	if a.eng.Playing() {
		res, err := a.eng.Pause()
		a.reportTake(res, err)
	}
	s, err := gp.Load(path)
	if err != nil {
		a.setStatus(true, "No se pudo abrir %s: %v", filepath.Base(path), err)
		return
	}
	tl := gp.NewTimeline(s)
	a.song, a.tl, a.path = s, tl, path
	a.eng.LoadSong(s, tl)
	a.tracks = make([]trackUI, len(s.Tracks))
	a.selTrack = 0
	for i, t := range s.Tracks {
		a.tracks[i].vol.Value = float32(t.Volume)
		a.tracks[i].lastVol = float32(t.Volume)
		if t.Percussion && a.selTrack == i && i+1 < len(s.Tracks) {
			a.selTrack = i + 1
		}
	}
	a.tab.SetSong(s, tl, a.selTrack)
	a.eng.StopPreview()
	a.previewing = ""
	a.refreshTakes()
	a.loadProject()
	a.openBacking()
	a.hasTake, a.takeOn, a.markPos = false, false, 0
	a.eng.SetTakePlayback(false)
	a.cfg.LastFile = path
	_ = a.cfg.Save()
	title := s.Title
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	a.w.Option(app.Title(title + " — Audio Tracker"))
	a.setStatus(false, "Cargado %s: %d pistas, %d compases, %s", filepath.Base(path),
		len(s.Tracks), len(s.MasterBars), clock(tl.Duration()))
}

func (a *App) applyAudible() {
	if a.song == nil {
		return
	}
	anySolo := false
	for _, t := range a.tracks {
		anySolo = anySolo || t.soloed
	}
	aud := make([]bool, len(a.tracks))
	vols := make([]float64, len(a.tracks))
	for i, t := range a.tracks {
		aud[i] = !t.muted
		if anySolo {
			aud[i] = t.soloed
		}
		vols[i] = float64(t.vol.Value)
	}
	a.eng.SetAudible(aud, vols)
}

func (a *App) togglePlay() {
	if a.song == nil {
		return
	}
	if a.eng.Playing() {
		res, err := a.eng.Pause()
		a.reportTake(res, err)
		return
	}
	a.markPos = a.eng.Position()
	if a.recArm && !a.startTake() {
		return
	}
	if err := a.eng.Play(); err != nil {
		a.eng.StopRecording()
		a.setStatus(true, "%v", err)
	}
}

// startTake begins recording the input; with the song playing this is a
// punch-in from the current position.
func (a *App) startTake() bool {
	p, err := a.takePath()
	if err == nil {
		err = a.eng.StartRecording(p)
	}
	if err != nil {
		a.setStatus(true, "No se pudo empezar a grabar: %v", err)
		return false
	}
	a.setStatus(false, "Grabando «%s» en %s", a.inputName(), p)
	return true
}

// toggleArm arms or disarms the input track. Arming while playing starts
// recording immediately; disarming while recording ends the take and keeps
// the song playing.
func (a *App) toggleArm() {
	if a.song == nil {
		a.setStatus(true, "Abre primero un archivo .gp")
		return
	}
	a.recArm = !a.recArm
	defer a.applyMonitor()
	switch {
	case a.recArm && a.eng.Playing():
		if !a.startTake() {
			a.recArm = false
		}
	case a.recArm && !a.inputConnected():
		a.setStatus(true, "Pista armada, pero se grabará silencio. %s", inputHelp)
	case a.recArm:
		a.setStatus(false, "Pista armada: pulsa Reproducir (Espacio) y se grabará «%s». Clic en un compás para empezar desde ahí.", a.inputName())
	case a.eng.Recording():
		res, err := a.eng.StopRecording()
		a.reportTake(res, err)
	default:
		a.setStatus(false, "Pista desarmada")
	}
}

// recordNow is the toolbar Record button: arm and roll, or stop the take.
func (a *App) recordNow() {
	if a.eng.Recording() {
		res, err := a.eng.Pause()
		a.reportTake(res, err)
		return
	}
	if !a.recArm {
		a.toggleArm()
		if !a.recArm {
			return
		}
	}
	if !a.eng.Playing() {
		a.togglePlay()
	}
}

func (a *App) inputName() string {
	if i := a.eng.Input(); i < len(a.inNames) {
		return a.inNames[i]
	}
	return "entrada"
}

func (a *App) stop() {
	if a.eng.Playing() {
		res, err := a.eng.Pause()
		a.reportTake(res, err)
		a.eng.Seek(a.markPos)
		return
	}
	a.markPos = 0
	a.eng.Seek(0)
}

func (a *App) takePath() (string, error) {
	dir := a.takesDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "toma "+time.Now().Format("2006-01-02 15.04.05")+".wav"), nil
}

func (a *App) reportTake(res *engine.TakeResult, err error) {
	if err != nil {
		a.setStatus(true, "Error guardando la toma: %v", err)
		return
	}
	if res == nil {
		return
	}
	a.hasTake = true
	bar := a.barAt(res.Start) + 1
	where := fmt.Sprintf("compás %d", bar)
	if bar == 0 {
		where = "intro"
	}
	// Name the file after where it starts in the song.
	named := strings.TrimSuffix(res.Path, ".wav") +
		fmt.Sprintf(" - %s (%s).wav", where, strings.ReplaceAll(clock(res.Start), ":", "-"))
	if err := os.Rename(res.Path, named); err == nil {
		res.Path = named
	}
	a.refreshTakes()
	a.lastTake = res.Path
	a.songTake = res.Path
	msg := fmt.Sprintf("Toma guardada: %s desde %s (%s) → %s",
		clock(res.Duration), where, clock(res.Start), filepath.Base(res.Path))
	if res.Peak < 0.1 {
		peak := 20 * math.Log10(float64(max(res.Peak, 1e-6)))
		a.setStatus(true, "%s — La señal es muy baja (pico %.0f dB): sube la Ganancia en la tarjeta de Grabación o el volumen del micrófono en Windows.", msg, peak)
		return
	}
	if res.Dropped {
		a.setStatus(true, "%s — ATENCIÓN: se perdieron muestras", msg)
		return
	}
	a.setStatus(false, "%s", msg)
}

func (a *App) seekMaster(m int) {
	if a.tl == nil || a.eng.Recording() {
		return
	}
	i := a.tl.FirstOccurrence(m)
	if i < 0 {
		return
	}
	t := a.sessionTime(a.tl.Bars[i].Time)
	a.markPos = t
	a.eng.Seek(t)
}

// --- frame ---------------------------------------------------------------

func (a *App) frame(gtx C) {
drain:
	for {
		select {
		case f := <-a.bg:
			f()
		default:
			break drain
		}
	}
	a.manageBackgroundAudio()
	if !a.panelOpen {
		if restarted, err := a.eng.CheckReset(); restarted {
			if err != nil {
				a.setStatus(true, "Error reiniciando el audio: %v", err)
			} else {
				a.setStatus(false, "Audio reiniciado con la nueva configuración del driver")
			}
			a.afterStreamChange()
		}
	}
	if res, err := a.eng.Poll(); res != nil || err != nil {
		a.reportTake(res, err)
		a.eng.Seek(a.markPos)
	}
	if a.eng.FeedbackTripped() {
		a.cfg.Monitor = false
		a.applyMonitor()
		_ = a.cfg.Save()
		a.setStatus(true, "¡Acople detectado! He apagado el monitor. Úsalo solo con auriculares, o baja la ganancia y el volumen.")
	}
	a.inLevel = max(a.eng.InputPeak(), a.inLevel*0.9)
	a.outLevel = max(a.eng.OutputPeak(), a.outLevel*0.9)

	a.handleInput(gtx)

	paint.Fill(gtx.Ops, colBg)
	layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(a.toolbar),
		layout.Rigid(a.deviceBar),
		layout.Flexed(1, a.body),
		layout.Rigid(a.statusBar),
	)

	if a.eng.Playing() {
		gtx.Execute(op.InvalidateCmd{})
	} else {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(50 * time.Millisecond)})
	}
}

func (a *App) handleInput(gtx C) {
	// Buttons grab keyboard focus when pressed, and a focused button reacts to
	// Space. Clear focus every frame so Space always means play/pause.
	// While a take is being renamed the editor needs focus and the keys.
	if a.renaming == "" && a.bk.editing == "" {
		gtx.Execute(key.FocusCmd{})
		for {
			ev, ok := gtx.Event(
				key.Filter{Name: key.NameSpace},
				key.Filter{Name: "R"},
				key.Filter{Name: key.NameHome},
				key.Filter{Name: "I"},
				key.Filter{Name: "T"},
			)
			if !ok {
				break
			}
			if e, ok := ev.(key.Event); ok && e.State == key.Press {
				switch e.Name {
				case key.NameSpace:
					a.togglePlay()
				case "R":
					a.toggleArm()
				case key.NameHome:
					a.stop()
				case "I":
					a.markBar1()
				case "T":
					a.markTempo()
				}
			}
		}
	}

	click := func(c *widget.Clickable) bool { return c.Clicked(gtx) }
	a.handleTakes(gtx)
	a.handleBacking(gtx)
	if click(&a.btnOpen) && !a.dialogOpen && !a.eng.Recording() {
		a.dialogOpen = true
		initial := a.cfg.LastFile
		go func() {
			p := openGPDialog(initial)
			a.post(func() {
				a.dialogOpen = false
				if p != "" {
					a.loadSong(p)
				}
			})
		}()
	}
	if click(&a.btnPlay) {
		a.togglePlay()
	}
	if click(&a.btnStop) {
		a.stop()
	}
	if click(&a.btnRec) {
		a.recordNow()
	}
	if click(&a.btnArm) {
		a.toggleArm()
	}
	if click(&a.btnMetro) {
		a.cfg.Metronome = !a.cfg.Metronome
		a.eng.SetMetronome(a.cfg.Metronome)
		_ = a.cfg.Save()
	}
	if click(&a.btnMon) {
		a.cfg.Monitor = !a.cfg.Monitor
		a.applyMonitor()
		_ = a.cfg.Save()
		if a.cfg.Monitor {
			a.setStatus(false, "Monitor activado: al armar REC te oirás por la app. Usa AURICULARES: con altavoces se acopla.")
		} else {
			a.setStatus(false, "Monitor desactivado")
		}
	}
	if click(&a.btnTake) && a.hasTake {
		a.takeOn = !a.takeOn
		a.eng.SetTakePlayback(a.takeOn)
	}
	if click(&a.btnFolder) {
		a.openTakesFolder()
	}
	if click(&a.btnFollow) {
		a.tab.Follow = !a.tab.Follow
	}
	if click(&a.btnDriver) && !a.panelOpen && !a.eng.Playing() {
		drivers := a.eng.Drivers()
		if len(drivers) > 0 {
			next := 0
			for i, d := range drivers {
				if d.Name == a.eng.DriverName() {
					next = (i + 1) % len(drivers)
				}
			}
			a.openDriver(drivers[next].Name)
		}
	}
	if click(&a.btnPanel) && !a.panelOpen && a.eng.DriverName() != "" && !a.eng.Playing() {
		a.panelOpen = true
		go func() {
			a.eng.ControlPanel()
			a.post(func() { a.panelOpen = false })
		}()
	}
	if (click(&a.btnIn) || click(&a.btnIn2)) && len(a.inNames) > 0 && !a.panelOpen && !a.eng.Playing() {
		if err := a.eng.SetInput((a.eng.Input() + 1) % len(a.inNames)); err != nil {
			a.setStatus(true, "Error cambiando la entrada: %v", err)
		}
		a.afterStreamChange()
	}
	if click(&a.btnOut) && len(a.outNames) > 1 && !a.panelOpen && !a.eng.Playing() {
		l, _ := a.eng.Outputs()
		l = (l/2*2 + 2) % (len(a.outNames) / 2 * 2)
		if err := a.eng.SetOutputs(l, min(l+1, len(a.outNames)-1)); err != nil {
			a.setStatus(true, "Error cambiando la salida: %v", err)
		}
		a.afterStreamChange()
	}
	if click(&a.btnLatMinus) {
		a.cfg.LatencyAdjustMs--
		a.applyLatencyAdjust()
		_ = a.cfg.Save()
	}
	if click(&a.btnLatPlus) {
		a.cfg.LatencyAdjustMs++
		a.applyLatencyAdjust()
		_ = a.cfg.Save()
	}

	if a.gain.Value != a.lastGain {
		a.lastGain = a.gain.Value
		a.cfg.InputGainDb = math.Round(float64(a.gain.Value) * maxGainDb)
		a.eng.SetInputGain(a.cfg.InputGainDb)
		_ = a.cfg.Save()
	}

	changed := false
	for i := range a.tracks {
		t := &a.tracks[i]
		if click(&t.sel) {
			a.selTrack = i
			a.tab.SetTrack(i)
		}
		if click(&t.mute) {
			t.muted = !t.muted
			changed = true
		}
		if click(&t.solo) {
			t.soloed = !t.soloed
			changed = true
		}
		if t.vol.Value != t.lastVol && !t.vol.Dragging() {
			t.lastVol = t.vol.Value
			changed = true
		} else if t.vol.Dragging() && math.Abs(float64(t.vol.Value-t.lastVol)) > 0.02 {
			t.lastVol = t.vol.Value
			changed = true
		}
	}
	if changed {
		a.applyAudible()
	}
}

// --- layout --------------------------------------------------------------

func (a *App) button(gtx C, c *widget.Clickable, label string, on bool, onCol color.NRGBA) D {
	b := material.Button(a.th, c, label)
	b.TextSize = 13
	b.CornerRadius = 4
	b.Inset = layout.Inset{Top: 7, Bottom: 7, Left: 12, Right: 12}
	b.Background, b.Color = colBtn, colText
	if on {
		b.Background, b.Color = onCol, colWhite
	}
	return layout.Inset{Right: 6}.Layout(gtx, b.Layout)
}

func (a *App) label(gtx C, size unit.Sp, col color.NRGBA, s string) D {
	l := material.Label(a.th, size, s)
	l.Color = col
	l.MaxLines = 1
	return l.Layout(gtx)
}

func (a *App) toolbar(gtx C) D {
	return layout.Inset{Top: 8, Bottom: 6, Left: 10, Right: 10}.Layout(gtx, func(gtx C) D {
		playing := a.eng.Playing()
		playIc, playTip := icPlay, "Reproducir (Espacio)"
		if playing {
			playIc, playTip = icPause, "Pausa (Espacio)"
		}
		recTip := "Grabar"
		if a.eng.Recording() {
			recTip = "Parar grabación"
		}
		var none color.NRGBA
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D { return a.toolIcon(gtx, &a.btnOpen, icOpen, "Abrir .gp", false, colAccent, none) }),
			layout.Rigid(layout.Spacer{Width: 10}.Layout),
			layout.Rigid(func(gtx C) D { return a.toolIcon(gtx, &a.btnPlay, playIc, playTip, playing, colGreen, none) }),
			layout.Rigid(func(gtx C) D { return a.toolIcon(gtx, &a.btnStop, icStop, "Parar (Inicio)", false, colAccent, none) }),
			layout.Rigid(func(gtx C) D { return a.toolIcon(gtx, &a.btnRec, icRec, recTip, a.eng.Recording(), colRec, colRec) }),
			layout.Rigid(layout.Spacer{Width: 10}.Layout),
			layout.Rigid(func(gtx C) D {
				return a.toolIcon(gtx, &a.btnMetro, icMetro, "Metrónomo", a.cfg.Metronome, colAccent, none)
			}),
			layout.Rigid(func(gtx C) D {
				return a.toolIcon(gtx, &a.btnFollow, icFollow, "Seguir cursor", a.tab.Follow, colAccent, none)
			}),
			layout.Rigid(layout.Spacer{Width: 10}.Layout),
			layout.Rigid(a.recBadge),
			layout.Flexed(1, func(gtx C) D {
				return layout.E.Layout(gtx, func(gtx C) D { return a.label(gtx, 15, colText, a.positionText()) })
			}),
		)
	})
}

// recBadge shows whether the input track is armed or recording.
func (a *App) recBadge(gtx C) D {
	var txt string
	var bg color.NRGBA
	switch {
	case a.eng.Recording():
		txt, bg = "GRABANDO  "+clock(a.eng.RecordingElapsed()), colRec
		if !a.eng.Playing() {
			txt = "GRABACIÓN LISTA"
		}
	case a.recArm:
		txt, bg = "PISTA ARMADA", rgb(0x6b2a27)
	default:
		return D{}
	}
	m := op.Record(gtx.Ops)
	d := layout.Inset{Top: 5, Bottom: 5, Left: 26, Right: 12}.Layout(gtx, func(gtx C) D {
		l := material.Label(a.th, 13, txt)
		l.Color = colWhite
		l.Font.Weight = font.Bold
		return l.Layout(gtx)
	})
	call := m.Stop()
	paint.FillShape(gtx.Ops, bg, clip.UniformRRect(image.Rectangle{Max: d.Size}, d.Size.Y/2).Op(gtx.Ops))
	// Blinking dot while recording.
	if !a.eng.Recording() || gtx.Now.UnixMilli()/500%2 == 0 {
		r := float32(gtx.Dp(5))
		dot(gtx, float32(gtx.Dp(14)), float32(d.Size.Y)/2, r, colWhite)
	}
	call.Add(gtx.Ops)
	return d
}

func (a *App) positionText() string {
	if a.tl == nil || len(a.tl.Bars) == 0 {
		return ""
	}
	pos := a.eng.Position()
	g := a.gpTime(pos)
	t := a.tl.SecToTick(max(g, 0))
	bpm := a.tl.TempoAt(int64(t))
	if a.bk.loaded != nil {
		bpm /= a.bk.proj.Scale
	}
	bar := fmt.Sprintf("Compás %d/%d", a.tl.Bars[a.tl.BarAt(t)].Master+1, len(a.song.MasterBars))
	if g < 0 {
		bar = "Intro " + clock(-g)
	}
	return fmt.Sprintf("%s   %s / %s   %.1f bpm", bar, clock(pos), clock(a.sessionDuration()), bpm)
}

func (a *App) deviceBar(gtx C) D {
	paint.FillShape(gtx.Ops, colPanel, clip.Rect{Max: image.Pt(gtx.Constraints.Max.X, gtx.Dp(44))}.Op())
	return layout.Inset{Top: 6, Bottom: 6, Left: 10, Right: 10}.Layout(gtx, func(gtx C) D {
		drv := a.eng.DriverName()
		if drv == "" {
			drv = "(ninguno)"
		}
		in := "—"
		if i := a.eng.Input(); i < len(a.inNames) {
			in = a.inNames[i]
		}
		out := "—"
		if l, r := a.eng.Outputs(); r < len(a.outNames) {
			out = fmt.Sprintf("%d/%d", l+1, r+1)
		}
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D { return a.button(gtx, &a.btnDriver, "Driver: "+drv, false, colAccent) }),
			layout.Rigid(func(gtx C) D { return a.button(gtx, &a.btnPanel, "Panel ASIO", a.panelOpen, colAccent) }),
			layout.Rigid(func(gtx C) D { return a.button(gtx, &a.btnIn, "Entrada: "+in, false, colAccent) }),
			layout.Rigid(func(gtx C) D { return a.meter(gtx, a.inLevel) }),
			layout.Rigid(layout.Spacer{Width: 10}.Layout),
			layout.Rigid(func(gtx C) D { return a.button(gtx, &a.btnOut, "Salida: "+out, false, colAccent) }),
			layout.Rigid(func(gtx C) D { return a.meter(gtx, a.outLevel) }),
			layout.Rigid(layout.Spacer{Width: 14}.Layout),
			layout.Rigid(func(gtx C) D {
				return a.label(gtx, 12, colDim, fmt.Sprintf("Ajuste latencia %+.0f ms", a.cfg.LatencyAdjustMs))
			}),
			layout.Rigid(layout.Spacer{Width: 6}.Layout),
			layout.Rigid(func(gtx C) D { return a.button(gtx, &a.btnLatMinus, "−", false, colAccent) }),
			layout.Rigid(func(gtx C) D { return a.button(gtx, &a.btnLatPlus, "+", false, colAccent) }),
			layout.Flexed(1, func(gtx C) D {
				return layout.E.Layout(gtx, func(gtx C) D { return a.label(gtx, 12, colDim, a.eng.Status()) })
			}),
		)
	})
}

func (a *App) meter(gtx C, level float32) D { return a.meterW(gtx, level, gtx.Dp(90)) }

func (a *App) body(gtx C) D {
	return layout.Flex{}.Layout(gtx,
		layout.Rigid(a.trackPanel),
		layout.Flexed(1, func(gtx C) D {
			a.tab.Recording = a.eng.Recording() && a.eng.Playing()
			d := a.tab.Layout(gtx, a.gpTime(a.eng.Position()), a.eng.Playing())
			if a.tab.Clicked >= 0 {
				a.seekMaster(a.tab.Clicked)
			}
			return d
		}),
	)
}

func (a *App) trackPanel(gtx C) D {
	w := gtx.Dp(270)
	gtx.Constraints = layout.Exact(image.Pt(w, gtx.Constraints.Max.Y))
	paint.FillShape(gtx.Ops, colPanel, clip.Rect{Max: gtx.Constraints.Max}.Op())
	layout.Inset{Top: 8, Left: 8, Right: 8}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(a.inputCard),
			layout.Rigid(layout.Spacer{Height: 8}.Layout),
			layout.Rigid(a.backingCard),
			layout.Rigid(layout.Spacer{Height: 14}.Layout),
			layout.Rigid(func(gtx C) D {
				if a.song == nil {
					return a.label(gtx, 13, colDim, "Sin partitura cargada")
				}
				title := a.song.Title
				if title == "" {
					title = strings.TrimSuffix(filepath.Base(a.path), filepath.Ext(a.path))
				}
				l := material.Label(a.th, 16, title)
				l.Font.Weight = font.Bold
				l.MaxLines = 2
				return layout.Inset{Bottom: 2}.Layout(gtx, l.Layout)
			}),
			layout.Rigid(func(gtx C) D {
				if a.song == nil || a.song.Artist == "" {
					return D{}
				}
				return a.label(gtx, 12, colDim, a.song.Artist)
			}),
			layout.Rigid(layout.Spacer{Height: 10}.Layout),
			layout.Flexed(1, func(gtx C) D {
				return a.trackList.Layout(gtx, a.panelItems(), a.panelItem)
			}),
		)
	})
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

func (a *App) trackRow(gtx C, i int) D {
	t := &a.tracks[i]
	tr := a.song.Tracks[i]
	return layout.Inset{Bottom: 6}.Layout(gtx, func(gtx C) D {
		m := op.Record(gtx.Ops)
		d := layout.UniformInset(6).Layout(gtx, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx C) D {
							return t.sel.Layout(gtx, func(gtx C) D {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx C) D {
										l := material.Label(a.th, 14, tr.Name)
										l.MaxLines = 1
										if i == a.selTrack {
											l.Font.Weight = font.Bold
										}
										return l.Layout(gtx)
									}),
									layout.Rigid(func(gtx C) D { return a.label(gtx, 11, colDim, trackInfo(tr)) }),
								)
							})
						}),
						layout.Rigid(func(gtx C) D { return a.small(gtx, &t.mute, "M", t.muted, colAmber) }),
						layout.Rigid(func(gtx C) D { return a.small(gtx, &t.solo, "S", t.soloed, colGreen) }),
					)
				}),
				layout.Rigid(func(gtx C) D {
					s := material.Slider(a.th, &t.vol)
					return s.Layout(gtx)
				}),
			)
		})
		call := m.Stop()
		bg := colBg
		if i == a.selTrack {
			bg = colSel
		}
		paint.FillShape(gtx.Ops, bg, clip.UniformRRect(image.Rectangle{Max: d.Size}, gtx.Dp(4)).Op(gtx.Ops))
		call.Add(gtx.Ops)
		return d
	})
}

func (a *App) small(gtx C, c *widget.Clickable, label string, on bool, onCol color.NRGBA) D {
	b := material.Button(a.th, c, label)
	b.TextSize = 12
	b.CornerRadius = 3
	b.Inset = layout.Inset{Top: 4, Bottom: 4, Left: 8, Right: 8}
	b.Background, b.Color = colBtn, colText
	if on {
		b.Background, b.Color = onCol, colWhite
	}
	return layout.Inset{Left: 4}.Layout(gtx, b.Layout)
}

func trackInfo(t *gp.Track) string {
	if t.Percussion {
		return "Batería"
	}
	st := t.Staves[0]
	names := make([]string, len(st.Tuning))
	for i, p := range st.Tuning {
		names[i] = noteName(p)
	}
	s := fmt.Sprintf("%d cuerdas · %s", len(st.Tuning), strings.Join(names, " "))
	if st.Capo > 0 {
		s += fmt.Sprintf(" · cejilla %d", st.Capo)
	}
	return s
}

func (a *App) statusBar(gtx C) D {
	paint.FillShape(gtx.Ops, colPanel, clip.Rect{Max: image.Pt(gtx.Constraints.Max.X, gtx.Dp(28))}.Op())
	return layout.Inset{Top: 6, Bottom: 6, Left: 10, Right: 10}.Layout(gtx, func(gtx C) D {
		col := colDim
		if a.statusErr {
			col = rgb(0xff7b72)
		}
		return layout.Flex{}.Layout(gtx,
			layout.Flexed(1, func(gtx C) D { return a.label(gtx, 12, col, a.status) }),
			layout.Rigid(func(gtx C) D {
				l := material.Label(a.th, 12, "Espacio: reproducir/pausa · R: armar grabación · Inicio: parar · clic en un compás para saltar")
				l.Color = colDim
				l.Alignment = text.End
				l.MaxLines = 1
				return l.Layout(gtx)
			}),
		)
	})
}

func clock(sec float64) string {
	s := int(max(sec, 0))
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func pt(x, y float32) f32.Point { return f32.Pt(x, y) }

// inputCard is the record track: the audio input you play into.
func (a *App) inputCard(gtx C) D {
	recording := a.eng.Recording()
	m := op.Record(gtx.Ops)
	d := layout.UniformInset(8).Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D {
						l := material.Label(a.th, 14, "Grabación")
						l.Font.Weight = font.Bold
						return l.Layout(gtx)
					}),
					layout.Rigid(func(gtx C) D {
						return a.smallIcon(gtx, &a.btnArm, icRec, "Armar grabación (R)", a.recArm, colRec, colRec)
					}),
					layout.Rigid(func(gtx C) D {
						tip, tint := "Monitor activado: te oyes al armar REC (clic para desactivar)", colAmber
						if !a.cfg.Monitor {
							tip, tint = "Monitor desactivado. Actívalo solo con auriculares (clic para oírte al armar REC)", colDim
						}
						return a.smallIcon(gtx, &a.btnMon, icMonitor, tip, a.monitoring(), colAmber, tint)
					}),
				)
			}),
			layout.Rigid(layout.Spacer{Height: 4}.Layout),
			layout.Rigid(func(gtx C) D {
				return a.btnIn2.Layout(gtx, func(gtx C) D {
					return a.label(gtx, 11, colDim, "Entrada: "+a.inputName()+"  (clic para cambiar)")
				})
			}),
			layout.Rigid(layout.Spacer{Height: 6}.Layout),
			layout.Rigid(func(gtx C) D { return a.meterW(gtx, a.inLevel, gtx.Constraints.Max.X) }),
			layout.Rigid(layout.Spacer{Height: 4}.Layout),
			layout.Rigid(func(gtx C) D {
				level := 20 * math.Log10(float64(max(a.inLevel, 1e-6)))
				txt := fmt.Sprintf("Ganancia %+.0f dB   ·   nivel %.0f dB", a.cfg.InputGainDb, level)
				if level < -60 {
					txt = fmt.Sprintf("Ganancia %+.0f dB   ·   sin señal", a.cfg.InputGainDb)
				}
				return a.label(gtx, 11, colDim, txt)
			}),
			layout.Rigid(func(gtx C) D { return material.Slider(a.th, &a.gain).Layout(gtx) }),
			layout.Rigid(layout.Spacer{Height: 6}.Layout),
			layout.Rigid(func(gtx C) D {
				if !a.inputConnected() {
					l := material.Label(a.th, 11, inputHelp)
					l.Color = rgb(0xff8a80)
					return l.Layout(gtx)
				}
				if !a.hasTake {
					return a.label(gtx, 11, colDim, "Arma REC y dale a Reproducir para grabar")
				}
				return layout.W.Layout(gtx, func(gtx C) D {
					return layout.Inset{Left: -4}.Layout(gtx, func(gtx C) D {
						return a.smallIcon(gtx, &a.btnTake, icReplay, "Oír última toma", a.takeOn, colAccent, color.NRGBA{})
					})
				})
			}),
			layout.Rigid(layout.Spacer{Height: 6}.Layout),
			layout.Rigid(func(gtx C) D {
				if a.song == nil {
					return D{}
				}
				return layout.W.Layout(gtx, func(gtx C) D {
					return layout.Inset{Left: -4}.Layout(gtx, func(gtx C) D {
						return a.smallIcon(gtx, &a.btnFolder, icFolder, "Abrir carpeta de tomas", false, colAccent, color.NRGBA{})
					})
				})
			}),
		)
	})
	call := m.Stop()
	bg := colBg
	switch {
	case recording:
		bg = rgb(0x5a1f1c)
	case a.recArm:
		bg = rgb(0x3d2524)
	}
	rr := clip.UniformRRect(image.Rectangle{Max: d.Size}, gtx.Dp(4))
	paint.FillShape(gtx.Ops, bg, rr.Op(gtx.Ops))
	if a.recArm {
		paint.FillShape(gtx.Ops, colRec, clip.Stroke{Path: rr.Path(gtx.Ops), Width: float32(gtx.Dp(1.5))}.Op())
	}
	call.Add(gtx.Ops)
	return d
}

func (a *App) meterW(gtx C, level float32, w int) D {
	h := gtx.Dp(8)
	paint.FillShape(gtx.Ops, rgb(0x15171a), clip.Rect{Max: image.Pt(w, h)}.Op())
	// dB scale from -48 to 0.
	db := 20 * math.Log10(float64(max(level, 1e-6)))
	f := min(max((db+48)/48, 0), 1)
	col := colGreen
	if level > 0.5 {
		col = colAmber
	}
	if level > 0.95 {
		col = colRec
	}
	paint.FillShape(gtx.Ops, col, clip.Rect{Max: image.Pt(int(f*float64(w)), h)}.Op())
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// inputConnected reports whether the driver has a usable input. ASIO4ALL
// names its channels "Not Connected" when no input device is enabled or
// another program holds it.
func (a *App) inputConnected() bool {
	return len(a.inNames) > 0 && !strings.Contains(strings.ToLower(a.inputName()), "not connected")
}

const inputHelp = "La entrada no está activa en ASIO4ALL. Pulsa «Panel ASIO» y activa el dispositivo y su entrada (vista avanzada, icono de llave inglesa). Si otra app (Reaper u otra ventana de esta misma) está usando el audio, ciérrala."

// maxGainDb is the top of the input gain slider.
const maxGainDb = 40

// takesDir is where takes for the current song are saved: a folder next to
// the .gp file named after it.
func (a *App) takesDir() string {
	return strings.TrimSuffix(a.path, filepath.Ext(a.path)) + " - tomas"
}

// openTakesFolder shows the takes folder in Explorer, selecting the last
// take when there is one.
func (a *App) openTakesFolder() {
	if a.song == nil {
		return
	}
	dir := a.takesDir()
	if _, err := os.Stat(dir); err != nil {
		a.setStatus(false, "Aún no hay tomas de esta canción. Se guardarán en %s", dir)
		return
	}
	target := dir
	if _, err := os.Stat(a.lastTake); a.lastTake != "" && err == nil {
		target = a.lastTake
	}
	if err := showInExplorer(target); err != nil {
		a.setStatus(true, "No se pudo abrir el Explorador: %v", err)
		return
	}
	a.setStatus(false, "Tomas en %s", dir)
}

// monitoring reports whether the input is being sent to the output: like
// Reaper's auto monitoring, you hear yourself while the track is armed.
func (a *App) monitoring() bool { return a.recArm && a.cfg.Monitor }

func (a *App) applyMonitor() { a.eng.SetMonitor(a.monitoring()) }

// manageBackgroundAudio releases the audio device while the window is in
// the background and idle, so other programs can use the sound card (ASIO
// drivers such as ASIO4ALL take it exclusively), and reopens it on return.
func (a *App) manageBackgroundAudio() {
	if a.cfg.KeepAudio || a.panelOpen {
		return
	}
	idle := !a.eng.Playing() && !a.eng.Recording() && a.eng.PreviewPosition() < 0 && !a.recArm
	switch {
	case !a.focused && idle && !a.suspended && a.eng.Running():
		a.eng.CloseDriver()
		a.suspended = true
		a.setStatus(false, "Audio liberado para otros programas mientras esta ventana está en segundo plano")
	case a.focused && a.suspended:
		a.suspended = false
		prev, prevErr := a.status, a.statusErr
		a.openDriver(a.cfg.Driver)
		if a.eng.Running() {
			a.status, a.statusErr = prev, prevErr
			if strings.HasPrefix(prev, "Audio liberado") {
				a.setStatus(false, "Audio recuperado")
			}
		}
		a.refreshTakes()
	}
}
