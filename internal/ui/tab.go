package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"sort"

	"gioui.org/f32"
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
	colStaff    = rgb(0x6a6862)
	colInk      = rgb(0x1f1f1f)
	colFaint    = rgb(0x9a968c)
	colSection  = rgb(0xb8452f)
	colCursor   = color.NRGBA{R: 0xff, G: 0xa5, B: 0x1c, A: 0x40}
	colCursorLn = rgb(0xe8850c)
)

// TabView draws one track as a system per row: the staff notation on top and
// the tablature below, sharing the same rhythmic columns, and follows the
// playback cursor. Drum tracks only have the (percussion) staff.
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

	// Notation of the displayed track, rebuilt by relayout.
	clef clefKind
	info map[*gp.Note]noteInfo
	// above and below are the room the row being laid out or drawn needs
	// over and under the notation staff (px); see rowSpace.
	above, below float32

	lastRow int
	Follow  bool
	// Zoom scales the whole score (1 = 100%).
	Zoom     float32
	laidZoom float32
	// Recording turns the cursor red.
	Recording bool

	// Clicked is set to the master bar index the user clicked, or -1.
	Clicked int
}

type tabRow struct {
	x0 float32 // where the first bar starts (after clef and key signature)
	// Room needed over and under the notation staff (px).
	above, below float32
	bars         []tabBar
}

type tabBar struct {
	master  int
	x, w    float32
	starts  []int     // unique beat start ticks
	xs      []float32 // x of each start relative to bar x, plus bar content end
	lead    []float32 // room before each start for accidentals; same length as xs
	showSig bool
	sigX    float32 // centre of the time signature, relative to x
	showKey bool    // key signature printed inside the row (it changed here)
	keyX    float32 // left edge of the key signature, relative to x
	key     int
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
	if st := v.staff(); st != nil && len(st.Tuning) > 0 {
		return len(st.Tuning)
	}
	return 6
}

// Geometry in dp.
const (
	dpMargin    = 16
	dpSp        = 8  // staff space of the notation
	dpHead      = 30 // bar numbers, sections, tempo, endings
	dpAbove     = 24 // room above the staff for stems and ledger lines
	dpBelow     = 24 // room below the staff
	dpTabGap    = 18 // distance between tablature lines
	dpTabPad    = 6
	dpRowBottom = 14
	dpClefW     = 30
	dpKeyAcc    = 9 // width of one key signature symbol
	dpBarPad    = 12
	dpSigW      = 26
	dpRepeat    = 12
	dpAccLead   = 12 // room an accidental needs before its note
)

func (v *TabView) px(dp float32) float32 { return dp * v.metric.PxPerDp * v.zoom() }

// zoom is the score magnification, 1 when unset.
func (v *TabView) zoom() float32 {
	if v.Zoom <= 0 {
		return 1
	}
	return v.Zoom
}

func (v *TabView) sp() float32        { return v.px(dpSp) }
func (v *TabView) nTop() float32      { return v.px(dpHead) + v.above }
func (v *TabView) nBottom() float32   { return v.nTop() + 4*v.sp() }
func (v *TabView) tabTop() float32    { return v.nBottom() + v.below + v.px(dpTabPad) }
func (v *TabView) tabGap() float32    { return v.px(dpTabGap) }
func (v *TabView) tabBottom() float32 { return v.tabTop() + float32(v.strings()-1)*v.tabGap() }

// lastBottom is the bottom of the lowest staff of a row.
func (v *TabView) lastBottom() float32 {
	if v.percussion() {
		return v.nBottom()
	}
	return v.tabBottom()
}

func (v *TabView) rowHeight() int {
	extra := float32(0)
	if v.percussion() {
		extra = v.below
	}
	return int(v.lastBottom() + extra + v.px(dpRowBottom))
}

// rowSpace estimates the room a row needs over and under its notation staff:
// at least the default, more where high or low notes carry their stems
// outwards or sit far from the staff.
func (v *TabView) rowSpace(row *tabRow) (above, below float32) {
	above, below = v.px(dpAbove), v.px(dpBelow)
	st := v.staff()
	if st == nil {
		return
	}
	perc := v.clef == clefPerc
	top, bottom := 8, 0 // steps the row reaches, the staff itself at least
	for _, b := range row.bars {
		if b.master >= len(st.Bars) {
			continue
		}
		bar := st.Bars[b.master]
		multi := len(shownVoices(bar)) > 1
		for vi, voice := range shownVoices(bar) {
			for _, beat := range voice {
				lo, hi := 1000, -1000
				for _, n := range beat.Notes {
					if info, ok := v.info[n]; ok {
						lo, hi = min(lo, info.step), max(hi, info.step)
					}
				}
				if hi < lo {
					continue
				}
				up := perc || float32(lo+hi)/2 < 4
				if multi {
					up = vi == 0
				}
				stem := 8 + 2*max(flagCount(beat.Value)-1, 0) // stem plus beams and a margin
				if beat.Value < 2 {
					stem = 2
				}
				if up {
					top, bottom = max(top, hi+stem), min(bottom, lo-2)
				} else {
					top, bottom = max(top, hi+2), min(bottom, lo-stem)
				}
			}
		}
	}
	above = max(above, float32(top-8)*v.sp()/2+v.px(4))
	below = max(below, float32(-bottom)*v.sp()/2+v.px(4))
	return
}

// yStep is the y of a staff step of the notation (0 = bottom line).
func (v *TabView) yStep(s float32) float32 { return v.nBottom() - s*v.sp()/2 }

// keyWidth is the width of a key signature.
func (v *TabView) keyWidth(key int) float32 {
	if key == 0 {
		return 0
	}
	return v.px(float32(iabs(key))*dpKeyAcc + 4)
}

// gutter is where the first bar of a row starts: after the clef and the key
// signature.
func (v *TabView) gutter(key int) float32 {
	return v.px(dpMargin+dpClefW) + v.keyWidth(key)
}

func iabs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func beatSpace(ticks int) float32 {
	return max(16, 20*float32(math.Sqrt(float64(ticks)/240)))
}

func (v *TabView) relayout(width int) {
	v.rows = v.rows[:0]
	st := v.staff()
	if st == nil {
		return
	}
	tr := v.song.Tracks[v.track]
	v.clef = clefOf(tr)
	keyOf := func(i int) int {
		if v.clef == clefPerc {
			return 0
		}
		return v.song.MasterBars[i].Key
	}
	v.info = make(map[*gp.Note]noteInfo)
	for i := range v.song.MasterBars {
		if i < len(st.Bars) {
			analyzeBar(tr, st.Bars[i], keyOf(i), v.clef, v.info)
		}
	}

	// measure lays out the columns of master bar i. first is whether it opens
	// a row, where the key signature goes in the gutter instead of the bar.
	measure := func(i int, first bool) tabBar {
		mb := v.song.MasterBars[i]
		var bar *gp.Bar
		if i < len(st.Bars) {
			bar = st.Bars[i]
		} else {
			bar = &gp.Bar{}
		}
		set := map[int]bool{}
		acc := map[int]bool{}
		for _, voice := range shownVoices(bar) {
			for _, b := range voice {
				if b.Grace {
					continue
				}
				set[b.Start] = true
				for _, n := range b.Notes {
					if v.info[n].showAcc {
						acc[b.Start] = true
					}
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
		key := keyOf(i)
		showSig := i == 0 || mb.Num != v.song.MasterBars[i-1].Num || mb.Den != v.song.MasterBars[i-1].Den
		showKey := !first && i > 0 && key != keyOf(i-1) && key != 0
		x := v.px(dpBarPad)
		if mb.RepeatStart {
			x += v.px(dpRepeat)
		}
		keyX := x
		if showKey {
			x += v.keyWidth(key)
		}
		sigX := x + v.px(6)
		if showSig {
			x += v.px(dpSigW)
		}
		xs := make([]float32, 0, len(starts)+1)
		lead := make([]float32, 0, len(starts)+1)
		for j, s := range starts {
			l := float32(0)
			if acc[s] {
				l = v.px(dpAccLead)
			}
			x += l
			xs = append(xs, x)
			lead = append(lead, l)
			next := mb.Duration
			if j+1 < len(starts) {
				next = starts[j+1]
			}
			x += v.px(beatSpace(max(next-s, 60)))
		}
		xs = append(xs, x)
		lead = append(lead, 0)
		w := x + v.px(4)
		if mb.RepeatEnd {
			w += v.px(dpRepeat)
		}
		return tabBar{master: i, w: w, starts: starts, xs: xs, lead: lead,
			showSig: showSig, sigX: sigX, showKey: showKey, keyX: keyX, key: key}
	}

	var row tabRow
	var rowW, avail float32
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
		x := row.x0
		for i := range row.bars {
			row.bars[i].x = x
			x += row.bars[i].w
		}
		row.above, row.below = v.rowSpace(&row)
		v.rows = append(v.rows, row)
		row, rowW = tabRow{}, 0
	}
	for i := range v.song.MasterBars {
		b := measure(i, len(row.bars) == 0)
		if len(row.bars) > 0 && rowW+b.w > avail {
			flush(true)
			b = measure(i, true)
		}
		if len(row.bars) == 0 {
			row.x0 = v.gutter(keyOf(i))
			avail = float32(width) - row.x0 - v.px(dpMargin)
		}
		row.bars = append(row.bars, b)
		rowW += b.w
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
	if v.dirty || gtx.Constraints.Max.X != v.width || gtx.Metric != v.metric || v.Zoom != v.laidZoom {
		v.laidZoom = v.Zoom
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

	return v.list.Layout(gtx, len(v.rows), func(gtx layout.Context, i int) layout.Dimensions {
		v.above, v.below = v.rows[i].above, v.rows[i].below
		size := image.Pt(gtx.Constraints.Max.X, v.rowHeight())
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

// barLine draws a vertical line through the staves of the row.
func (v *TabView) barLine(gtx layout.Context, x0, x1 float32, col color.NRGBA) {
	fillRect(gtx, x0, v.nTop(), x1, v.nBottom()+v.px(1), col)
	if !v.percussion() {
		fillRect(gtx, x0, v.tabTop(), x1, v.tabBottom()+v.px(1), col)
	}
}

// repeatDots draws the two dots of a repeat sign at x.
func (v *TabView) repeatDots(gtx layout.Context, x float32) {
	r := v.px(2.2)
	dot(gtx, x, v.yStep(5), r, colInk)
	dot(gtx, x, v.yStep(3), r, colInk)
	if !v.percussion() {
		mid := (v.tabTop() + v.tabBottom()) / 2
		dot(gtx, x, mid-v.tabGap()/2, r, colInk)
		dot(gtx, x, mid+v.tabGap()/2, r, colInk)
	}
}

func (v *TabView) drawKeySig(gtx layout.Context, x float32, key int) {
	if key == 0 || v.clef == clefPerc {
		return
	}
	steps := keySigSteps[v.clef][key > 0]
	p := pen{gtx: gtx, sp: v.sp(), col: colInk}
	for i := range iabs(key) {
		cx := x + v.px(3.5) + float32(i)*v.px(dpKeyAcc)
		y := v.yStep(float32(steps[i]))
		if key > 0 {
			p.sharp(cx, y)
		} else {
			p.flat(cx, y)
		}
	}
}

func (v *TabView) drawRow(gtx layout.Context, row *tabRow, curMaster, curTick int) {
	v.above, v.below = row.above, row.below
	st := v.staff()
	perc := v.percussion()
	n := v.strings()
	sp := v.sp()
	nTop := v.nTop()
	tTop, tBot, tGap := v.tabTop(), v.tabBottom(), v.tabGap()
	bottom := v.lastBottom()
	xl := v.px(dpMargin)
	last := row.bars[len(row.bars)-1]
	x1 := last.x + last.w
	p := pen{gtx: gtx, sp: sp, col: colInk}

	// Cursor highlight behind everything.
	for bi := range row.bars {
		b := &row.bars[bi]
		if b.master != curMaster {
			continue
		}
		mb := v.song.MasterBars[b.master]
		i := sort.Search(len(b.starts), func(i int) bool { return b.starts[i] > curTick }) - 1
		i = max(i, 0)
		hx0 := b.x + b.xs[i] - v.px(9) - b.lead[i]
		hx1 := b.x + b.xs[i+1] - v.px(9) - b.lead[i+1]
		hl, ln := colCursor, colCursorLn
		if v.Recording {
			hl, ln = color.NRGBA{R: 0xe0, G: 0x3a, B: 0x30, A: 0x38}, rgb(0xd8403a)
		}
		fillRect(gtx, hx0, nTop-v.px(12), hx1, bottom+v.px(8), hl)
		cx := b.tickX(curTick, mb.Duration) - v.px(9)
		fillRect(gtx, cx, nTop-v.px(14), cx+v.px(2), bottom+v.px(10), ln)
	}

	// Staff lines.
	for s := range 5 {
		y := nTop + float32(s)*sp
		fillRect(gtx, xl, y, x1, y+v.px(1), colStaff)
	}
	if !perc {
		for s := range n {
			y := tTop + float32(s)*tGap
			fillRect(gtx, xl, y, x1, y+v.px(1), colLine)
		}
	}
	// System start: a line joining the staves.
	fillRect(gtx, xl, nTop, xl+v.px(1), bottom+v.px(1), colInk)
	if !perc {
		fillRect(gtx, xl-v.px(4), nTop, xl-v.px(2), tBot+v.px(1), colInk)
	}

	// Clef, key signature and TAB mark.
	clefX := xl + v.px(5)
	switch v.clef {
	case clefTreble:
		p.trebleClef(clefX, v.yStep(2))
	case clefBass:
		p.bassClef(clefX, v.yStep(6))
	default:
		p.percClef(clefX, v.yStep(4))
	}
	if !perc {
		v.drawKeySig(gtx, xl+v.px(dpClefW), row.bars[0].key)
		mid := (tTop + tBot) / 2
		for k, ch := range []string{"T", "A", "B"} {
			v.text(gtx, ch, 12, colInk, xl+v.px(14), mid+float32(k-1)*v.px(13), alignCenter, true, nil)
		}
	}

	var ties [4]map[int]float32
	for bi := range row.bars {
		b := &row.bars[bi]
		mb := v.song.MasterBars[b.master]
		bx, bw := b.x, b.w

		// Bar lines.
		v.barLine(gtx, bx+bw-v.px(1), bx+bw, colInk)
		if mb.RepeatStart {
			v.barLine(gtx, bx, bx+v.px(3), colInk)
			v.barLine(gtx, bx+v.px(5), bx+v.px(6), colInk)
			v.repeatDots(gtx, bx+v.px(10))
		}
		if mb.RepeatEnd {
			ex := bx + bw
			v.barLine(gtx, ex-v.px(3), ex, colInk)
			v.barLine(gtx, ex-v.px(6), ex-v.px(5), colInk)
			v.repeatDots(gtx, ex-v.px(10))
			if mb.RepeatCount > 2 {
				v.text(gtx, fmt.Sprintf("x%d", mb.RepeatCount), 10, colInk, ex-v.px(4), nTop-v.px(22), alignRight, true, nil)
			}
		}

		// Header: bar number, section, tempo, alternate endings.
		v.text(gtx, fmt.Sprint(b.master+1), 9, colFaint, bx+v.px(2), nTop-v.px(11), alignLeft, false, nil)
		hx := bx + v.px(2)
		if mb.Section != "" {
			d := v.text(gtx, mb.Section, 11, colSection, hx, v.px(9), alignLeft, true, nil)
			hx += float32(d.X) + v.px(8)
		}
		for _, tc := range mb.TempoChanges {
			v.text(gtx, fmt.Sprintf("%.0f bpm", tc.BPM), 10, colInk, hx, v.px(9), alignLeft, false, nil)
		}
		if mb.Alternate != 0 {
			y := v.px(dpHead - 8)
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

		// Key and time signatures.
		if b.showKey {
			v.drawKeySig(gtx, bx+b.keyX, b.key)
		}
		if b.showSig {
			sx := bx + b.sigX
			v.text(gtx, fmt.Sprint(mb.Num), 21, colInk, sx, v.yStep(6), alignCenter, true, nil)
			v.text(gtx, fmt.Sprint(mb.Den), 21, colInk, sx, v.yStep(2), alignCenter, true, nil)
			if !perc {
				mid := (tTop + tBot) / 2
				bg := colPaper
				v.text(gtx, fmt.Sprint(mb.Num), 15, colInk, sx, mid-v.px(7), alignCenter, true, &bg)
				v.text(gtx, fmt.Sprint(mb.Den), 15, colInk, sx, mid+v.px(8), alignCenter, true, &bg)
			}
		}

		if st == nil || b.master >= len(st.Bars) {
			continue
		}
		bar := st.Bars[b.master]
		for vi, voice := range shownVoices(bar) {
			col := colInk
			var tie map[int]float32
			if vi < len(ties) {
				if ties[vi] == nil {
					ties[vi] = map[int]float32{}
				}
				tie = ties[vi]
			}
			v.drawVoice(gtx, b, mb, voice, vi, len(shownVoices(bar)) > 1, col, tie)
			if perc {
				continue
			}
			for _, beat := range voice {
				x := b.beatX(beat.Start)
				size := unit.Sp(16)
				if beat.Grace {
					x -= v.px(9)
					size = 11
				}
				for _, note := range beat.Notes {
					y := tTop + float32(n-1-note.String)*tGap
					label := fmt.Sprint(note.Fret)
					switch {
					case note.Dead:
						label = "x"
					case note.TieDest || note.Ghost:
						label = "(" + label + ")"
					}
					bg := colPaper
					numCol := rgb(0x000000)
					v.text(gtx, label, size, numCol, x, y, alignCenter, true, &bg)
				}
			}
		}
	}
}

// nHead is one note head of the staff.
type nHead struct {
	n    *gp.Note
	info noteInfo
	x, y float32
	kind int
	flip bool // displaced to the other side of the stem
}

// nBeat is one beat of a voice as drawn on the staff.
type nBeat struct {
	b     *gp.Beat
	x     float32
	heads []nHead // lowest first
	rest  bool
	grace bool
	up    bool
	flags int
	stemX float32
	tipY  float32 // end of the stem, where flags or the beam start
	stem  bool
	beam  bool // joined to its neighbours by a beam
}

// drawVoice engraves one voice of a bar on the staff: heads, accidentals,
// stems, beams, flags, rests, dots, ties and tuplet numbers.
func (v *TabView) drawVoice(gtx layout.Context, b *tabBar, mb *gp.MasterBar, voice []*gp.Beat, vi int, multi bool, col color.NRGBA, tie map[int]float32) {
	tr := v.song.Tracks[v.track]
	perc := v.clef == clefPerc
	sp := v.sp()
	hw := 0.6 * sp
	stemW := max(1, float32(math.Round(float64(0.12*sp))))
	pn := pen{gtx: gtx, sp: sp, col: col}
	yS := v.yStep

	// 1. Collect the beats.
	var bs []nBeat
	for _, beat := range voice {
		nb := nBeat{b: beat, x: b.beatX(beat.Start), flags: flagCount(beat.Value)}
		if beat.Grace {
			nb.grace = true
			nb.x -= v.px(12)
			nb.flags = 1
		}
		for _, nt := range beat.Notes {
			info, ok := v.info[nt]
			if !ok {
				continue
			}
			kind := headFilled
			switch {
			case beat.Value <= 1:
				kind = headWhole
			case beat.Value == 2:
				kind = headHalf
			}
			if nt.Dead || (perc && nt.Drum >= 0 && nt.Drum < len(tr.DrumMap) && tr.DrumMap[nt.Drum].Cross) {
				kind = headCross
			}
			nb.heads = append(nb.heads, nHead{n: nt, info: info, kind: kind})
		}
		if len(beat.Notes) == 0 {
			nb.rest = true
		} else if len(nb.heads) == 0 {
			continue
		}
		sort.SliceStable(nb.heads, func(i, j int) bool { return nb.heads[i].info.step < nb.heads[j].info.step })
		bs = append(bs, nb)
	}

	// 2. Beam groups: runs of eighth or shorter notes in the same beat of
	// the bar, not interrupted by rests.
	glen := beamGroupLen(mb)
	var groups [][]int
	var cur []int
	curG := -1
	closeGroup := func() {
		if len(cur) >= 2 {
			groups = append(groups, cur)
		}
		cur = nil
	}
	for i := range bs {
		nb := &bs[i]
		if nb.grace || nb.rest || nb.flags == 0 {
			closeGroup()
			continue
		}
		if g := nb.b.Start / glen; len(cur) > 0 && g != curG {
			closeGroup()
		}
		cur = append(cur, i)
		curG = nb.b.Start / glen
	}
	closeGroup()

	// 3. Stem directions. With several voices the first goes up and the
	// others down; otherwise stems point away from the middle line (drums
	// always go up).
	mid := func(nb *nBeat) float32 {
		h := nb.heads
		return float32(h[0].info.step+h[len(h)-1].info.step)/2 - 4
	}
	for _, g := range groups {
		up := vi == 0
		if !multi {
			var sum float32
			for _, i := range g {
				sum += mid(&bs[i])
			}
			up = sum < 0 || perc
		}
		for _, i := range g {
			bs[i].up, bs[i].beam = up, true
		}
	}
	for i := range bs {
		nb := &bs[i]
		if nb.rest || nb.beam {
			continue
		}
		switch {
		case nb.grace:
			nb.up = true
		case multi:
			nb.up = vi == 0
		default:
			nb.up = mid(nb) < 0 || perc
		}
	}

	// 4. Place heads (displacing seconds) and compute stems.
	for i := range bs {
		nb := &bs[i]
		if nb.rest {
			continue
		}
		h := nb.heads
		for j := range h {
			h[j].x, h[j].y = nb.x, yS(float32(h[j].info.step))
		}
		shift := 1.94 * hw
		if nb.up {
			for j := 1; j < len(h); j++ {
				if h[j].info.step-h[j-1].info.step == 1 && !h[j-1].flip {
					h[j].flip = true
					h[j].x += shift
				}
			}
		} else {
			for j := len(h) - 2; j >= 0; j-- {
				if h[j+1].info.step-h[j].info.step == 1 && !h[j+1].flip {
					h[j].flip = true
					h[j].x -= shift
				}
			}
		}
		nb.stem = nb.b.Value >= 2 || nb.grace
		if nb.up {
			nb.stemX = nb.x + hw - stemW/2
		} else {
			nb.stemX = nb.x - hw + stemW/2
		}
		if nb.grace {
			nb.stemX = nb.x + 0.7*hw - stemW/2
		}
		low, high := float32(h[0].info.step), float32(h[len(h)-1].info.step)
		length := float32(7) + 1.5*float32(max(nb.flags-1, 0))
		if nb.grace {
			length = 5
		}
		if nb.up {
			nb.tipY = yS(max(high+length, 4))
		} else {
			nb.tipY = yS(min(low-length, 4))
		}
	}

	// 5. Beams: fit a line to the group, never shorter than any stem.
	type beamLine struct {
		x0, x1 float32 // stem positions of the first and last note
		b0, b1 float32 // beam outer edge at x0 and x1, in steps
	}
	lines := make([]beamLine, len(groups))
	for gi, g := range groups {
		first, lastB := &bs[g[0]], &bs[g[len(g)-1]]
		up := first.up
		maxF := 0
		for _, i := range g {
			maxF = max(maxF, bs[i].flags)
		}
		length := float32(7) + 1.5*float32(maxF-1)
		tip := func(nb *nBeat) float32 {
			h := nb.heads
			if up {
				return float32(h[len(h)-1].info.step) + length
			}
			return float32(h[0].info.step) - length
		}
		span := lastB.stemX - first.stemX
		frac := func(nb *nBeat) float32 {
			if span == 0 {
				return 0
			}
			return (nb.stemX - first.stemX) / span
		}
		slant := min(max((tip(lastB)-tip(first))*0.5, -2), 2)
		var b0 float32
		for k, i := range g {
			req := tip(&bs[i])
			if up {
				req = max(req, 4)
			} else {
				req = min(req, 4)
			}
			c := req - slant*frac(&bs[i])
			if k == 0 || (up && c > b0) || (!up && c < b0) {
				b0 = c
			}
		}
		lines[gi] = beamLine{first.stemX, lastB.stemX, b0, b0 + slant}
		for _, i := range g {
			bs[i].tipY = yS(b0 + slant*frac(&bs[i]))
		}
	}

	// 6. Draw rests.
	for i := range bs {
		nb := &bs[i]
		if !nb.rest {
			continue
		}
		x := nb.x
		if nb.b.Value <= 1 && nb.b.Start == 0 && len(voice) == 1 {
			x = (b.x + b.xs[0] + b.x + b.xs[len(b.xs)-1]) / 2
		}
		ys := yS
		if vi > 0 {
			ys = func(s float32) float32 { return yS(s - 2) }
		}
		pn.rest(x, nb.b.Value, ys)
		for d := range nb.b.Dots {
			pn.dotAt(x+0.85*sp+float32(d)*0.45*sp, ys(5))
		}
	}

	// 7. Draw notes: ledger lines, accidentals, heads, dots, stems, flags.
	for i := range bs {
		nb := &bs[i]
		if nb.rest {
			continue
		}
		h := nb.heads
		minX := h[0].x
		for _, hd := range h {
			minX = min(minX, hd.x)
			v.ledgerLines(gtx, hd.x, hd.info.step, hw*1.75, col)
		}
		if !nb.grace {
			type placed struct{ step, col int }
			var pl []placed
			for j := len(h) - 1; j >= 0; j-- {
				if !h[j].info.showAcc {
					continue
				}
				c := 0
			retry:
				for _, q := range pl {
					if q.col == c && iabs(q.step-h[j].info.step) < 6 {
						c++
						goto retry
					}
				}
				pl = append(pl, placed{h[j].info.step, c})
				pn.accidental(h[j].info.acc, minX-hw-0.85*sp-float32(c)*1.05*sp, h[j].y)
			}
		}
		scale := float32(1)
		if nb.grace {
			scale = 0.7
		}
		maxX := minX
		for _, hd := range h {
			pn.head(hd.x, hd.y, hd.kind, scale)
			maxX = max(maxX, hd.x)
		}
		if nb.b.Dots > 0 && !nb.grace {
			used := map[float32]bool{}
			for _, hd := range h {
				y := hd.y
				if hd.info.step%2 == 0 {
					y -= sp / 2
				}
				if used[y] {
					continue
				}
				used[y] = true
				for d := range nb.b.Dots {
					pn.dotAt(maxX+hw+0.55*sp+float32(d)*0.45*sp, y)
				}
			}
		}
		if nb.stem {
			from := h[0].y // lowest head
			if !nb.up {
				from = h[len(h)-1].y
			}
			y0, y1 := min(from, nb.tipY), max(from, nb.tipY)
			fillRect(gtx, nb.stemX-stemW/2, y0, nb.stemX+stemW/2, y1, col)
		}
		if nb.grace {
			// Slashed flag.
			pn.flag(nb.stemX+stemW/2, nb.tipY, 1)
			pn.line(nb.stemX-0.5*sp, nb.tipY+0.9*sp, nb.stemX+1.0*sp, nb.tipY+0.3*sp, 1)
		} else if nb.stem && !nb.beam && nb.flags > 0 {
			dir := float32(1)
			if !nb.up {
				dir = -1
			}
			xr := nb.stemX + stemW/2
			for k := range nb.flags {
				pn.flag(xr-0.5, nb.tipY+dir*float32(k)*0.75*sp, dir)
			}
		}
	}

	// 8. Draw beams.
	for gi, g := range groups {
		ln := lines[gi]
		up := bs[g[0]].up
		dir := float32(1)
		if !up {
			dir = -1
		}
		thick := dir * 0.5 * sp
		lineY := func(x float32) float32 {
			if ln.x1 == ln.x0 {
				return yS(ln.b0)
			}
			return yS(ln.b0 + (ln.b1-ln.b0)*(x-ln.x0)/(ln.x1-ln.x0))
		}
		beam := func(xa, xb float32, level int) {
			off := dir * float32(level) * 0.75 * sp
			ya, yb := lineY(xa)+off, lineY(xb)+off
			pn.fill(func(path *clip.Path) {
				path.MoveTo(f32.Pt(xa, ya))
				path.LineTo(f32.Pt(xb, yb))
				path.LineTo(f32.Pt(xb, yb+thick))
				path.LineTo(f32.Pt(xa, ya+thick))
				path.Close()
			})
		}
		maxF := 0
		for _, i := range g {
			maxF = max(maxF, bs[i].flags)
		}
		beam(ln.x0-stemW/2, ln.x1+stemW/2, 0)
		for level := 1; level < maxF; level++ {
			for k := 0; k < len(g); {
				if bs[g[k]].flags <= level {
					k++
					continue
				}
				e := k
				for e+1 < len(g) && bs[g[e+1]].flags > level {
					e++
				}
				if e > k {
					beam(bs[g[k]].stemX-stemW/2, bs[g[e]].stemX+stemW/2, level)
				} else {
					// A lone short note: a stub towards the rest of the group.
					x := bs[g[k]].stemX
					if k == len(g)-1 {
						beam(x-1.1*sp, x+stemW/2, level)
					} else {
						beam(x-stemW/2, x+1.1*sp, level)
					}
				}
				k = e + 1
			}
		}
	}

	// 9. Ties. A tied note is joined to the previous note of the same pitch
	// in this voice; if that one is in another row, a short tie comes in.
	for i := range bs {
		nb := &bs[i]
		for _, hd := range nb.heads {
			if tie != nil && hd.n.TieDest {
				x0, ok := tie[hd.n.Key]
				if !ok {
					x0 = hd.x - 2.6*sp
				}
				dir := float32(1)
				if !nb.up {
					dir = -1
				}
				pn.tie(x0+hw+0.2*sp, hd.x-hw-0.2*sp, hd.y+dir*0.55*sp, dir)
			}
		}
		if tie != nil && !nb.grace {
			for _, hd := range nb.heads {
				tie[hd.n.Key] = hd.x
			}
		}
	}

	// 10. Tuplet numbers over (or under) each group of tuplet beats.
	for i := 0; i < len(bs); {
		t := bs[i].b.Tuplet
		if t == 0 || bs[i].grace {
			i++
			continue
		}
		e := i
		for e+1 < len(bs) && e+1-i < t && bs[e+1].b.Tuplet == t {
			e++
		}
		up, y := true, float32(math.MaxFloat32)
		low := float32(-1)
		for k := i; k <= e; k++ {
			if bs[k].rest || !bs[k].stem {
				continue
			}
			up = bs[k].up
			y = min(y, bs[k].tipY)
			low = max(low, bs[k].tipY)
		}
		ty := yS(10)
		switch {
		case y == math.MaxFloat32:
		case up:
			ty = y - 1.4*sp
		default:
			ty = low + 1.4*sp
		}
		v.text(gtx, fmt.Sprint(t), 10, col, (bs[i].x+bs[e].x)/2, ty, alignCenter, true, nil)
		i = e + 1
	}
}

// ledgerLines draws the short lines for a note outside the staff.
func (v *TabView) ledgerLines(gtx layout.Context, x float32, step int, half float32, col color.NRGBA) {
	w := v.px(1)
	if step < 0 {
		for s := -2; s >= step-(step%2); s -= 2 {
			y := v.yStep(float32(s))
			fillRect(gtx, x-half, y, x+half, y+w, col)
		}
	}
	if step > 8 {
		for s := 10; s <= step; s += 2 {
			y := v.yStep(float32(s))
			fillRect(gtx, x-half, y, x+half, y+w, col)
		}
	}
}

const (
	alignLeft = iota
	alignCenter
	alignRight
)

// text draws s vertically centred on y and returns its size.
func (v *TabView) text(gtx layout.Context, s string, size unit.Sp, col color.NRGBA, x, y float32, align int, bold bool, bg *color.NRGBA) image.Point {
	l := material.Label(v.th, size*unit.Sp(v.zoom()), s)
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
