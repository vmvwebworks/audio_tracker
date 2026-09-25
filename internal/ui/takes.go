package ui

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
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

// takeItem is a recorded take of the current song, listed in the panel.
type takeItem struct {
	path   string
	title  string
	detail string
	fav    bool

	play, mix, star, edit, del widget.Clickable
	ok, cancel, yes, no        widget.Clickable
}

// takesMeta is stored as tomas.json in the takes folder, so favourites
// travel with the folder.
type takesMeta struct {
	Favoritas []string `json:"favoritas"`
}

const takesMetaFile = "tomas.json"

var defaultTakeName = regexp.MustCompile(`^toma \d{4}-\d{2}-\d{2} \d{2}\.\d{2}\.\d{2}`)

func (a *App) loadMeta() takesMeta {
	var m takesMeta
	if b, err := os.ReadFile(filepath.Join(a.takesDir(), takesMetaFile)); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

func (a *App) saveMeta(m takesMeta) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err == nil {
		err = os.WriteFile(filepath.Join(a.takesDir(), takesMetaFile), b, 0o644)
	}
	if err != nil {
		a.setStatus(true, "No se pudieron guardar las favoritas: %v", err)
	}
}

// refreshTakes lists the WAV files in the song's takes folder, newest first.
func (a *App) refreshTakes() {
	a.takes = a.takes[:0]
	if a.song == nil {
		return
	}
	entries, err := os.ReadDir(a.takesDir())
	if err != nil {
		return
	}
	meta := a.loadMeta()
	type found struct {
		item *takeItem
		mod  int64
	}
	var list []found
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".wav") {
			continue
		}
		path := filepath.Join(a.takesDir(), e.Name())
		ti, err := engine.ReadTakeInfo(path)
		if err != nil {
			continue
		}
		info, _ := e.Info()
		where := fmt.Sprintf("Compás %d · %s", a.barAt(ti.Start())+1, clock(ti.Start()))
		if a.barAt(ti.Start()) < 0 {
			where = "Intro · " + clock(ti.Start())
		}
		it := &takeItem{
			path:   path,
			title:  where,
			detail: fmt.Sprintf("dura %s · %s", clock(ti.Duration()), info.ModTime().Format("02/01 15:04")),
			fav:    slices.Contains(meta.Favoritas, e.Name()),
		}
		if name := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())); !defaultTakeName.MatchString(name) {
			// Renamed by the user: show the name, keep the position below.
			it.title = name
			it.detail = strings.ToLower(where[:1]) + where[1:] + " · dura " + clock(ti.Duration())
		}
		list = append(list, found{it, info.ModTime().UnixNano()})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].mod > list[j].mod })
	for _, f := range list {
		a.takes = append(a.takes, f.item)
	}
}

// visibleTakes applies the favourites filter.
func (a *App) visibleTakes() []*takeItem {
	if !a.onlyFavs {
		return a.takes
	}
	var v []*takeItem
	for _, t := range a.takes {
		if t.fav {
			v = append(v, t)
		}
	}
	return v
}

func (a *App) handleTakes(gtx C) {
	if a.previewing != "" && a.eng.PreviewPosition() < 0 {
		a.previewing = ""
	}
	if a.btnFavFilter.Clicked(gtx) {
		a.onlyFavs = !a.onlyFavs
	}
	if a.renaming != "" {
		for {
			ev, ok := a.renameEd.Update(gtx)
			if !ok {
				break
			}
			if _, ok := ev.(widget.SubmitEvent); ok {
				a.commitRename()
			}
		}
		for {
			ev, ok := gtx.Event(key.Filter{Focus: &a.renameEd, Name: key.NameEscape})
			if !ok {
				break
			}
			if e, ok := ev.(key.Event); ok && e.State == key.Press {
				a.renaming = ""
			}
		}
	}
	for _, t := range a.takes {
		switch {
		case t.play.Clicked(gtx):
			a.previewTake(t)
		case t.mix.Clicked(gtx):
			a.playTakeWithSong(t)
		case t.star.Clicked(gtx):
			a.toggleFavorite(t)
		case t.edit.Clicked(gtx):
			a.confirmDel = ""
			a.renaming = t.path
			a.renameEd.SingleLine = true
			a.renameEd.Submit = true
			name := strings.TrimSuffix(filepath.Base(t.path), filepath.Ext(t.path))
			a.renameEd.SetText(name)
			a.renameEd.SetCaret(len([]rune(name)), 0)
			gtx.Execute(key.FocusCmd{Tag: &a.renameEd})
		case t.ok.Clicked(gtx):
			a.commitRename()
		case t.cancel.Clicked(gtx):
			a.renaming = ""
		case t.del.Clicked(gtx):
			a.renaming = ""
			a.confirmDel = t.path
		case t.yes.Clicked(gtx):
			a.deleteTake(t)
		case t.no.Clicked(gtx):
			a.confirmDel = ""
		}
	}
}

func (a *App) toggleFavorite(t *takeItem) {
	meta := a.loadMeta()
	name := filepath.Base(t.path)
	if i := slices.Index(meta.Favoritas, name); i >= 0 {
		meta.Favoritas = slices.Delete(meta.Favoritas, i, i+1)
	} else {
		meta.Favoritas = append(meta.Favoritas, name)
	}
	a.saveMeta(meta)
	t.fav = !t.fav
}

// commitRename renames the WAV being edited to the editor's text.
func (a *App) commitRename() {
	old := a.renaming
	a.renaming = ""
	name := strings.TrimSpace(strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return '-'
		}
		return r
	}, a.renameEd.Text()))
	name = strings.TrimSuffix(name, ".wav")
	if name == "" || name+".wav" == filepath.Base(old) {
		return
	}
	dst := filepath.Join(filepath.Dir(old), name+".wav")
	if _, err := os.Stat(dst); err == nil {
		a.setStatus(true, "Ya hay una toma llamada «%s»", name)
		return
	}
	if err := os.Rename(old, dst); err != nil {
		a.setStatus(true, "No se pudo renombrar: %v", err)
		return
	}
	meta := a.loadMeta()
	if i := slices.Index(meta.Favoritas, filepath.Base(old)); i >= 0 {
		meta.Favoritas[i] = filepath.Base(dst)
		a.saveMeta(meta)
	}
	for _, p := range []*string{&a.lastTake, &a.previewing, &a.songTake} {
		if *p == old {
			*p = dst
		}
	}
	a.refreshTakes()
	a.setStatus(false, "Toma renombrada a «%s»", name)
}

// deleteTake sends a take to the Recycle Bin.
func (a *App) deleteTake(t *takeItem) {
	a.confirmDel = ""
	if a.previewing == t.path {
		a.eng.StopPreview()
		a.previewing = ""
	}
	if err := moveToRecycleBin(t.path); err != nil {
		a.setStatus(true, "No se pudo eliminar: %v", err)
		return
	}
	if a.songTake == t.path {
		a.eng.SetTake(nil, 0)
		a.eng.SetTakePlayback(false)
		a.hasTake, a.takeOn, a.songTake = false, false, ""
	}
	if a.lastTake == t.path {
		a.lastTake = ""
	}
	meta := a.loadMeta()
	if i := slices.Index(meta.Favoritas, filepath.Base(t.path)); i >= 0 {
		meta.Favoritas = slices.Delete(meta.Favoritas, i, i+1)
		a.saveMeta(meta)
	}
	a.refreshTakes()
	a.setStatus(false, "«%s» enviada a la Papelera", filepath.Base(t.path))
}

// previewTake plays a take on its own, or stops it if it is playing.
func (a *App) previewTake(t *takeItem) {
	if a.previewing == t.path {
		a.eng.StopPreview()
		a.previewing = ""
		return
	}
	if a.eng.Playing() {
		res, err := a.eng.Pause()
		a.reportTake(res, err)
	}
	samples, _, err := a.loadTake(t)
	if err != nil {
		return
	}
	a.eng.PlayPreview(samples)
	a.previewing = t.path
	a.setStatus(false, "Escuchando %s", filepath.Base(t.path))
}

// playTakeWithSong plays the song from the take's bar with the take mixed in
// at its position.
func (a *App) playTakeWithSong(t *takeItem) {
	a.eng.StopPreview()
	a.previewing = ""
	if a.eng.Recording() {
		return
	}
	samples, at, err := a.loadTake(t)
	if err != nil {
		return
	}
	if a.eng.Playing() {
		a.eng.Pause()
	}
	a.eng.SetTake(samples, at)
	a.hasTake, a.takeOn, a.songTake = true, true, t.path
	a.eng.SetTakePlayback(true)
	start := float64(at) / a.eng.SampleRate()
	if a.tl != nil && len(a.tl.Bars) > 0 {
		// Start at the beginning of the bar the take starts in.
		if g := a.gpTime(start); g >= 0 {
			start = a.sessionTime(a.tl.Bars[a.tl.BarAt(a.tl.SecToTick(g))].Time)
		}
	}
	a.markPos = start
	a.eng.Seek(start)
	if err := a.eng.Play(); err != nil {
		a.setStatus(true, "%v", err)
		return
	}
	a.setStatus(false, "Escuchando %s con la canción", filepath.Base(t.path))
}

func (a *App) loadTake(t *takeItem) ([]float32, int64, error) {
	sr := a.eng.SampleRate()
	if sr == 0 {
		err := fmt.Errorf("el audio no está en marcha")
		a.setStatus(true, "No se puede escuchar la toma: %v", err)
		return nil, 0, err
	}
	samples, at, err := engine.LoadTake(t.path, sr)
	if err != nil {
		a.setStatus(true, "No se pudo leer %s: %v", filepath.Base(t.path), err)
		a.refreshTakes()
	}
	return samples, at, err
}

// panelItem lays out the scrolling part of the side panel: tracks, then
// the takes of the song.
func (a *App) panelItem(gtx C, i int) D {
	takes := a.visibleTakes()
	switch {
	case i < len(a.tracks):
		return a.trackRow(gtx, i)
	case i == len(a.tracks):
		return a.takesHeader(gtx, len(takes))
	default:
		return a.takeRow(gtx, takes[i-len(a.tracks)-1])
	}
}

func (a *App) panelItems() int {
	if a.song == nil {
		return 0
	}
	return len(a.tracks) + 1 + len(a.visibleTakes())
}

func (a *App) takesHeader(gtx C, n int) D {
	return layout.Inset{Top: 10, Bottom: 6}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx C) D {
				txt := "Tomas"
				switch {
				case len(a.takes) == 0:
					txt = "Tomas: aún no hay ninguna"
				case a.onlyFavs && n == 0:
					txt = "Tomas: ninguna favorita"
				case a.onlyFavs:
					txt = "Tomas favoritas"
				}
				l := material.Label(a.th, 14, txt)
				l.Font.Weight = font.Bold
				return l.Layout(gtx)
			}),
			layout.Rigid(func(gtx C) D {
				if len(a.takes) == 0 {
					return D{}
				}
				tip := "Mostrar solo las favoritas"
				if a.onlyFavs {
					tip = "Mostrar todas las tomas"
				}
				return a.smallIcon(gtx, &a.btnFavFilter, icStar, tip, a.onlyFavs, colAmber, colDim)
			}),
		)
	})
}

func (a *App) takeRow(gtx C, t *takeItem) D {
	playing := a.previewing == t.path
	return layout.Inset{Bottom: 6}.Layout(gtx, func(gtx C) D {
		m := op.Record(gtx.Ops)
		d := layout.UniformInset(6).Layout(gtx, func(gtx C) D {
			if a.renaming == t.path {
				return a.renameRow(gtx, t)
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx C) D { return a.label(gtx, 13, colText, t.title) }),
						layout.Rigid(func(gtx C) D {
							ic, tip := icStarBorder, "Marcar como favorita"
							if t.fav {
								ic, tip = icStar, "Quitar de favoritas"
							}
							return a.smallIcon(gtx, &t.star, ic, tip, false, colAmber, starColor(t.fav))
						}),
						layout.Rigid(func(gtx C) D {
							ic, tip := icPlay, "Escuchar la toma sola"
							if playing {
								ic, tip = icStop, "Parar"
							}
							return a.smallIcon(gtx, &t.play, ic, tip, playing, colGreen, colText)
						}),
						layout.Rigid(func(gtx C) D {
							return a.smallIcon(gtx, &t.mix, icWithSong, "Escuchar con la canción desde su compás", false, colAccent, colText)
						}),
					)
				}),
				layout.Rigid(func(gtx C) D {
					if a.confirmDel == t.path {
						return a.confirmRow(gtx, t)
					}
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx C) D { return a.label(gtx, 11, colDim, t.detail) }),
						layout.Rigid(func(gtx C) D {
							return a.smallIcon(gtx, &t.edit, icEdit, "Renombrar", false, colAccent, colDim)
						}),
						layout.Rigid(func(gtx C) D {
							return a.smallIcon(gtx, &t.del, icDelete, "Eliminar (a la Papelera)", false, colRec, colDim)
						}),
					)
				}),
			)
		})
		call := m.Stop()
		bg := colBg
		if playing {
			bg = colSel
		}
		paint.FillShape(gtx.Ops, bg, clip.UniformRRect(image.Rectangle{Max: d.Size}, gtx.Dp(4)).Op(gtx.Ops))
		call.Add(gtx.Ops)
		return d
	})
}

func (a *App) renameRow(gtx C, t *takeItem) D {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			m := op.Record(gtx.Ops)
			d := layout.UniformInset(5).Layout(gtx, func(gtx C) D {
				e := material.Editor(a.th, &a.renameEd, "Nombre de la toma")
				e.TextSize = 13
				e.Color = colText
				return e.Layout(gtx)
			})
			call := m.Stop()
			paint.FillShape(gtx.Ops, colBtn, clip.UniformRRect(image.Rectangle{Max: d.Size}, gtx.Dp(3)).Op(gtx.Ops))
			call.Add(gtx.Ops)
			return d
		}),
		layout.Rigid(layout.Spacer{Height: 4}.Layout),
		layout.Rigid(func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D { return a.label(gtx, 11, colDim, "Enter guarda · Esc cancela") }),
				layout.Rigid(func(gtx C) D { return a.smallIcon(gtx, &t.ok, icCheck, "Guardar", false, colGreen, colGreen) }),
				layout.Rigid(func(gtx C) D { return a.smallIcon(gtx, &t.cancel, icClose, "Cancelar", false, colBtn, colDim) }),
			)
		}),
	)
}

func (a *App) confirmRow(gtx C, t *takeItem) D {
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Flexed(1, func(gtx C) D { return a.label(gtx, 12, rgb(0xff8a80), "¿Enviar a la Papelera?") }),
		layout.Rigid(func(gtx C) D { return a.small(gtx, &t.yes, "Eliminar", true, colRec) }),
		layout.Rigid(func(gtx C) D { return a.small(gtx, &t.no, "No", false, colAccent) }),
	)
}

func starColor(fav bool) color.NRGBA {
	if fav {
		return colAmber
	}
	return colDim
}
