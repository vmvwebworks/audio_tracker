package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"sort"

	"gioui.org/font"
	"gioui.org/gesture"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"github.com/vmvwebworks/audio_tracker/internal/gp"
)

var (
	colPaper    = rgb(0xfbfaf6)
	colLine     = rgb(0x9c9a94)
	colInk      = rgb(0x1f1f1f)
	colFaint    = rgb(0x9a968c)
	colSection  = rgb(0xb8452f)
	colVoice2   = rgb(0x2f6aa8)
	colCursor   = color.NRGBA{R: 0xff, G: 0xa5, B: 0x1c, A: 0x40}
	colCursorLn = rgb(0xe8850c)
)

// TabView draws one staff of a track as tablature (or a drum staff) and
// follows the playback cursor.
type TabView struct {
	th *material.Theme

	song  *gp.Song
	tl    *gp.Timeline
	track int

	list   layout.List
	rows   []tabRow
	width  int
	metric unit.Metric
	dirty  bool
	clicks []gesture.Click

	lastRow int
	Follow  bool
	// Recording turns the cursor red.
	Recording bool

	// Clicked is set to the master bar index the user clicked, or -1.
	Clicked int
}

type tabRow struct {
	bars []tabBar
}

type tabBar struct {
	master  int
	x, w    float32
	starts  []int     // unique beat start ticks
	xs      []float32 // x of each start relative to bar x, plus bar content end
	showSig bool
	sigX    float32 // centre of the time signature, relative to x
}

// NewTabView creates an empty view.
func NewTabView(th *material.Theme) *TabView {
	return &TabView{th: th, list: layout.List{Axis: layout.Vertical}, Follow: true, lastRow: -1, Clicked: -1}
}

// SetSong shows a new song.
func (v *TabView) SetSong(s *gp.Song, tl *gp.Timeline, track int) {
	v.song, v.tl, v.track = s, tl, track
	v.dirty = true
	v.list.Position = layout.Position{}
	v.lastRow = -1
}

// SetTrack switches the displayed track.
func (v *TabView) SetTrack(track int) {
	if track != v.track {
		v.track = track
		v.dirty = true
	}
}

func (v *TabView) staff() *gp.Staff {
	if v.song == nil || v.track >= len(v.song.Tracks) {
		return nil
	}
	return v.song.Tracks[v.track].Staves[0]
}

func (v *TabView) percussion() bool {
	return v.song != nil && v.song.Tracks[v.track].Percussion
}

func (v *TabView) strings() int {
	if v.percussion() {
		return 5
	}
	if st := v.staff(); st != nil && len(st.Tuning) > 0 {
		return len(st.Tuning)
	}
	return 6
}

// Geometry in dp.
const (
	dpMargin    = 16
	dpLeft      = 34 // tuning labels
	dpTop       = 40 // bar numbers, sections, tempo, endings
	dpLineGap   = 13
	dpRhythm    = 34
	dpRowBottom = 14
	dpBarPad    = 12
	dpSigW      = 26
	dpRepeat    = 12
)

func (v *TabView) px(dp float32) float32 { return dp * v.metric.PxPerDp }

func (v *TabView) rowHeight() int {
	return int(v.px(dpTop + float32(v.strings()-1)*dpLineGap + v.rhythmGap() + dpRhythm + dpRowBottom))
}

// rhythmGap is the space between the staff and the stems; drum notes can sit
// below the staff.
func (v *TabView) rhythmGap() float32 {
	if v.percussion() {
		return 20
	}
	return 9
}

func beatSpace(ticks int) float32 {
	return 20 * float32(math.Sqrt(float64(ticks)/240))
}

func (v *TabView) relayout(width int) {
	v.rows = v.rows[:0]
	st := v.staff()
	if st == nil {
		return
	}
	avail := float32(width) - v.px(dpMargin*2+dpLeft)
	var row tabRow
	var rowW float32
	flush := func(stretch bool) {
		if len(row.bars) == 0 {
			return
		}
		if stretch && rowW > 0 {
			k := avail / rowW
			for i := range row.bars {
				b := &row.bars[i]
				b.w *= k
				for j := range b.xs {
					b.xs[j] *= k
				}
			}
		}
		x := v.px(dpMargin + dpLeft)
		for i := range row.bars {
			row.bars[i].x = x
			x += row.bars[i].w
		}
		v.rows = append(v.rows, row)
		row, rowW = tabRow{}, 0
	}
	for i, mb := range v.song.MasterBars {
		bar := st.Bars[i]
		set := map[int]bool{}
		for _, voice := range bar.Voices {
			for _, b := range voice {
				if !b.Grace {
					set[b.Start] = true
				}
			}
		}
		starts := make([]int, 0, len(set))
		for s := range set {
			if s < mb.Duration {
				starts = append(starts, s)
			}
		}
		sort.Ints(starts)
		if len(starts) == 0 {
			starts = []int{0}
		}
		showSig := i == 0 || mb.Num != v.song.MasterBars[i-1].Num || mb.Den != v.song.MasterBars[i-1].Den
		x := v.px(dpBarPad)
		if mb.RepeatStart {
			x += v.px(dpRepeat)
		}
		sigX := x + v.px(6)
		if showSig {
			x += v.px(dpSigW)
		}
		xs := make([]float32, 0, len(starts)+1)
		for j, s := range starts {
			xs = append(xs, x)
			next := mb.Duration
			if j+1 < len(starts) {
				next = starts[j+1]
			}
			x += v.px(beatSpace(max(next-s, 60)))
		}
		xs = append(xs, x)
		w := x + v.px(4)
		if mb.RepeatEnd {
			w += v.px(dpRepeat)
		}
		if rowW+w > avail && len(row.bars) > 0 {
			flush(true)
		}
		row.bars = append(row.bars, tabBar{master: i, w: w, starts: starts, xs: xs, showSig: showSig, sigX: sigX})
		rowW += w
	}
	flush(rowW > avail*0.75)
}

func (b *tabBar) beatX(start int) float32 {
	i := sort.SearchInts(b.starts, start)
	if i < len(b.starts) && b.starts[i] == start {
		return b.x + b.xs[i]
	}
	// Unknown start (grace note): interpolate.
	return b.tickX(start, 0)
}

func (b *tabBar) tickX(t int, dur int) float32 {
	i := sort.Search(len(b.starts), func(i int) bool { return b.starts[i] > t }) - 1
	i = max(i, 0)
	next := dur
	if i+1 < len(b.starts) {
		next = b.starts[i+1]
	}
	frac := float32(0)
	if next > b.starts[i] {
		frac = float32(t-b.starts[i]) / float32(next-b.starts[i])
	}
	frac = min(max(frac, 0), 1)
	return b.x + b.xs[i] + (b.xs[i+1]-b.xs[i])*frac
}

func (v *TabView) rowOf(master int) int {
	for r, row := range v.rows {
		if len(row.bars) > 0 && master <= row.bars[len(row.bars)-1].master {
			return r
		}
	}
	return len(v.rows) - 1
}

// Layout draws the view. pos is the playback position in seconds and
// playing whether the cursor should be followed.
func (v *TabView) Layout(gtx layout.Context, pos float64, playing bool) layout.Dimensions {
	paint.FillShape(gtx.Ops, colPaper, clip.Rect{Max: gtx.Constraints.Max}.Op())
	v.Clicked = -1
	if v.song == nil {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(v.th, 18, "Abre un archivo de Guitar Pro 7/8 (.gp) para empezar")
			l.Color = colFaint
			return l.Layout(gtx)
		})
	}
	if v.dirty || gtx.Constraints.Max.X != v.width || gtx.Metric != v.metric {
		v.width = gtx.Constraints.Max.X
		v.metric = gtx.Metric
		v.relayout(v.width)
		v.dirty = false
		v.lastRow = -1
	}
	if len(v.clicks) < len(v.rows) {
		v.clicks = append(v.clicks, make([]gesture.Click, len(v.rows)-len(v.clicks))...)
	}

	// Cursor location.
	curMaster, curTick := -1, 0
	if v.tl != nil && len(v.tl.Bars) > 0 {
		t := v.tl.SecToTick(pos)
		pb := v.tl.Bars[v.tl.BarAt(t)]
		curMaster = pb.Master
		curTick = int(t) - int(pb.Tick)
	}
	if curMaster >= 0 && v.Follow {
		r := v.rowOf(curMaster)
		if r != v.lastRow && playing {
			v.list.Position.First = max(r-1, 0)
			v.list.Position.Offset = 0
		}
		v.lastRow = r
	}

	rh := v.rowHeight()
	return v.list.Layout(gtx, len(v.rows), func(gtx layout.Context, i int) layout.Dimensions {
		size := image.Pt(gtx.Constraints.Max.X, rh)
		cl := &v.clicks[i]
		for {
			ev, ok := cl.Update(gtx.Source)
			if !ok {
				break
			}
			if ev.Kind == gesture.KindClick {
				x := float32(ev.Position.X)
				for _, b := range v.rows[i].bars {
					if x >= b.x && x < b.x+b.w {
						v.Clicked = b.master
					}
				}
			}
		}
		area := clip.Rect{Max: size}.Push(gtx.Ops)
		cl.Add(gtx.Ops)
		v.drawRow(gtx, &v.rows[i], curMaster, curTick)
		area.Pop()
		return layout.Dimensions{Size: size}
	})
}

func (v *TabView) drawRow(gtx layout.Context, row *tabRow, curMaster, curTick int) {
	st := v.staff()
	perc := v.percussion()
	n := v.strings()
	gap := v.px(dpLineGap)
	top := v.px(dpTop)
	bottom := top + float32(n-1)*gap
	x0 := v.px(dpMargin + dpLeft)
	last := row.bars[len(row.bars)-1]
	x1 := last.x + last.w

	// Cursor highlight behind everything.
	for bi := range row.bars {
		b := &row.bars[bi]
		if b.master != curMaster {
			continue
		}
		mb := v.song.MasterBars[b.master]
		i := sort.Search(len(b.starts), func(i int) bool { return b.starts[i] > curTick }) - 1
		i = max(i, 0)
		hx0 := b.x + b.xs[i] - v.px(9)
		hx1 := b.x + b.xs[i+1] - v.px(9)
		hl, ln := colCursor, colCursorLn
		if v.Recording {
			hl, ln = color.NRGBA{R: 0xe0, G: 0x3a, B: 0x30, A: 0x38}, rgb(0xd8403a)
		}
		fillRect(gtx, hx0, top-v.px(8), hx1, bottom+v.px(8), hl)
		cx := b.tickX(curTick, mb.Duration) - v.px(9)
		fillRect(gtx, cx, top-v.px(12), cx+v.px(2), bottom+v.px(12), ln)
	}

	// Staff lines.
	for s := range n {
		y := top + float32(s)*gap
		fillRect(gtx, x0, y, x1, y+v.px(1), colLine)
	}
	// Left labels.
	if perc {
		v.text(gtx, "BAT", 10, colFaint, x0-v.px(18), (top+bottom)/2, alignCenter, true, nil)
	} else if st != nil {
		for s := range n {
			name := ""
			if s < len(st.Tuning) {
				name = noteName(st.Tuning[s])
			}
			y := top + float32(n-1-s)*gap
			v.text(gtx, name, 10, colFaint, x0-v.px(14), y, alignCenter, false, nil)
		}
	}

	for bi := range row.bars {
		b := &row.bars[bi]
		mb := v.song.MasterBars[b.master]
		bx, bw := b.x, b.w

		// Bar lines.
		fillRect(gtx, bx+bw-v.px(1), top, bx+bw, bottom+v.px(1), colInk)
		if bi == 0 {
			fillRect(gtx, bx, top, bx+v.px(1), bottom+v.px(1), colInk)
		}
		mid := (top + bottom) / 2
		dotR := v.px(2.2)
		if mb.RepeatStart {
			fillRect(gtx, bx, top, bx+v.px(3), bottom+v.px(1), colInk)
			fillRect(gtx, bx+v.px(5), top, bx+v.px(6), bottom+v.px(1), colInk)
			dot(gtx, bx+v.px(10), mid-gap/2, dotR, colInk)
			dot(gtx, bx+v.px(10), mid+gap/2, dotR, colInk)
		}
		if mb.RepeatEnd {
			ex := bx + bw
			fillRect(gtx, ex-v.px(3), top, ex, bottom+v.px(1), colInk)
			fillRect(gtx, ex-v.px(6), top, ex-v.px(5), bottom+v.px(1), colInk)
			dot(gtx, ex-v.px(10), mid-gap/2, dotR, colInk)
			dot(gtx, ex-v.px(10), mid+gap/2, dotR, colInk)
			if mb.RepeatCount > 2 {
				v.text(gtx, fmt.Sprintf("x%d", mb.RepeatCount), 10, colInk, ex-v.px(4), top-v.px(9), alignRight, true, nil)
			}
		}

		// Header: bar number, section, tempo, alternate endings.
		v.text(gtx, fmt.Sprint(b.master+1), 9, colFaint, bx+v.px(2), top-v.px(9), alignLeft, false, nil)
		hx := bx + v.px(2)
		if mb.Section != "" {
			d := v.text(gtx, mb.Section, 11, colSection, hx, v.px(9), alignLeft, true, nil)
			hx += float32(d.X) + v.px(8)
		}
		for _, tc := range mb.TempoChanges {
			v.text(gtx, fmt.Sprintf("%.0f bpm", tc.BPM), 10, colInk, hx, v.px(9), alignLeft, false, nil)
		}
		if mb.Alternate != 0 {
			y := top - v.px(20)
			fillRect(gtx, bx+v.px(2), y, bx+bw-v.px(4), y+v.px(1), colInk)
			fillRect(gtx, bx+v.px(2), y, bx+v.px(3), y+v.px(7), colInk)
			label := ""
			for k := range 32 {
				if mb.Alternate&(1<<k) != 0 {
					if label != "" {
						label += ","
					}
					label += fmt.Sprint(k + 1)
				}
			}
			v.text(gtx, label+".", 9, colInk, bx+v.px(6), y+v.px(6), alignLeft, false, nil)
		}

		// Time signature.
		if b.showSig {
			sx := bx + b.sigX
			bg := colPaper
			v.text(gtx, fmt.Sprint(mb.Num), 15, colInk, sx, mid-v.px(7), alignCenter, true, &bg)
			v.text(gtx, fmt.Sprint(mb.Den), 15, colInk, sx, mid+v.px(8), alignCenter, true, &bg)
		}

		if st == nil || b.master >= len(st.Bars) {
			continue
		}
		bar := st.Bars[b.master]
		for vi, voice := range bar.Voices {
			col := colInk
			if vi > 0 {
				col = colVoice2
			}
			for _, beat := range voice {
				x := b.beatX(beat.Start)
				size := unit.Sp(11)
				if beat.Grace {
					x -= v.px(9)
					size = 8
				}
				for _, note := range beat.Notes {
					if perc {
						v.drumNote(gtx, note, x, top, gap, col)
						continue
					}
					y := top + float32(n-1-note.String)*gap
					label := fmt.Sprint(note.Fret)
					switch {
					case note.Dead:
						label = "x"
					case note.TieDest || note.Ghost:
						label = "(" + label + ")"
					}
					bg := colPaper
					v.text(gtx, label, size, col, x, y, alignCenter, false, &bg)
				}
			}
			if vi == 0 {
				v.drawRhythm(gtx, b, voice, bottom)
			}
		}
	}
}

func (v *TabView) drumNote(gtx layout.Context, note *gp.Note, x, top, gap float32, col color.NRGBA) {
	tr := v.song.Tracks[v.track]
	line, cross := 4, false
	if note.Drum >= 0 && note.Drum < len(tr.DrumMap) {
		a := tr.DrumMap[note.Drum]
		line, cross = a.StaffLine, a.Cross
	}
	y := top + float32(line)*gap/2
	r := gap * 0.36
	if line < 0 || line > 8 {
		// Ledger line.
		fillRect(gtx, x-r*1.8, y, x+r*1.8, y+v.px(1), colLine)
	}
	if cross {
		var p clip.Path
		p.Begin(gtx.Ops)
		p.MoveTo(pt(x-r, y-r))
		p.LineTo(pt(x+r, y+r))
		p.MoveTo(pt(x-r, y+r))
		p.LineTo(pt(x+r, y-r))
		paint.FillShape(gtx.Ops, col, clip.Stroke{Path: p.End(), Width: v.px(1.6)}.Op())
		return
	}
	dot(gtx, x, y, r*1.15, col)
}

// drawRhythm draws stems, flags/beams, dots and tuplet marks under the staff.
func (v *TabView) drawRhythm(gtx layout.Context, b *tabBar, voice []*gp.Beat, staffBottom float32) {
	y0 := staffBottom + v.px(v.rhythmGap())
	stemLen := v.px(18)
	beamGap := v.px(4)
	type stem struct {
		x     float32
		flags int
		group int
		beat  *gp.Beat
	}
	var stems []stem
	for _, beat := range voice {
		if beat.Grace {
			continue
		}
		x := b.beatX(beat.Start)
		if len(beat.Notes) == 0 {
			// Rest: a small block.
			fillRect(gtx, x-v.px(3), y0+v.px(6), x+v.px(3), y0+v.px(9), colFaint)
			stems = append(stems, stem{x: x, flags: -1, group: -1})
			continue
		}
		if beat.Value < 2 {
			stems = append(stems, stem{x: x, flags: -1, group: -1})
			continue
		}
		l := stemLen
		if beat.Value == 2 {
			l = stemLen / 2
		}
		fillRect(gtx, x-v.px(0.6), y0, x+v.px(0.6), y0+l, colInk)
		flags := int(math.Log2(float64(beat.Value))) - 2
		if beat.Dots > 0 {
			dot(gtx, x+v.px(5), y0+l-v.px(2), v.px(1.4), colInk)
		}
		stems = append(stems, stem{x: x, flags: max(flags, 0), group: beat.Start / gp.TicksPerQuarter, beat: beat})
	}
	yb := y0 + stemLen
	for i := 0; i < len(stems); {
		s := stems[i]
		if s.flags <= 0 {
			i++
			continue
		}
		j := i
		for j+1 < len(stems) && stems[j+1].flags > 0 && stems[j+1].group == s.group {
			j++
		}
		if j == i {
			// Lone flagged note: short stubs.
			for k := range s.flags {
				y := yb - float32(k)*beamGap
				fillRect(gtx, s.x, y-v.px(2), s.x+v.px(7), y, colInk)
			}
		} else {
			common := s.flags
			for k := i; k <= j; k++ {
				common = min(common, stems[k].flags)
			}
			for k := range common {
				y := yb - float32(k)*beamGap
				fillRect(gtx, s.x-v.px(0.6), y-v.px(2), stems[j].x+v.px(0.6), y, colInk)
			}
			for k := i; k <= j; k++ {
				for f := common; f < stems[k].flags; f++ {
					y := yb - float32(f)*beamGap
					dir := float32(1)
					if k == j {
						dir = -1
					}
					fillRect(gtx, min(stems[k].x, stems[k].x+dir*v.px(6)), y-v.px(2), max(stems[k].x, stems[k].x+dir*v.px(6)), y, colInk)
				}
			}
		}
		if s.beat != nil && s.beat.Tuplet > 0 {
			v.text(gtx, fmt.Sprint(s.beat.Tuplet), 8, colInk, (s.x+stems[j].x)/2, yb+v.px(7), alignCenter, false, nil)
		}
		i = j + 1
	}
}

const (
	alignLeft = iota
	alignCenter
	alignRight
)

// text draws s vertically centred on y and returns its size.
func (v *TabView) text(gtx layout.Context, s string, size unit.Sp, col color.NRGBA, x, y float32, align int, bold bool, bg *color.NRGBA) image.Point {
	l := material.Label(v.th, size, s)
	l.Color = col
	l.MaxLines = 1
	if bold {
		l.Font.Weight = font.Bold
	}
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(4096, 4096)}
	d := l.Layout(g)
	call := m.Stop()
	ox := x
	switch align {
	case alignCenter:
		ox -= float32(d.Size.X) / 2
	case alignRight:
		ox -= float32(d.Size.X)
	}
	oy := y - float32(d.Size.Y)/2
	if bg != nil {
		pad := v.px(1)
		fillRect(gtx, ox-pad, y-float32(d.Size.Y)*0.36, ox+float32(d.Size.X)+pad, y+float32(d.Size.Y)*0.36, *bg)
	}
	st := op.Offset(image.Pt(int(math.Round(float64(ox))), int(math.Round(float64(oy))))).Push(gtx.Ops)
	call.Add(gtx.Ops)
	st.Pop()
	return d.Size
}

func fillRect(gtx layout.Context, x0, y0, x1, y1 float32, c color.NRGBA) {
	r := image.Rect(int(math.Round(float64(x0))), int(math.Round(float64(y0))),
		int(math.Round(float64(x1))), int(math.Round(float64(y1))))
	if r.Dx() == 0 {
		r.Max.X++
	}
	if r.Dy() == 0 {
		r.Max.Y++
	}
	paint.FillShape(gtx.Ops, c, clip.Rect(r).Op())
}

func dot(gtx layout.Context, x, y, r float32, c color.NRGBA) {
	rect := image.Rect(int(x-r), int(y-r), int(math.Ceil(float64(x+r))), int(math.Ceil(float64(y+r))))
	paint.FillShape(gtx.Ops, c, clip.Ellipse(rect).Op(gtx.Ops))
}

func noteName(midi int) string {
	names := [...]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}
	return names[((midi%12)+12)%12]
}

func rgb(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}
