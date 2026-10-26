package main

// Background images: PNG, JPEG or GIF drawn with half blocks ("▀"), so every
// terminal cell shows two pixels (foreground = top, background = bottom).
// Works in any true color terminal, no graphics protocol needed.

import (
	"errors"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
)

type imageLayer struct {
	path   string
	src    image.Image
	err    error
	w, h   int
	bright float64
	top    []rgb
	bottom []rgb
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

// draw paints the image, scaled to cover the whole screen, onto the canvas.
func (l *imageLayer) draw(cv *canvas, path string, bright float64) error {
	if path != l.path {
		l.path, l.w, l.h = path, 0, 0
		l.src, l.err = decodeImage(path)
	}
	if l.err != nil {
		return l.err
	}
	if cv.w != l.w || cv.h != l.h || bright != l.bright {
		l.resample(cv.w, cv.h, bright)
	}
	for i := range cv.cells {
		cv.cells[i] = cell{ch: '▀', c: l.top[i], on: true, bg: l.bottom[i], hasBg: true}
	}
	return nil
}

// resample scales with "cover" semantics (fill, crop the overflow, keep the center).
func (l *imageLayer) resample(w, h int, bright float64) {
	l.w, l.h, l.bright = w, h, bright
	l.top, l.bottom = make([]rgb, w*h), make([]rgb, w*h)
	b := l.src.Bounds()
	iw, ih := float64(b.Dx()), float64(b.Dy())
	pw, ph := float64(w), float64(h*2) // pixel grid, half blocks are roughly square
	scale := max(pw/iw, ph/ih)
	offX := (iw - pw/scale) / 2
	offY := (ih - ph/scale) / 2
	sample := func(px, py int) rgb {
		x0 := offX + float64(px)/scale
		y0 := offY + float64(py)/scale
		step := 1 / scale // source pixels covered by one target pixel
		var r, g, bl, n float64
		for sy := 0; sy < 3; sy++ {
			for sx := 0; sx < 3; sx++ {
				x := int(x0+step*(float64(sx)+0.5)/3) + b.Min.X
				y := int(y0+step*(float64(sy)+0.5)/3) + b.Min.Y
				c := color.RGBAModel.Convert(l.src.At(x, y)).(color.RGBA)
				r += float64(c.R)
				g += float64(c.G)
				bl += float64(c.B)
				n++
			}
		}
		c := rgb{int(r / n), int(g / n), int(bl / n)}
		return lerp(rgb{0, 0, 0}, c, bright)
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			l.top[y*w+x] = sample(x, y*2)
			l.bottom[y*w+x] = sample(x, y*2+1)
		}
	}
}

// imageSetting resolves which image to show: --image flag, then the theme's own
// image, then the global config. Returns "" when images are off.
func (a *App) imageSetting() (string, float64) {
	if a.imageOff {
		return "", 0
	}
	t := themes[a.theme]
	path, bright := a.cfg.Image, a.cfg.ImageBrightness
	if t.Image != "" {
		path = t.Image
	}
	if t.ImageBrightness > 0 {
		bright = t.ImageBrightness
	}
	if a.imageFlag != "" {
		path = a.imageFlag
	}
	if bright <= 0 || bright > 1 {
		bright = 0.45
	}
	return path, bright
}
