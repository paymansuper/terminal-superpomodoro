package main

// Background images: PNG, JPEG or GIF.
//
// Two ways to place a picture:
//   cover    fill the whole window, crop what sticks out (photos)
//   contain  show the whole picture, scaled by image_scale, at image_position
//            (logos and anything with transparency)
// "auto" picks contain for pictures with transparent pixels, cover otherwise.
//
// This file draws the pixel style with half blocks ("▀"/"▄"), two pixels per
// cell, in any true color terminal. sharp.go shows real pictures where possible.

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
)

type imgOpts struct {
	Path   string
	Bright float64 // 0..1
	Fit    string  // auto, cover, contain
	Scale  float64 // contain only, 0.1..1
	Pos    string  // contain only: top, center, bottom
}

func (o imgOpts) key() string {
	return fmt.Sprintf("%s|%s|%s|%.3f|%.3f", o.Path, o.Fit, o.Pos, o.Bright, o.Scale)
}

type imageLayer struct {
	path     string
	src      image.Image
	hasAlpha bool
	err      error
	w, h     int
	key      string
	top      []rgba8
	bottom   []rgba8
}

type rgba8 struct {
	c  rgb
	on bool // opaque enough to draw
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

func decodeImage(path string) (image.Image, error) {
	f, err := os.Open(expandHome(path))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("file not found")
		}
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, errors.New("unsupported image, use png, jpg or gif")
	}
	return img, nil
}

// hasTransparency checks a grid of pixels for anything not fully opaque.
func hasTransparency(img image.Image) bool {
	if o, ok := img.(interface{ Opaque() bool }); ok && o.Opaque() {
		return false
	}
	b := img.Bounds()
	for y := 0; y < 48; y++ {
		for x := 0; x < 48; x++ {
			px := b.Min.X + x*(b.Dx()-1)/47
			py := b.Min.Y + y*(b.Dy()-1)/47
			if _, _, _, a := img.At(px, py).RGBA(); a < 0xf000 {
				return true
			}
		}
	}
	return false
}

// load makes sure the source picture for path is decoded.
func (l *imageLayer) load(path string) error {
	if path != l.path || (l.src == nil && l.err == nil) {
		l.path, l.key = path, ""
		l.src, l.err = decodeImage(path)
		if l.err == nil {
			l.hasAlpha = hasTransparency(l.src)
		}
	}
	return l.err
}

// resolvedFit turns "auto" into cover or contain for the loaded picture.
func (l *imageLayer) resolvedFit(o imgOpts) string {
	switch o.Fit {
	case "cover", "contain":
		return o.Fit
	}
	if l.hasAlpha {
		return "contain"
	}
	return "cover"
}

// placement maps target pixels to source pixels: src = (target - offset) / scale.
func placement(iw, ih, w, h float64, fit string, o imgOpts) (scale, offX, offY float64) {
	if fit == "contain" {
		s := o.Scale
		if s <= 0 || s > 1 {
			s = 1
		}
		scale = min(w/iw, h/ih) * s
		offX = (w - iw*scale) / 2
		switch o.Pos {
		case "top":
			offY = h * 0.06
		case "bottom":
			offY = h - ih*scale - h*0.06
		default:
			offY = (h - ih*scale) / 2
		}
		return
	}
	scale = max(w/iw, h/ih)
	return scale, (w - iw*scale) / 2, (h - ih*scale) / 2
}

// sampleArea averages the source area behind one target pixel.
// It returns the color and the coverage (0 = transparent).
func sampleArea(src image.Image, x0, y0, step float64) (rgb, float64) {
	b := src.Bounds()
	var r, g, bl, a, n float64
	const k = 3
	for sy := 0; sy < k; sy++ {
		for sx := 0; sx < k; sx++ {
			x := int(x0 + step*(float64(sx)+0.5)/k)
			y := int(y0 + step*(float64(sy)+0.5)/k)
			n++
			if x < 0 || y < 0 || x >= b.Dx() || y >= b.Dy() {
				continue
			}
			c := color.NRGBAModel.Convert(src.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			af := float64(c.A) / 255
			r += float64(c.R) * af
			g += float64(c.G) * af
			bl += float64(c.B) * af
			a += af
		}
	}
	if a == 0 {
		return rgb{}, 0
	}
	return rgb{int(r / a), int(g / a), int(bl / a)}, a / n
}

// sampleBilinear interpolates between the four nearest source pixels (enlarging).
func sampleBilinear(src image.Image, x, y float64) (rgb, float64) {
	b := src.Bounds()
	x, y = x-0.5, y-0.5
	x0, y0 := int(x), int(y)
	if x < 0 {
		x0 = -1
	}
	if y < 0 {
		y0 = -1
	}
	fx, fy := x-float64(x0), y-float64(y0)
	var r, g, bl, a float64
	for dy := 0; dy < 2; dy++ {
		for dx := 0; dx < 2; dx++ {
			px, py := x0+dx, y0+dy
			w := (1 - abs(float64(dx)-fx)) * (1 - abs(float64(dy)-fy))
			if px < 0 || py < 0 || px >= b.Dx() || py >= b.Dy() || w <= 0 {
				continue
			}
			c := color.NRGBAModel.Convert(src.At(b.Min.X+px, b.Min.Y+py)).(color.NRGBA)
			af := float64(c.A) / 255 * w
			r += float64(c.R) * af
			g += float64(c.G) * af
			bl += float64(c.B) * af
			a += af
		}
	}
	if a == 0 {
		return rgb{}, 0
	}
	return rgb{int(r / a), int(g / a), int(bl / a)}, min(a, 1)
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// draw paints the picture onto the canvas.
func (l *imageLayer) draw(cv *canvas, o imgOpts) error {
	if err := l.load(o.Path); err != nil {
		return err
	}
	if cv.w != l.w || cv.h != l.h || o.key() != l.key {
		l.resample(cv.w, cv.h, o)
	}
	for i := range cv.cells {
		t, b := l.top[i], l.bottom[i]
		switch {
		case t.on && b.on:
			cv.cells[i] = cell{ch: '▀', c: t.c, on: true, bg: b.c, hasBg: true}
		case t.on:
			cv.cells[i] = cell{ch: '▀', c: t.c, on: true}
		case b.on:
			cv.cells[i] = cell{ch: '▄', c: b.c, on: true}
		}
	}
	return nil
}

func (l *imageLayer) resample(w, h int, o imgOpts) {
	l.w, l.h, l.key = w, h, o.key()
	l.top, l.bottom = make([]rgba8, w*h), make([]rgba8, w*h)
	b := l.src.Bounds()
	iw, ih := float64(b.Dx()), float64(b.Dy())
	pw, ph := float64(w), float64(h*2) // half blocks are roughly square pixels
	scale, offX, offY := placement(iw, ih, pw, ph, l.resolvedFit(o), o)
	px := func(x, y int) rgba8 {
		c, cov := sampleArea(l.src, (float64(x)-offX)/scale, (float64(y)-offY)/scale, 1/scale)
		if cov < 0.5 {
			return rgba8{}
		}
		return rgba8{lerp(rgb{0, 0, 0}, c, o.Bright), true}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			l.top[y*w+x] = px(x, y*2)
			l.bottom[y*w+x] = px(x, y*2+1)
		}
	}
}

// imageOptions resolves which image to show and how: --image flag, then the
// theme's own image, then the global config. Path is "" when images are off.
func (a *App) imageOptions() imgOpts {
	if a.imageOff {
		return imgOpts{}
	}
	t := themes[a.theme]
	o := imgOpts{Path: a.cfg.Image, Bright: a.cfg.ImageBrightness, Fit: a.cfg.ImageFit,
		Scale: a.cfg.ImageScale, Pos: a.cfg.ImagePosition}
	if t.Image != "" {
		o.Path = t.Image
	}
	if t.ImageBrightness > 0 {
		o.Bright = t.ImageBrightness
	}
	if t.ImageFit != "" {
		o.Fit = t.ImageFit
	}
	if t.ImageScale > 0 {
		o.Scale = t.ImageScale
	}
	if t.ImagePosition != "" {
		o.Pos = t.ImagePosition
	}
	if a.imageFlag != "" {
		o.Path = a.imageFlag
	}
	if o.Bright <= 0 || o.Bright > 1 {
		o.Bright = 0.45
	}
	if o.Scale <= 0 || o.Scale > 1 {
		o.Scale = 0.8
	}
	if o.Fit == "" {
		o.Fit = "auto"
	}
	if o.Pos == "" {
		o.Pos = "center"
	}
	return o
}

// imageSetting is the short form used where only path and brightness matter.
func (a *App) imageSetting() (string, float64) {
	o := a.imageOptions()
	return o.Path, o.Bright
}
