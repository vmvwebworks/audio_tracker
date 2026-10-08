package ui

import (
	"image/color"
	"math"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// Vector music symbols. Gio has no music font, so every symbol is a path.
// A pen carries the drawing context; sp is the staff space in pixels and all
// the shapes scale with it.
type pen struct {
	gtx layout.Context
	sp  float32
	col color.NRGBA
}

// fill paints the closed shape built by f. Subpaths wound in opposite
// directions leave holes (non-zero winding rule).
func (p pen) fill(f func(*clip.Path)) {
	var path clip.Path
	path.Begin(p.gtx.Ops)
	f(&path)
	paint.FillShape(p.gtx.Ops, p.col, clip.Outline{Path: path.End()}.Op())
}

// stroke paints the outline of the path built by f, w pixels wide.
func (p pen) stroke(w float32, f func(*clip.Path)) {
	var path clip.Path
	path.Begin(p.gtx.Ops)
	f(&path)
	paint.FillShape(p.gtx.Ops, p.col, clip.Stroke{Path: path.End(), Width: max(w, 1)}.Op())
}

func (p pen) line(x0, y0, x1, y1, w float32) {
	p.stroke(w, func(path *clip.Path) {
		path.MoveTo(f32.Pt(x0, y0))
		path.LineTo(f32.Pt(x1, y1))
	})
}

// ellipse appends an ellipse centred on (cx, cy), rotated by angle radians.
// Reverse winds it the other way, which makes it a hole in a larger shape.
func ellipse(path *clip.Path, cx, cy, rx, ry, angle float32, reverse bool) {
	const k = 0.5522847
	sin, cos := float32(math.Sin(float64(angle))), float32(math.Cos(float64(angle)))
	at := func(u, v float32) f32.Point {
		return f32.Pt(cx+u*cos-v*sin, cy+u*sin+v*cos)
	}
	if !reverse {
		path.MoveTo(at(rx, 0))
		path.CubeTo(at(rx, k*ry), at(k*rx, ry), at(0, ry))
		path.CubeTo(at(-k*rx, ry), at(-rx, k*ry), at(-rx, 0))
		path.CubeTo(at(-rx, -k*ry), at(-k*rx, -ry), at(0, -ry))
		path.CubeTo(at(k*rx, -ry), at(rx, -k*ry), at(rx, 0))
	} else {
		path.MoveTo(at(rx, 0))
		path.CubeTo(at(rx, -k*ry), at(k*rx, -ry), at(0, -ry))
		path.CubeTo(at(-k*rx, -ry), at(-rx, -k*ry), at(-rx, 0))
		path.CubeTo(at(-rx, k*ry), at(-k*rx, ry), at(0, ry))
		path.CubeTo(at(k*rx, ry), at(rx, k*ry), at(rx, 0))
	}
	path.Close()
}

// spline appends a smooth curve through pts (Catmull-Rom converted to cubic
// Béziers), starting with a MoveTo to the first point.
func spline(path *clip.Path, pts []f32.Point) {
	path.MoveTo(pts[0])
	at := func(i int) f32.Point { return pts[min(max(i, 0), len(pts)-1)] }
	for i := 0; i+1 < len(pts); i++ {
		p0, p1, p2, p3 := at(i-1), at(i), at(i+1), at(i+2)
		c1 := f32.Pt(p1.X+(p2.X-p0.X)/6, p1.Y+(p2.Y-p0.Y)/6)
		c2 := f32.Pt(p2.X-(p3.X-p1.X)/6, p2.Y-(p3.Y-p1.Y)/6)
		path.CubeTo(c1, c2, p2)
	}
}

// pts scales a list of (x, y) pairs given in staff spaces to pixels around
// (ox, oy).
func (p pen) pts(ox, oy float32, xy ...float32) []f32.Point {
	out := make([]f32.Point, 0, len(xy)/2)
	for i := 0; i+1 < len(xy); i += 2 {
		out = append(out, f32.Pt(ox+xy[i]*p.sp, oy+xy[i+1]*p.sp))
	}
	return out
}

// Note head kinds.
const (
	headFilled = iota
	headHalf
	headWhole
	headCross
)

// headHalfWidth is half the width of a note head, where stems attach.
func (p pen) headHalfWidth(kind int) float32 {
	if kind == headWhole {
		return 0.72 * p.sp
	}
	return 0.6 * p.sp
}

// head draws a note head centred on (x, y). scale shrinks it (grace notes).
func (p pen) head(x, y float32, kind int, scale float32) {
	sp := p.sp * scale
	switch kind {
	case headWhole:
		p.fill(func(path *clip.Path) {
			ellipse(path, x, y, 0.72*sp, 0.45*sp, 0, false)
			ellipse(path, x, y, 0.27*sp, 0.4*sp, -0.7, true)
		})
	case headHalf:
		p.fill(func(path *clip.Path) {
			ellipse(path, x, y, 0.62*sp, 0.43*sp, -0.38, false)
			ellipse(path, x, y, 0.46*sp, 0.15*sp, -0.62, true)
		})
	case headCross:
		r := 0.46 * sp
		p.stroke(max(1.2, 0.17*sp), func(path *clip.Path) {
			path.MoveTo(f32.Pt(x-r, y-r))
			path.LineTo(f32.Pt(x+r, y+r))
			path.MoveTo(f32.Pt(x-r, y+r))
			path.LineTo(f32.Pt(x+r, y-r))
		})
	default:
		p.fill(func(path *clip.Path) { ellipse(path, x, y, 0.62*sp, 0.43*sp, -0.38, false) })
	}
}

// flag draws one flag at the end of a stem. dir is +1 for a stem going up
// (flag hangs down from its tip) and -1 for a stem going down.
func (p pen) flag(x, yTip, dir float32) {
	sp := p.sp
	// Outline of a curved wedge: down the stem side, out and back.
	p.fill(func(path *clip.Path) {
		path.MoveTo(f32.Pt(x, yTip))
		path.CubeTo(f32.Pt(x+0.1*sp, yTip+dir*0.9*sp), f32.Pt(x+1.15*sp, yTip+dir*1.1*sp), f32.Pt(x+0.85*sp, yTip+dir*2.5*sp))
		path.CubeTo(f32.Pt(x+0.95*sp, yTip+dir*1.5*sp), f32.Pt(x+0.35*sp, yTip+dir*1.3*sp), f32.Pt(x, yTip+dir*1.25*sp))
		path.Close()
	})
}

// dotAt draws an augmentation dot.
func (p pen) dotAt(x, y float32) {
	p.fill(func(path *clip.Path) { ellipse(path, x, y, 0.2*p.sp, 0.2*p.sp, 0, false) })
}

// Rests. yStep(s) gives the y of a staff step.

func (p pen) rest(x float32, value int, yStep func(float32) float32) {
	sp := p.sp
	switch {
	case value <= 1:
		// Whole rest hangs from the fourth line.
		y := yStep(6)
		p.fill(func(path *clip.Path) {
			path.MoveTo(f32.Pt(x-0.55*sp, y))
			path.LineTo(f32.Pt(x+0.55*sp, y))
			path.LineTo(f32.Pt(x+0.55*sp, y+0.5*sp))
			path.LineTo(f32.Pt(x-0.55*sp, y+0.5*sp))
			path.Close()
		})
	case value == 2:
		// Half rest sits on the middle line.
		y := yStep(4)
		p.fill(func(path *clip.Path) {
			path.MoveTo(f32.Pt(x-0.55*sp, y))
			path.LineTo(f32.Pt(x+0.55*sp, y))
			path.LineTo(f32.Pt(x+0.55*sp, y-0.5*sp))
			path.LineTo(f32.Pt(x-0.55*sp, y-0.5*sp))
			path.Close()
		})
	case value == 4:
		y := yStep(4)
		p.stroke(max(1.5, 0.3*sp), func(path *clip.Path) {
			spline(path, p.pts(x, y, 0.25, -1.45, -0.3, -0.75, 0.3, -0.05, -0.25, 0.6))
		})
		p.stroke(max(1.2, 0.22*sp), func(path *clip.Path) {
			spline(path, p.pts(x, y, -0.25, 0.6, 0.35, 0.8, 0.45, 1.05, 0.05, 1.45, -0.2, 1.15))
		})
	default:
		// Eighth and shorter: one curl per flag along a slanted stem.
		flags := flagCount(value)
		y := yStep(5)
		top := y - 0.9*sp
		bottom := y + float32(flags)*0.5*sp + 0.9*sp
		p.line(x+0.45*sp, top, x-0.3*sp, bottom, max(1.2, 0.2*sp))
		for k := range flags {
			yy := top + float32(k)*sp*0.75
			p.fill(func(path *clip.Path) { ellipse(path, x-0.2*sp, yy+0.1*sp, 0.27*sp, 0.27*sp, 0, false) })
			p.stroke(max(1.2, 0.2*sp), func(path *clip.Path) {
				path.MoveTo(f32.Pt(x-0.2*sp, yy+0.1*sp))
				path.CubeTo(f32.Pt(x+0.1*sp, yy+0.4*sp), f32.Pt(x+0.3*sp, yy+0.3*sp), f32.Pt(x+0.5*sp, yy+0.0*sp))
			})
		}
	}
}

// Accidentals, centred on (x, y).

func (p pen) sharp(x, y float32) {
	sp := p.sp
	w := max(1, 0.1*sp)
	p.line(x-0.2*sp, y-1.1*sp, x-0.2*sp, y+1.25*sp, w)
	p.line(x+0.2*sp, y-1.25*sp, x+0.2*sp, y+1.1*sp, w)
	p.stroke(max(1.6, 0.3*sp), func(path *clip.Path) {
		path.MoveTo(f32.Pt(x-0.5*sp, y+0.15*sp))
		path.LineTo(f32.Pt(x+0.5*sp, y-0.35*sp))
		path.MoveTo(f32.Pt(x-0.5*sp, y+0.65*sp))
		path.LineTo(f32.Pt(x+0.5*sp, y+0.15*sp))
	})
}

func (p pen) flat(x, y float32) {
	sp := p.sp
	p.line(x-0.28*sp, y-1.7*sp, x-0.28*sp, y+0.55*sp, max(1, 0.1*sp))
	p.stroke(max(1.4, 0.2*sp), func(path *clip.Path) {
		path.MoveTo(f32.Pt(x-0.28*sp, y+0.55*sp))
		path.CubeTo(f32.Pt(x+0.15*sp, y+0.35*sp), f32.Pt(x+0.55*sp, y+0.1*sp), f32.Pt(x+0.5*sp, y-0.3*sp))
		path.CubeTo(f32.Pt(x+0.45*sp, y-0.75*sp), f32.Pt(x-0.05*sp, y-0.75*sp), f32.Pt(x-0.28*sp, y-0.35*sp))
	})
}

func (p pen) natural(x, y float32) {
	sp := p.sp
	w := max(1, 0.1*sp)
	p.line(x-0.25*sp, y-1.3*sp, x-0.25*sp, y+0.55*sp, w)
	p.line(x+0.25*sp, y-0.55*sp, x+0.25*sp, y+1.3*sp, w)
	p.stroke(max(1.6, 0.28*sp), func(path *clip.Path) {
		path.MoveTo(f32.Pt(x-0.25*sp, y+0.5*sp))
		path.LineTo(f32.Pt(x+0.25*sp, y+0.2*sp))
		path.MoveTo(f32.Pt(x-0.25*sp, y-0.2*sp))
		path.LineTo(f32.Pt(x+0.25*sp, y-0.5*sp))
	})
}

// accidental draws a sharp (+1), flat (-1) or natural (0).
func (p pen) accidental(acc int, x, y float32) {
	switch acc {
	case 1:
		p.sharp(x, y)
	case -1:
		p.flat(x, y)
	default:
		p.natural(x, y)
	}
}

// Clefs. x is the left edge; the y arguments are the line the clef wraps.

// trebleClef draws a G clef curling around the line at yG.
func (p pen) trebleClef(x, yG float32) {
	p.sp *= 1.15
	sp := p.sp
	ox := x + 0.85*sp
	// Upper loop and the bowl around the G line.
	p.stroke(max(2, 0.3*sp), func(path *clip.Path) {
		spline(path, p.pts(ox, yG,
			0.35, -4.5, 0.85, -3.7, 0.6, -2.7, -0.05, -1.7, -0.6, -0.6,
			-0.4, 0.55, 0.3, 1.0, 1.0, 0.5, 0.85, -0.5, 0.15, -0.8,
			-0.25, -0.3, 0, 0.25))
	})
	// Spine with the hook at the bottom.
	p.stroke(max(2, 0.28*sp), func(path *clip.Path) {
		spline(path, p.pts(ox, yG, 0.4, -4.0, 0.3, -1.5, 0.2, 1.2, 0.05, 2.0, -0.4, 2.3, -0.7, 1.85))
	})
	p.fill(func(path *clip.Path) { ellipse(path, ox-0.68*sp, yG+1.8*sp, 0.2*sp, 0.2*sp, 0, false) })
}

// bassClef draws an F clef with its dots around the line at yF.
func (p pen) bassClef(x, yF float32) {
	sp := p.sp
	ox := x + 0.3*sp
	p.fill(func(path *clip.Path) { ellipse(path, ox, yF, 0.3*sp, 0.3*sp, 0, false) })
	p.stroke(max(1.8, 0.28*sp), func(path *clip.Path) {
		spline(path, p.pts(ox, yF, 0, 0, 0.55, -0.7, 1.3, -0.4, 1.45, 0.4, 1.0, 1.3, 0.4, 1.9, -0.15, 2.3))
	})
	p.fill(func(path *clip.Path) { ellipse(path, ox+1.95*sp, yF-0.5*sp, 0.17*sp, 0.17*sp, 0, false) })
	p.fill(func(path *clip.Path) { ellipse(path, ox+1.95*sp, yF+0.5*sp, 0.17*sp, 0.17*sp, 0, false) })
}

// percClef draws the two bars of the neutral (percussion) clef, centred on
// yMid.
func (p pen) percClef(x, yMid float32) {
	sp := p.sp
	w := max(2, 0.45*sp)
	p.line(x+0.5*sp, yMid-1*sp, x+0.5*sp, yMid+1*sp, w)
	p.line(x+1.3*sp, yMid-1*sp, x+1.3*sp, yMid+1*sp, w)
}

// tie draws a slur between two points; dir is +1 to bow downwards and -1 to
// bow upwards.
func (p pen) tie(x0, x1, y, dir float32) {
	sp := p.sp
	h := min(0.9*sp, (x1-x0)*0.3)
	t := 0.2 * sp
	p.fill(func(path *clip.Path) {
		path.MoveTo(f32.Pt(x0, y))
		path.QuadTo(f32.Pt((x0+x1)/2, y+dir*(h*2+t)), f32.Pt(x1, y))
		path.QuadTo(f32.Pt((x0+x1)/2, y+dir*h*2), f32.Pt(x0, y))
		path.Close()
	})
}
