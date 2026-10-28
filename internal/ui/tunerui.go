package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"sort"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/vmvwebworks/audio_tracker/internal/tuner"
)

const (
	tunerInterval = 70 * time.Millisecond // between pitch estimates
	tunerHold     = 700 * time.Millisecond
	tunerMinConf  = 0.85
	tunerHistory  = 5
	inTuneCents   = 5
	maxTunerA4    = 450
	minTunerA4    = 430
)

// tunerUI is the state of the tuner strip. The audio thread only copies the
// input into a queue (engine.SetTuner); the pitch is estimated here, on the
// UI goroutine, a few times per second.
type tunerUI struct {
	on bool

	window         []float32 // most recent input samples
	scratch        []float32
	lastRun        time.Time
	lastSeen       time.Time
	hist           []float64 // recent frequency estimates, for the median
	freq           float64   // 0 when nothing is being played
	conf           float64
	sel            int   // index of the string chosen as target, -1 for automatic
	trackEdit      []int // session edit of the selected track's tuning
	trackEditTrack int
	needle         float32 // displayed cents, smoothed

	btnClose, btnPlus, btnMinus widget.Clickable
	btnInstPrev, btnInstNext    widget.Clickable
	btnSemiMinus, btnSemiPlus   widget.Clickable
	btnOctMinus, btnOctPlus     widget.Clickable
	btnReset                    widget.Clickable
	strBtns                     [12]widget.Clickable
}

// a4 is the reference pitch from the settings.
func (a *App) a4() float64 {
	if a.cfg.TunerA4 < minTunerA4 || a.cfg.TunerA4 > maxTunerA4 {
		return tuner.A4
	}
	return a.cfg.TunerA4
}

func (a *App) setTuner(on bool) {
	if on == a.tn.on {
		return
	}
	a.tn.on = on
	a.tn.window = a.tn.window[:0]
	a.tn.hist = a.tn.hist[:0]
	a.tn.freq = 0
	a.tn.sel = -1
	a.eng.SetTuner(on)
	if on {
		a.setStatus(false, "Afinador activado: toca una cuerda al aire. Usa la entrada «%s».", a.inputName())
	}
}

// updateTuner drains the input queue and refreshes the pitch estimate.
func (a *App) updateTuner(now time.Time) {
	t := &a.tn
	if !t.on {
		return
	}
	sr := a.eng.SampleRate()
	if sr <= 0 {
		t.freq = 0
		return
	}
	size := tuner.WindowSize(sr)
	if cap(t.scratch) < 4096 {
		t.scratch = make([]float32, 4096)
	}
	for {
		n := a.eng.TunerRead(t.scratch[:4096])
		if n == 0 {
			break
		}
		t.window = append(t.window, t.scratch[:n]...)
	}
	if len(t.window) > size {
		t.window = append(t.window[:0], t.window[len(t.window)-size:]...)
	}
	if len(t.window) == size && now.Sub(t.lastRun) >= tunerInterval {
		t.lastRun = now
		f, conf := tuner.Detect(t.window, sr)
		if f > 0 && conf >= tunerMinConf {
			t.hist = append(t.hist, f)
			if len(t.hist) > tunerHistory {
				t.hist = t.hist[1:]
			}
			t.freq, t.conf, t.lastSeen = median(t.hist), conf, now
		}
	}
	if t.freq > 0 && now.Sub(t.lastSeen) > tunerHold {
		t.freq = 0
		t.hist = t.hist[:0]
	}
}

func median(v []float64) float64 {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	return s[len(s)/2]
}

// tunerReading is what the strip shows for the current pitch.
type tunerReading struct {
	ok    bool
	midi  int     // note the pitch is measured against
	cents float64 // deviation from it
	freq  float64
}

func (a *App) tunerReading() tunerReading {
	t := &a.tn
	if t.freq <= 0 {
		return tunerReading{}
	}
	if notes := a.stringNotes(); t.sel >= 0 && t.sel < len(notes) {
		m := notes[t.sel]
		return tunerReading{ok: true, midi: m, cents: tuner.Cents(t.freq, m, a.a4()), freq: t.freq}
	}
	n := tuner.Nearest(t.freq, a.a4())
	return tunerReading{ok: true, midi: n.Midi, cents: n.Cents, freq: t.freq}
}

// instrument is a tuning preset.
type instrument struct {
	name    string
	strings []int // MIDI keys of the open strings, in the order they are shown
}

// instruments are the presets the tuner offers besides the selected track's
// own tuning (which is the empty name).
var instruments = []instrument{
	{"Guitarra", []int{40, 45, 50, 55, 59, 64}},
	{"Guitarra Drop D", []int{38, 45, 50, 55, 59, 64}},
	{"Guitarra 7 cuerdas", []int{35, 40, 45, 50, 55, 59, 64}},
	{"Bajo 4 cuerdas", []int{28, 33, 38, 43}},
	{"Bajo 5 cuerdas", []int{23, 28, 33, 38, 43}},
	{"Ukelele", []int{67, 60, 64, 69}},
	{"Mandolina / Violín", []int{55, 62, 69, 76}},
	{"Banjo 5 cuerdas", []int{67, 50, 55, 59, 62}},
	{"Cromático", nil},
}

// instrumentLabel is the name shown for the current choice.
func (a *App) instrumentLabel() string {
	if a.cfg.TunerInstrument == "" {
		return "De la pista"
	}
	return a.cfg.TunerInstrument
}

// stepInstrument moves through the list: the track's tuning first, then
// the presets.
func (a *App) stepInstrument(d int) {
	names := []string{""}
	for _, in := range instruments {
		names = append(names, in.name)
	}
	cur := 0
	for i, n := range names {
		if n == a.cfg.TunerInstrument {
			cur = i
		}
	}
	a.cfg.TunerInstrument = names[(cur+d+len(names))%len(names)]
	a.tn.sel = -1
	_ = a.cfg.Save()
}

// presetTuning is the stock tuning of the current choice, before any edit.
func (a *App) presetTuning() []int {
	if name := a.cfg.TunerInstrument; name != "" {
		for _, in := range instruments {
			if in.name == name {
				return in.strings
			}
		}
		return nil
	}
	if a.song == nil || a.selTrack >= len(a.song.Tracks) {
		return nil
	}
	tr := a.song.Tracks[a.selTrack]
	if tr.Percussion || len(tr.Staves) == 0 {
		return nil
	}
	return tr.Staves[0].Tuning
}

// stringNotes are the open strings the tuner shows: the preset or the
// track's tuning, with the user's changes applied.
func (a *App) stringNotes() []int {
	if name := a.cfg.TunerInstrument; name != "" {
		if c := a.cfg.TunerCustom[name]; len(c) > 0 {
			return c
		}
	} else if len(a.tn.trackEdit) > 0 && a.tn.trackEditTrack == a.selTrack {
		return a.tn.trackEdit
	}
	return a.presetTuning()
}

// shiftString moves the selected string by semitones. Changes to a preset
// are remembered; changes to a track's tuning last until you leave it.
func (a *App) shiftString(semis int) {
	notes := append([]int(nil), a.stringNotes()...)
	if a.tn.sel < 0 || a.tn.sel >= len(notes) {
		return
	}
	notes[a.tn.sel] = min(max(notes[a.tn.sel]+semis, 12), 100)
	if name := a.cfg.TunerInstrument; name != "" {
		if a.cfg.TunerCustom == nil {
			a.cfg.TunerCustom = map[string][]int{}
		}
		a.cfg.TunerCustom[name] = notes
		_ = a.cfg.Save()
		return
	}
	a.tn.trackEdit, a.tn.trackEditTrack = notes, a.selTrack
}

// resetTuning drops the user's changes to the current choice.
func (a *App) resetTuning() {
	if name := a.cfg.TunerInstrument; name != "" {
		delete(a.cfg.TunerCustom, name)
		_ = a.cfg.Save()
	}
	a.tn.trackEdit = nil
}

// tuningEdited reports whether the strings differ from the preset.
func (a *App) tuningEdited() bool {
	cur, pre := a.stringNotes(), a.presetTuning()
	if len(cur) != len(pre) {
		return true
	}
	for i := range cur {
		if cur[i] != pre[i] {
			return true
		}
	}
	return false
}

func (a *App) handleTuner(gtx C) {
	t := &a.tn
	if !t.on {
		return
	}
	if t.btnClose.Clicked(gtx) {
		a.setTuner(false)
		return
	}
	if t.btnPlus.Clicked(gtx) && a.cfg.TunerA4 < maxTunerA4 {
		a.cfg.TunerA4 = a.a4() + 1
		_ = a.cfg.Save()
	}
	if t.btnMinus.Clicked(gtx) && a.cfg.TunerA4 > minTunerA4 {
		a.cfg.TunerA4 = a.a4() - 1
		_ = a.cfg.Save()
	}
	for _, c := range []struct {
		b *widget.Clickable
		f func()
	}{
		{&t.btnInstPrev, func() { a.stepInstrument(-1) }},
		{&t.btnInstNext, func() { a.stepInstrument(1) }},
		{&t.btnSemiMinus, func() { a.shiftString(-1) }},
		{&t.btnSemiPlus, func() { a.shiftString(1) }},
		{&t.btnOctMinus, func() { a.shiftString(-12) }},
		{&t.btnOctPlus, func() { a.shiftString(12) }},
		{&t.btnReset, a.resetTuning},
	} {
		if c.b.Clicked(gtx) {
			c.f()
		}
	}
	notes := a.stringNotes()
	for i := range notes {
		if i < len(t.strBtns) && t.strBtns[i].Clicked(gtx) {
			if t.sel == i {
				t.sel = -1
			} else {
				t.sel = i
			}
		}
	}
}

const tunerHeight = 176

func (a *App) tunerBar(gtx C) D {
	if !a.tn.on {
		return D{}
	}
	h := gtx.Dp(tunerHeight)
	w := gtx.Constraints.Max.X
	paint.FillShape(gtx.Ops, colPanel, clip.Rect{Max: image.Pt(w, h)}.Op())
	paint.FillShape(gtx.Ops, colBg, clip.Rect{Min: image.Pt(0, h-gtx.Dp(2)), Max: image.Pt(w, h)}.Op())
	gtx.Constraints = layout.Exact(image.Pt(w, h))
	r := a.tunerReading()
	return layout.Inset{Top: 8, Bottom: 8, Left: 14, Right: 14}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D { return a.tunerNote(gtx, r) }),
			layout.Rigid(layout.Spacer{Width: 18}.Layout),
			layout.Flexed(1, func(gtx C) D { return a.tunerGauge(gtx, r) }),
			layout.Rigid(layout.Spacer{Width: 18}.Layout),
			layout.Rigid(func(gtx C) D { return a.tunerSide(gtx, r) }),
		)
	})
}

// tunerColor is green when in tune, amber when close and red otherwise.
func tunerColor(cents float64) color.NRGBA {
	switch c := math.Abs(cents); {
	case c <= inTuneCents:
		return colGreen
	case c <= 15:
		return colAmber
	}
	return colRec
}

func (a *App) tunerNote(gtx C, r tunerReading) D {
	gtx.Constraints.Min.X = gtx.Dp(150)
	gtx.Constraints.Max.X = gtx.Dp(150)
	name, oct, hz, col := "—", "", "", colDim
	if r.ok {
		name, oct = tuner.Name(r.midi), fmt.Sprint(r.midi/12-1)
		hz = fmt.Sprintf("%.1f Hz", r.freq)
		col = tunerColor(r.cents)
	}
	return layout.Flex{Axis: layout.Vertical, Alignment: layout.Start}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					l := material.Label(a.th, 56, name)
					l.Color, l.Font.Weight, l.MaxLines = col, font.Bold, 1
					return l.Layout(gtx)
				}),
				layout.Rigid(func(gtx C) D { return a.label(gtx, 20, colDim, oct) }),
			)
		}),
		layout.Rigid(func(gtx C) D { return a.label(gtx, 13, colDim, hz) }),
	)
}

func (a *App) tunerGauge(gtx C, r tunerReading) D {
	w := gtx.Constraints.Max.X
	hint, hintCol := "Toca una cuerda al aire", colDim
	switch {
	case !a.eng.Running():
		hint = "El audio no está en marcha: revisa el driver ASIO"
	case !a.inputConnected():
		hint = "No hay entrada activa: elige la entrada en la barra de abajo"
	case r.ok && math.Abs(r.cents) <= inTuneCents:
		hint, hintCol = "Afinado", colGreen
	case r.ok && r.cents < 0:
		hint, hintCol = "Grave: sube la cuerda", tunerColor(r.cents)
	case r.ok:
		hint, hintCol = "Agudo: afloja la cuerda", tunerColor(r.cents)
	}
	// The needle eases towards the reading.
	want := float32(0)
	if r.ok {
		want = float32(min(max(r.cents, -50), 50))
	}
	a.tn.needle += (want - a.tn.needle) * 0.35

	gh := gtx.Dp(34)
	return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			paint.FillShape(gtx.Ops, rgb(0x15171a), clip.Rect{Max: image.Pt(w, gh)}.Op())
			mid := float32(w) / 2
			per := float32(w) / 100 // pixels per cent
			// In-tune zone.
			fillRect(gtx, mid-inTuneCents*per, 0, mid+inTuneCents*per, float32(gh), rgb(0x1f3a28))
			// Ticks every 10 cents, longer at 0 and ±50.
			for c := -50; c <= 50; c += 10 {
				x := mid + float32(c)*per
				top := float32(gh) * 0.55
				if c == 0 || c == -50 || c == 50 {
					top = float32(gh) * 0.25
				}
				fillRect(gtx, x-0.5, top, x+0.5, float32(gh), rgb(0x4a4f59))
			}
			if r.ok {
				x := mid + a.tn.needle*per
				col := tunerColor(r.cents)
				fillRect(gtx, x-float32(gtx.Dp(2)), 0, x+float32(gtx.Dp(2)), float32(gh), col)
			}
			return D{Size: image.Pt(w, gh)}
		}),
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X = w
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return a.label(gtx, 11, colDim, "−50") }),
				layout.Flexed(1, func(gtx C) D {
					return layout.Center.Layout(gtx, func(gtx C) D {
						txt := ""
						if r.ok {
							txt = fmt.Sprintf("%+.0f cents", r.cents)
						}
						return a.label(gtx, 13, colDim, txt)
					})
				}),
				layout.Rigid(func(gtx C) D { return a.label(gtx, 11, colDim, "+50") }),
			)
		}),
		layout.Rigid(layout.Spacer{Height: 6}.Layout),
		layout.Rigid(func(gtx C) D {
			l := material.Label(a.th, 17, hint)
			l.Color, l.Font.Weight, l.MaxLines = hintCol, font.Bold, 1
			return l.Layout(gtx)
		}),
	)
}

// tunerSide holds the string buttons, the reference pitch and the close button.
func (a *App) tunerSide(gtx C, r tunerReading) D {
	t := &a.tn
	notes := a.stringNotes()
	gtx.Constraints.Min.X = gtx.Dp(370)
	gtx.Constraints.Max.X = gtx.Dp(370)
	return layout.Flex{Axis: layout.Vertical, Alignment: layout.Start}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return a.label(gtx, 12, colDim, fmt.Sprintf("La = %.0f Hz", a.a4())) }),
				layout.Rigid(layout.Spacer{Width: 6}.Layout),
				layout.Rigid(func(gtx C) D {
					return a.smallIcon(gtx, &t.btnMinus, icRemove, "Bajar el La de referencia", false, colAccent, color.NRGBA{})
				}),
				layout.Rigid(func(gtx C) D {
					return a.smallIcon(gtx, &t.btnPlus, icAdd, "Subir el La de referencia", false, colAccent, color.NRGBA{})
				}),
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
				layout.Rigid(func(gtx C) D {
					return a.smallIcon(gtx, &t.btnClose, icClose, "Cerrar el afinador", false, colAccent, color.NRGBA{})
				}),
			)
		}),
		layout.Rigid(layout.Spacer{Height: 8}.Layout),
		layout.Rigid(func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return a.label(gtx, 12, colDim, "Instrumento:") }),
				layout.Rigid(func(gtx C) D {
					return a.smallIcon(gtx, &t.btnInstPrev, icPrev, "Instrumento anterior", false, colAccent, color.NRGBA{})
				}),
				layout.Rigid(func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Dp(150)
					return layout.Center.Layout(gtx, func(gtx C) D {
						l := material.Label(a.th, 14, a.instrumentLabel())
						l.Font.Weight, l.MaxLines = font.Bold, 1
						return l.Layout(gtx)
					})
				}),
				layout.Rigid(func(gtx C) D {
					return a.smallIcon(gtx, &t.btnInstNext, icNext, "Instrumento siguiente", false, colAccent, color.NRGBA{})
				}),
			)
		}),
		layout.Rigid(layout.Spacer{Height: 8}.Layout),
		layout.Rigid(func(gtx C) D {
			if len(notes) == 0 {
				return a.label(gtx, 12, colDim, "Modo cromático: se mide contra la nota más cercana")
			}
			var kids []layout.FlexChild
			for i, m := range notes {
				if i >= len(t.strBtns) {
					break
				}
				kids = append(kids, layout.Rigid(func(gtx C) D { return a.stringButton(gtx, &t.strBtns[i], m, i, r) }))
			}
			return layout.Flex{}.Layout(gtx, kids...)
		}),
		layout.Rigid(layout.Spacer{Height: 6}.Layout),
		layout.Rigid(func(gtx C) D {
			if t.sel < 0 || t.sel >= len(notes) {
				txt := "Automático: mide contra la nota más cercana"
				if len(notes) > 0 {
					txt = "Pulsa una cuerda para fijarla o cambiar su nota"
				}
				return a.label(gtx, 11, colDim, txt)
			}
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return a.label(gtx, 12, colText, "Cuerda "+tuner.FullName(notes[t.sel])+"  Nota:")
				}),
				layout.Rigid(func(gtx C) D {
					return a.smallIcon(gtx, &t.btnSemiMinus, icRemove, "Bajar medio tono", false, colAccent, color.NRGBA{})
				}),
				layout.Rigid(func(gtx C) D {
					return a.smallIcon(gtx, &t.btnSemiPlus, icAdd, "Subir medio tono", false, colAccent, color.NRGBA{})
				}),
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
				layout.Rigid(func(gtx C) D { return a.label(gtx, 12, colText, "Octava:") }),
				layout.Rigid(func(gtx C) D {
					return a.smallIcon(gtx, &t.btnOctMinus, icRemove, "Bajar una octava", false, colAccent, color.NRGBA{})
				}),
				layout.Rigid(func(gtx C) D {
					return a.smallIcon(gtx, &t.btnOctPlus, icAdd, "Subir una octava", false, colAccent, color.NRGBA{})
				}),
			)
		}),
		layout.Rigid(func(gtx C) D {
			if !a.tuningEdited() {
				return D{}
			}
			return layout.Inset{Top: 4}.Layout(gtx, func(gtx C) D {
				b := material.Button(a.th, &t.btnReset, "Restaurar")
				b.TextSize = 12
				b.CornerRadius = 4
				b.Inset = layout.Inset{Top: 5, Bottom: 5, Left: 10, Right: 10}
				b.Background, b.Color = colBtn, colText
				return b.Layout(gtx)
			})
		}),
	)
}

// stringButton is the button of one open string. It lights up while the
// pitch heard is that string's note and is outlined while it is the target.
func (a *App) stringButton(gtx C, c *widget.Clickable, midi, idx int, r tunerReading) D {
	heard := r.ok && r.midi == midi
	locked := a.tn.sel == idx
	b := material.Button(a.th, c, tuner.FullName(midi))
	b.TextSize = 13
	b.CornerRadius = 4
	b.Inset = layout.Inset{Top: 7, Bottom: 7, Left: 9, Right: 9}
	b.Background, b.Color = colBtn, colText
	switch {
	case heard:
		b.Background, b.Color = tunerColor(r.cents), colWhite
	case locked:
		b.Background, b.Color = colAccent, colWhite
	}
	return layout.Inset{Left: 4}.Layout(gtx, b.Layout)
}

const (
	minZoom  = 0.5
	maxZoom  = 2.5
	zoomStep = 0.1
)

// zoom is the score magnification from the settings.
func (a *App) zoom() float32 {
	if a.cfg.Zoom < minZoom || a.cfg.Zoom > maxZoom {
		return 1
	}
	return float32(a.cfg.Zoom)
}

// stepZoom changes the score magnification by n steps.
func (a *App) stepZoom(n int) {
	z := math.Round((float64(a.zoom())+float64(n)*zoomStep)*10) / 10
	a.cfg.Zoom = math.Min(math.Max(z, minZoom), maxZoom)
	a.tab.Zoom = a.zoom()
	_ = a.cfg.Save()
}
