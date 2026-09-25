package ui

import (
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"golang.org/x/exp/shiny/materialdesign/icons"
)

var (
	icOpen       = mustIcon(icons.FileFolderOpen)
	icPlay       = mustIcon(icons.AVPlayArrow)
	icPause      = mustIcon(icons.AVPause)
	icStop       = mustIcon(icons.AVStop)
	icRec        = mustIcon(icons.AVFiberManualRecord)
	icMetro      = mustIcon(icons.AVAVTimer)
	icFollow     = mustIcon(icons.DeviceGPSFixed)
	icMonitor    = mustIcon(icons.HardwareHeadset)
	icReplay     = mustIcon(icons.AVReplay)
	icFolder     = mustIcon(icons.FileFolder)
	icWithSong   = mustIcon(icons.AVQueueMusic)
	icStar       = mustIcon(icons.ToggleStar)
	icStarBorder = mustIcon(icons.ToggleStarBorder)
	icEdit       = mustIcon(icons.EditorModeEdit)
	icDelete     = mustIcon(icons.ActionDelete)
	icCheck      = mustIcon(icons.NavigationCheck)
	icClose      = mustIcon(icons.NavigationClose)
	icVolume     = mustIcon(icons.AVVolumeUp)
	icRemove     = mustIcon(icons.ContentRemove)
	icAdd        = mustIcon(icons.ContentAdd)
	icFlag       = mustIcon(icons.ContentFlag)
)

func mustIcon(data []byte) *widget.Icon {
	ic, err := widget.NewIcon(data)
	if err != nil {
		panic(err)
	}
	return ic
}

// toolIcon is the toolbar-sized icon button (the icon counterpart of button).
func (a *App) toolIcon(gtx C, c *widget.Clickable, ic *widget.Icon, tip string, on bool, onCol, offCol color.NRGBA) D {
	return layout.Inset{Right: 6}.Layout(gtx, func(gtx C) D {
		return a.iconBtn(gtx, c, ic, tip, 20, on, onCol, offCol)
	})
}

// smallIcon is the compact icon button (the icon counterpart of small).
func (a *App) smallIcon(gtx C, c *widget.Clickable, ic *widget.Icon, tip string, on bool, onCol, offCol color.NRGBA) D {
	return layout.Inset{Left: 4}.Layout(gtx, func(gtx C) D {
		return a.iconBtn(gtx, c, ic, tip, 16, on, onCol, offCol)
	})
}

// iconBtn draws an icon button styled like the text buttons, with a tooltip
// on hover. offCol tints the icon while the button is off (zero = colText).
func (a *App) iconBtn(gtx C, c *widget.Clickable, ic *widget.Icon, tip string, size unit.Dp, on bool, onCol, offCol color.NRGBA) D {
	b := material.ButtonLayout(a.th, c)
	b.CornerRadius = 4
	b.Background = colBtn
	fg := colText
	if offCol != (color.NRGBA{}) {
		fg = offCol
	}
	if on {
		b.Background, fg = onCol, colWhite
	}
	d := b.Layout(gtx, func(gtx C) D {
		return layout.UniformInset(6).Layout(gtx, func(gtx C) D {
			s := gtx.Dp(size)
			gtx.Constraints = layout.Exact(image.Pt(s, s))
			return ic.Layout(gtx, fg)
		})
	})
	if c.Hovered() && tip != "" {
		a.tooltip(gtx, d, tip)
	}
	return d
}

// tooltip paints tip just below a widget of dimensions d, above everything else.
func (a *App) tooltip(gtx C, d D, tip string) {
	m := op.Record(gtx.Ops)
	op.Offset(image.Pt(0, d.Size.Y+gtx.Dp(4))).Add(gtx.Ops)
	gtx.Constraints.Min = image.Point{}
	pad := layout.Inset{Top: 3, Bottom: 3, Left: 6, Right: 6}
	lm := op.Record(gtx.Ops)
	ld := pad.Layout(gtx, func(gtx C) D { return a.label(gtx, 12, colText, tip) })
	lc := lm.Stop()
	r := gtx.Dp(3)
	paint.FillShape(gtx.Ops, rgb(0x111316), clip.UniformRRect(image.Rectangle{Max: ld.Size}, r).Op(gtx.Ops))
	lc.Add(gtx.Ops)
	op.Defer(gtx.Ops, m.Stop())
}
