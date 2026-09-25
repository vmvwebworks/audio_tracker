package ui

import (
	"encoding/json"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gioui.org/font"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/vmvwebworks/audio_tracker/internal/engine"
)

// The backing track is the real song (e.g. a mix exported from Reaper). When
// it is loaded, playback follows the audio file's clock ("session time") and
// the score is placed on it: score time g plays at session time
// Offset + g*Scale. Offset skips the intro, Scale absorbs a small tempo
// difference between the recording and the Guitar Pro file.

// project holds the per-song settings, saved as proyecto.json in the takes
// folder.
type project struct {
	Backing string  `json:"tema"`
	Offset  float64 `json:"inicio_compas_1"`
	Scale   float64 `json:"escala_tiempo"`
	GainDb  float64 `json:"volumen_db"`
	Muted   bool    `json:"silenciado"`
}

const projectFile = "proyecto.json"

type backingUI struct {
	proj    project
	loaded  *engine.Backing
	sr      float64 // sample rate the loaded audio was decoded at
	loading bool

	gain, lastGain                                     float32
	vol                                                widget.Float
	btnLoad, btnMute, btnRemove                        widget.Clickable
	btnOffMinus, btnOffPlus, btnOffMark                widget.Clickable
	btnTempoMinus, btnTempoPlus, btnTempoMark, btnSync widget.Clickable

	// Typing a value by hand: editing is "offset", "speed" or "".
	editing                  string
	ed                       widget.Editor
	btnOffEdit, btnSpeedEdit widget.Clickable
	btnEdOK, btnEdCancel     widget.Clickable
}

// gpTime converts session time to score time.
func (a *App) gpTime(s float64) float64 {
	if a.bk.loaded == nil {
		return s
	}
	return (s - a.bk.proj.Offset) / a.bk.proj.Scale
}

// sessionTime converts score time to session time.
func (a *App) sessionTime(g float64) float64 {
	if a.bk.loaded == nil {
		return g
	}
	return a.bk.proj.Offset + g*a.bk.proj.Scale
}

// sessionDuration is the length of the playback timeline.
func (a *App) sessionDuration() float64 {
	d := 0.0
	if a.tl != nil {
		d = a.sessionTime(a.tl.Duration())
	}
	if a.bk.loaded != nil {
		d = max(d, a.bk.loaded.Duration)
	}
	return d
}

// barAt returns the master bar (0-based) sounding at session time s, or -1
// during the intro before bar 1.
func (a *App) barAt(s float64) int {
	if a.tl == nil || len(a.tl.Bars) == 0 {
		return 0
	}
	g := a.gpTime(s)
	if g < 0 {
		return -1
	}
	return a.tl.Bars[a.tl.BarAt(a.tl.SecToTick(g))].Master
}

func (a *App) loadProject() {
	a.bk.proj = project{Scale: 1}
	if b, err := os.ReadFile(filepath.Join(a.takesDir(), projectFile)); err == nil {
		_ = json.Unmarshal(b, &a.bk.proj)
	}
	if a.bk.proj.Scale <= 0 {
		a.bk.proj.Scale = 1
	}
	a.bk.vol.Value = gainToSlider(a.bk.proj.GainDb)
	a.bk.lastGain = a.bk.vol.Value
}

func (a *App) saveProject() {
	if a.song == nil {
		return
	}
	dir := a.takesDir()
	if a.bk.proj.Backing == "" {
		if _, err := os.Stat(filepath.Join(dir, projectFile)); err != nil {
			return // nothing to remember
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	b, _ := json.MarshalIndent(a.bk.proj, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, projectFile), b, 0o644); err != nil {
		a.setStatus(true, "No se pudo guardar la sincronización: %v", err)
	}
}

// Volume slider: 0..1 maps to -30..+6 dB.
func gainToSlider(db float64) float32 { return float32((db + 30) / 36) }
func sliderToGain(v float32) float64  { return math.Round(float64(v)*36 - 30) }

// clearBacking removes the backing track from the engine (not from the
// project).
func (a *App) clearBacking() {
	a.bk.loaded = nil
	a.eng.SetBacking(nil, nil)
	a.eng.SetSync(0, 1)
}

// openBacking decodes the project's backing track in the background.
func (a *App) openBacking() {
	a.clearBacking()
	path := a.bk.proj.Backing
	sr := a.eng.SampleRate()
	if path == "" {
		return
	}
	if sr == 0 {
		a.setStatus(true, "Tema: el audio no está en marcha, no se puede cargar")
		return
	}
	a.bk.loading = true
	song := a.path
	a.setStatus(false, "Cargando el tema %s…", filepath.Base(path))
	go func() {
		b, err := engine.LoadBacking(path, sr)
		a.post(func() {
			a.bk.loading = false
			if a.path != song || a.bk.proj.Backing != path {
				return // the song or the file changed meanwhile
			}
			if err != nil {
				a.setStatus(true, "No se pudo cargar el tema %s: %v", filepath.Base(path), err)
				return
			}
			if a.eng.Playing() {
				res, err := a.eng.Pause()
				a.reportTake(res, err)
			}
			a.bk.loaded, a.bk.sr = b, sr
			a.eng.SetBacking(b.L, b.R)
			a.applyBacking()
			a.refreshTakes()
			msg := fmt.Sprintf("Tema cargado: %s (%s).", filepath.Base(path), clock(b.Duration))
			if a.bk.proj.Offset == 0 && a.bk.proj.Scale == 1 {
				msg += " Para sincronizarlo, dale a Reproducir y pulsa I justo cuando empiece el compás 1."
			}
			a.setStatus(false, "%s", msg)
		})
	}()
}

// applyBacking sends sync and level to the engine.
func (a *App) applyBacking() {
	if a.bk.loaded == nil {
		a.eng.SetSync(0, 1)
		return
	}
	p := a.bk.proj
	a.eng.SetSync(p.Offset, p.Scale)
	a.eng.SetBackingLevel(float32(math.Pow(10, p.GainDb/20)), !p.Muted)
}

// syncChanged keeps the heard position when the mapping changes while
// stopped, and persists the new mapping.
func (a *App) syncChanged() {
	a.applyBacking()
	a.saveProject()
}

// markBar1 sets bar 1 to start at the moment being heard.
func (a *App) markBar1() {
	if a.bk.loaded == nil {
		return
	}
	s := a.eng.Position()
	a.bk.proj.Offset = s
	a.syncChanged()
	a.setStatus(false, "Compás 1 marcado en %s. Afina con − / + si hace falta.", preciseClock(s))
}

// markTempo sets the tempo scale so that the bar downbeat nearest to what is
// heard now lands exactly now.
func (a *App) markTempo() {
	if a.bk.loaded == nil || a.tl == nil {
		return
	}
	s := a.eng.Position()
	g := a.gpTime(s)
	if g < 4 {
		a.setStatus(true, "Para ajustar el tempo, marca un compás más adelante en la canción (cuanto más lejos del compás 1, más preciso).")
		return
	}
	i := sort.Search(len(a.tl.Bars), func(i int) bool { return a.tl.Bars[i].Time > g })
	best := max(i-1, 0)
	if i < len(a.tl.Bars) && a.tl.Bars[i].Time-g < g-a.tl.Bars[best].Time {
		best = i
	}
	pb := a.tl.Bars[best]
	if pb.Time <= 0 {
		return
	}
	scale := (s - a.bk.proj.Offset) / pb.Time
	if scale < 0.8 || scale > 1.25 {
		a.setStatus(true, "El ajuste saldría de más de un 20%%: revisa primero el compás 1.")
		return
	}
	a.bk.proj.Scale = scale
	a.syncChanged()
	a.setStatus(false, "Compás %d marcado en %s: el tema va al %.2f%% de la velocidad del Guitar Pro.",
		pb.Master+1, preciseClock(s), 100/scale)
}

// handleBacking processes the backing card's widgets.
func (a *App) handleBacking(gtx C) {
	bk := &a.bk
	a.handleSyncEdit(gtx)
	if bk.btnLoad.Clicked(gtx) && !a.dialogOpen && a.song != nil && !a.eng.Recording() {
		a.dialogOpen = true
		initial := bk.proj.Backing
		if initial == "" {
			initial = a.path
		}
		go func() {
			p := openAudioDialog(initial)
			a.post(func() {
				a.dialogOpen = false
				if p == "" {
					return
				}
				bk.proj.Backing = p
				a.saveProject()
				a.openBacking()
			})
		}()
	}
	if bk.btnRemove.Clicked(gtx) && !a.eng.Recording() {
		if a.eng.Playing() {
			res, err := a.eng.Pause()
			a.reportTake(res, err)
		}
		bk.proj.Backing = ""
		a.clearBacking()
		a.saveProject()
		a.refreshTakes()
		a.setStatus(false, "Tema quitado (la sincronización se conserva si lo vuelves a cargar)")
	}
	if bk.loaded == nil {
		return
	}
	if bk.btnMute.Clicked(gtx) {
		bk.proj.Muted = !bk.proj.Muted
		a.syncChanged()
	}
	if bk.vol.Value != bk.lastGain {
		bk.lastGain = bk.vol.Value
		bk.proj.GainDb = sliderToGain(bk.vol.Value)
		a.applyBacking()
		if !bk.vol.Dragging() {
			a.saveProject()
		}
	}
	nudge := func(d float64) {
		bk.proj.Offset = math.Round((bk.proj.Offset+d)*1000) / 1000
		a.syncChanged()
	}
	if bk.btnOffMinus.Clicked(gtx) {
		nudge(-0.01)
	}
	if bk.btnOffPlus.Clicked(gtx) {
		nudge(0.01)
	}
	if bk.btnOffMark.Clicked(gtx) {
		a.markBar1()
	}
	speed := func(d float64) {
		pct := math.Round((100/bk.proj.Scale+d)*100) / 100
		bk.proj.Scale = 100 / pct
		a.syncChanged()
	}
	if bk.btnTempoMinus.Clicked(gtx) {
		speed(-0.05)
	}
	if bk.btnTempoPlus.Clicked(gtx) {
		speed(0.05)
	}
	if bk.btnTempoMark.Clicked(gtx) {
		a.markTempo()
	}
	if bk.btnSync.Clicked(gtx) {
		bk.proj.Offset, bk.proj.Scale = 0, 1
		a.syncChanged()
		a.setStatus(false, "Sincronización reiniciada")
	}
}

// backingCard shows the backing track and its sync controls.
func (a *App) backingCard(gtx C) D {
	if a.song == nil {
		return D{}
	}
	bk := &a.bk
	m := op.Record(gtx.Ops)
	d := layout.UniformInset(8).Layout(gtx, func(gtx C) D {
		rows := []layout.FlexChild{
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D {
						l := material.Label(a.th, 14, "Tema (audio)")
						l.Font.Weight = font.Bold
						return l.Layout(gtx)
					}),
					layout.Rigid(func(gtx C) D {
						if bk.loaded == nil {
							return D{}
						}
						tip := "Silenciar el tema"
						if bk.proj.Muted {
							tip = "Activar el tema"
						}
						return a.smallIcon(gtx, &bk.btnMute, icVolume, tip, bk.proj.Muted, colAmber, colText)
					}),
					layout.Rigid(func(gtx C) D {
						return a.smallIcon(gtx, &bk.btnLoad, icOpen, "Cargar el tema exportado de Reaper (WAV o MP3)", false, colAccent, colText)
					}),
					layout.Rigid(func(gtx C) D {
						if bk.proj.Backing == "" {
							return D{}
						}
						return a.smallIcon(gtx, &bk.btnRemove, icClose, "Quitar el tema", false, colBtn, colDim)
					}),
				)
			}),
			layout.Rigid(layout.Spacer{Height: 4}.Layout),
			layout.Rigid(func(gtx C) D {
				switch {
				case bk.loading:
					return a.label(gtx, 11, colDim, "Cargando…")
				case bk.loaded == nil:
					l := material.Label(a.th, 11, "Exporta el tema desde Reaper (WAV o MP3) y cárgalo para tocar sobre el sonido real.")
					l.Color = colDim
					return l.Layout(gtx)
				}
				return a.label(gtx, 11, colDim, fmt.Sprintf("%s · %s", filepath.Base(bk.proj.Backing), clock(bk.loaded.Duration)))
			}),
		}
		if bk.loaded != nil {
			rows = append(rows,
				layout.Rigid(func(gtx C) D { return material.Slider(a.th, &bk.vol).Layout(gtx) }),
				layout.Rigid(func(gtx C) D {
					return a.syncRow(gtx, "offset", "Compás 1:", preciseClock(bk.proj.Offset), &bk.btnOffEdit,
						&bk.btnOffMinus, &bk.btnOffPlus, &bk.btnOffMark,
						"−10 ms", "+10 ms", "Marcar ahora (tecla I): pulsa justo cuando empiece el compás 1")
				}),
				layout.Rigid(layout.Spacer{Height: 4}.Layout),
				layout.Rigid(func(gtx C) D {
					return a.syncRow(gtx, "speed", "Velocidad:", fmt.Sprintf("%.2f %%", 100/bk.proj.Scale), &bk.btnSpeedEdit,
						&bk.btnTempoMinus, &bk.btnTempoPlus, &bk.btnTempoMark,
						"−0,05 %", "+0,05 %", "Marcar ahora (tecla T): pulsa en el primer tiempo de un compás avanzado")
				}),
				layout.Rigid(layout.Spacer{Height: 2}.Layout),
				layout.Rigid(func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx C) D {
							l := material.Label(a.th, 10, "Con el tema sonando: I en el compás 1, T en un compás lejano.")
							l.Color = colDim
							return l.Layout(gtx)
						}),
						layout.Rigid(func(gtx C) D {
							return a.smallIcon(gtx, &bk.btnSync, icReplay, "Reiniciar la sincronización", false, colBtn, colDim)
						}),
					)
				}),
			)
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
	})
	call := m.Stop()
	paint.FillShape(gtx.Ops, colBg, clip.UniformRRect(image.Rectangle{Max: d.Size}, gtx.Dp(4)).Op(gtx.Ops))
	call.Add(gtx.Ops)
	return d
}

// preciseClock formats seconds as m:ss.mmm.
func preciseClock(sec float64) string {
	sign := ""
	if sec < 0 {
		sign, sec = "-", -sec
	}
	ms := int(math.Round(sec * 1000))
	return fmt.Sprintf("%s%d:%02d.%03d", sign, ms/60000, ms/1000%60, ms%1000)
}

// syncRow is a sync setting: a label, its value (click to type it), nudge
// buttons and a "mark now" button. While being edited it shows an editor.
func (a *App) syncRow(gtx C, id, name, value string, edit, minus, plus, mark *widget.Clickable, tipMinus, tipPlus, tipMark string) D {
	bk := &a.bk
	if bk.editing == id {
		hint := "p. ej. 0:07.350 o 7,35"
		if id == "speed" {
			hint = "p. ej. 98,5"
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return a.label(gtx, 12, colText, name+" ") }),
					layout.Flexed(1, func(gtx C) D { return a.inputBox(gtx, &bk.ed, hint) }),
					layout.Rigid(func(gtx C) D {
						return a.smallIcon(gtx, &bk.btnEdOK, icCheck, "Guardar (Enter)", false, colGreen, colGreen)
					}),
					layout.Rigid(func(gtx C) D {
						return a.smallIcon(gtx, &bk.btnEdCancel, icClose, "Cancelar (Esc)", false, colBtn, colDim)
					}),
				)
			}),
			layout.Rigid(func(gtx C) D { return a.label(gtx, 10, colDim, "Escribe el valor y pulsa Enter · Esc cancela") }),
		)
	}
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return a.label(gtx, 12, colText, name+" ") }),
		layout.Flexed(1, func(gtx C) D {
			d := edit.Layout(gtx, func(gtx C) D {
				return layout.W.Layout(gtx, func(gtx C) D {
					return a.valueBox(gtx, value)
				})
			})
			if edit.Hovered() {
				a.tooltip(gtx, d, "Clic para escribir el valor a mano")
			}
			return d
		}),
		layout.Rigid(func(gtx C) D { return a.smallIcon(gtx, minus, icRemove, tipMinus, false, colBtn, colText) }),
		layout.Rigid(func(gtx C) D { return a.smallIcon(gtx, plus, icAdd, tipPlus, false, colBtn, colText) }),
		layout.Rigid(func(gtx C) D { return a.smallIcon(gtx, mark, icFlag, tipMark, false, colAccent, colAmber) }),
	)
}

// valueBox draws a value that looks editable.
func (a *App) valueBox(gtx C, value string) D {
	m := op.Record(gtx.Ops)
	d := layout.Inset{Top: 3, Bottom: 3, Left: 5, Right: 5}.Layout(gtx, func(gtx C) D {
		return a.label(gtx, 12, colText, value)
	})
	call := m.Stop()
	rr := clip.UniformRRect(image.Rectangle{Max: d.Size}, gtx.Dp(3))
	paint.FillShape(gtx.Ops, colBtn, rr.Op(gtx.Ops))
	call.Add(gtx.Ops)
	return d
}

func (a *App) inputBox(gtx C, ed *widget.Editor, hint string) D {
	m := op.Record(gtx.Ops)
	d := layout.Inset{Top: 3, Bottom: 3, Left: 5, Right: 5}.Layout(gtx, func(gtx C) D {
		e := material.Editor(a.th, ed, hint)
		e.TextSize = 12
		e.Color = colText
		e.HintColor = colDim
		return e.Layout(gtx)
	})
	call := m.Stop()
	rr := clip.UniformRRect(image.Rectangle{Max: d.Size}, gtx.Dp(3))
	paint.FillShape(gtx.Ops, colSel, rr.Op(gtx.Ops))
	paint.FillShape(gtx.Ops, colAccent, clip.Stroke{Path: rr.Path(gtx.Ops), Width: float32(gtx.Dp(1))}.Op())
	call.Add(gtx.Ops)
	return d
}

// startEdit opens the editor for a sync value.
func (a *App) startEdit(gtx C, id, value string) {
	bk := &a.bk
	bk.editing = id
	bk.ed.SingleLine, bk.ed.Submit = true, true
	bk.ed.SetText(value)
	bk.ed.SetCaret(len([]rune(value)), 0)
	gtx.Execute(key.FocusCmd{Tag: &bk.ed})
}

// handleSyncEdit processes the value editor.
func (a *App) handleSyncEdit(gtx C) {
	bk := &a.bk
	if bk.btnOffEdit.Clicked(gtx) {
		a.startEdit(gtx, "offset", preciseClock(bk.proj.Offset))
	}
	if bk.btnSpeedEdit.Clicked(gtx) {
		a.startEdit(gtx, "speed", strconv.FormatFloat(100/bk.proj.Scale, 'f', 2, 64))
	}
	if bk.editing == "" {
		return
	}
	commit := bk.btnEdOK.Clicked(gtx)
	for {
		ev, ok := bk.ed.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			commit = true
		}
	}
	for {
		ev, ok := gtx.Event(key.Filter{Focus: &bk.ed, Name: key.NameEscape})
		if !ok {
			break
		}
		if e, ok := ev.(key.Event); ok && e.State == key.Press {
			bk.editing = ""
		}
	}
	if bk.btnEdCancel.Clicked(gtx) {
		bk.editing = ""
	}
	if !commit || bk.editing == "" {
		return
	}
	text := bk.ed.Text()
	switch bk.editing {
	case "offset":
		v, err := parseTime(text)
		if err != nil {
			a.setStatus(true, "No entiendo «%s»: escribe el tiempo como 0:07.350 o 7,35 (segundos)", text)
			return
		}
		bk.proj.Offset = v
		a.setStatus(false, "Compás 1 en %s", preciseClock(v))
	case "speed":
		v, err := strconv.ParseFloat(strings.TrimSpace(strings.ReplaceAll(strings.TrimSuffix(strings.TrimSpace(text), "%"), ",", ".")), 64)
		if err != nil || v < 80 || v > 125 {
			a.setStatus(true, "La velocidad tiene que ser un porcentaje entre 80 y 125 (p. ej. 98,5)")
			return
		}
		bk.proj.Scale = 100 / v
		a.setStatus(false, "Velocidad del tema: %.2f %%", v)
	}
	bk.editing = ""
	a.syncChanged()
}

// parseTime reads a time as seconds ("7.35", "7,35") or minutes:seconds
// ("0:07.350", "1:02,5"), optionally negative.
func parseTime(s string) (float64, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", ".")
	s = strings.TrimSuffix(strings.TrimSpace(strings.TrimSuffix(s, "s")), " ")
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	if s == "" {
		return 0, fmt.Errorf("vacío")
	}
	var total float64
	parts := strings.Split(s, ":")
	if len(parts) > 3 {
		return 0, fmt.Errorf("formato")
	}
	for i, p := range parts {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil || v < 0 || (i > 0 && v >= 60) || (i < len(parts)-1 && v != math.Trunc(v)) {
			return 0, fmt.Errorf("formato")
		}
		total = total*60 + v
	}
	if neg {
		total = -total
	}
	return total, nil
}
