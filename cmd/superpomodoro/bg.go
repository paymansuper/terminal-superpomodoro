package main

import (
	"math"
	"math/rand"
)

// ---------- canvas ----------

type cell struct {
	ch    rune
	c     rgb
	bold  bool
	on    bool // has a foreground character
	bg    rgb
	hasBg bool // cell background color (used by image backgrounds)
}

type canvas struct {
	w, h  int
	cells []cell
}

func newCanvas(w, h int) *canvas { return &canvas{w, h, make([]cell, w*h)} }

func (cv *canvas) set(x, y int, ch rune, c rgb) {
	if x < 0 || y < 0 || x >= cv.w || y >= cv.h {
		return
	}
	old := cv.cells[y*cv.w+x]
	cv.cells[y*cv.w+x] = cell{ch: ch, c: c, on: true, bg: old.bg, hasBg: old.hasBg}
}

func (cv *canvas) clear(x, y int) {
	if x < 0 || y < 0 || x >= cv.w || y >= cv.h {
		return
	}
	old := cv.cells[y*cv.w+x]
	if old.hasBg {
		// over an image: keep a darkened, flat version of it so text stays readable
		avg := lerp(old.c, old.bg, 0.5)
		if !old.on {
			avg = old.bg
		}
		cv.cells[y*cv.w+x] = cell{bg: lerp(avg, rgb{0, 0, 0}, 0.6), hasBg: true}
		return
	}
	cv.cells[y*cv.w+x] = cell{}
}

// ---------- effects ----------
//
// Only three themes get a scene; everything else is styled purely by color.

var effectNames = []string{"auto", "off", "rain", "waves", "grid"}

var themeEffect = map[string]string{
	"matrix":    "rain",
	"kanagawa":  "waves",
	"synthwave": "grid",
}

func effectIndex(name string) int {
	for i, n := range effectNames {
		if n == name {
			return i
		}
	}
	return 0
}

// layout tells the background where the UI sits, so scenes can be composed around it.
type layout struct {
	clockTop, clockBottom int     // rows occupied by the big digits
	horizon               int     // preferred horizon row (just below the progress bar)
	progress              float64 // 0..1 of the current phase
}

type particle struct {
	x, y, vy, phase, speed float64
	ln                     int
	glyphs                 []rune
}

type background struct {
	name  string
	w, h  int
	parts []particle
	ridge []float64 // synthwave mountain silhouette
	t     float64
}

var katakana = []rune("ｱｲｳｴｵｶｷｸｹｺｻｼｽｾｿﾀﾁﾂﾃﾄﾅﾆﾇﾈﾉﾊﾋﾌﾍﾎﾏﾐﾑﾒﾓﾔﾕﾖﾗﾘﾙﾚﾛﾜﾝ0123456789Z:.=*+-<>")

func randGlyph() rune { return katakana[rand.Intn(len(katakana))] }

func (b *background) reset(name string, w, h int) {
	b.name, b.w, b.h, b.parts, b.ridge = name, w, h, nil, nil
	switch name {
	case "rain":
		for x := 0; x < w; x += 2 {
			b.parts = append(b.parts, b.newDrop(float64(x), true))
		}
	case "grid":
		for i := 0; i < w*h/140; i++ {
			b.parts = append(b.parts, particle{x: rand.Float64() * float64(w), y: rand.Float64() * float64(h),
				phase: rand.Float64() * 6.28, speed: 0.4 + rand.Float64()*1.2})
		}
		b.ridge = make([]float64, w)
		p1, p2 := rand.Float64()*6, rand.Float64()*6
		for x := range b.ridge {
			fx := float64(x)
			v := 1.6*math.Sin(fx*0.045+p1) + 1.1*math.Sin(fx*0.11+p2) + 0.5*math.Sin(fx*0.27)
			b.ridge[x] = math.Max(0, v+1.2)
		}
	}
}

func (b *background) newDrop(x float64, scatter bool) particle {
	p := particle{x: x, vy: 7 + rand.Float64()*16, ln: 6 + rand.Intn(18)}
	p.y = -float64(rand.Intn(b.h + 1))
	if scatter {
		p.y = rand.Float64()*float64(b.h*2) - float64(b.h)
	}
	p.glyphs = make([]rune, p.ln)
	for i := range p.glyphs {
		p.glyphs[i] = randGlyph()
	}
	return p
}

func (b *background) update(dt float64) {
	b.t += dt
	if b.name != "rain" {
		return
	}
	for i := range b.parts {
		p := &b.parts[i]
		p.y += p.vy * dt
		if rand.Float64() < 0.08 {
			p.glyphs[rand.Intn(len(p.glyphs))] = randGlyph()
		}
		if p.y-float64(p.ln) > float64(b.h) {
			*p = b.newDrop(p.x, false)
		}
	}
}

// draw paints the effect; dim interpolates from the theme surface toward the color.
func (b *background) draw(cv *canvas, th Theme, ca, cb rgb, lo layout) {
	dim := func(c rgb, f float64) rgb { return lerp(th.Base, c, f) }
	switch b.name {
	case "rain":
		for _, p := range b.parts {
			head := int(p.y)
			for k := 0; k < p.ln; k++ {
				y := head - k
				if y < 0 || y >= b.h {
					continue
				}
				f := 1 - float64(k)/float64(p.ln)
				c := dim(ca, 0.15+0.55*f)
				if k == 0 {
					c = dim(th.Text, 0.85)
				}
				cv.set(int(p.x), y, p.glyphs[k%len(p.glyphs)], c)
			}
		}
	case "waves":
		base := float64(b.h) * 0.7
		for x := 0; x < b.w; x++ {
			fx := float64(x)
			hy := base + 1.6*math.Sin(fx*0.11+b.t*1.3) + 0.9*math.Sin(fx*0.29-b.t*0.8)
			for y := int(hy); y < b.h; y++ {
				d := float64(y) - hy
				n := math.Sin(fx*0.21+float64(y)*0.9-b.t*1.6) + 0.6*math.Sin(fx*0.07+b.t*0.5+float64(y)*0.3)
				depth := 1 - math.Min(d/float64(b.h-int(base)+2), 1)*0.7
				switch {
				case d < 1:
					ch := '~'
					if n > 1.1 {
						ch = '°'
					}
					cv.set(x, y, ch, dim(lerp(cb, th.Text, 0.4), 0.85))
				case n > 0.7:
					cv.set(x, y, '≈', dim(ca, 0.55*depth))
				case n > -0.2:
					cv.set(x, y, '~', dim(lerp(ca, cb, 0.5), 0.35*depth))
				case n > -0.9:
					cv.set(x, y, '-', dim(cb, 0.22*depth))
				}
			}
		}
	case "grid":
		b.drawSynthwave(cv, th, dim, lo)
	}
}

// drawSynthwave: a striped sun behind the digits that sets as the phase runs out,
// a mountain ridge on the horizon and a calm perspective floor scrolling toward you.
func (b *background) drawSynthwave(cv *canvas, th Theme, dim func(rgb, float64) rgb, lo layout) {
	hz := lo.horizon
	if hz <= 2 || hz >= b.h {
		hz = int(float64(b.h) * 0.7)
	}
	cx := float64(b.w) / 2

	// a few faint stars in the sky
	for _, p := range b.parts {
		if int(p.y) >= hz-4 {
			continue
		}
		v := (math.Sin(b.t*p.speed+p.phase) + 1) / 2
		cv.set(int(p.x), int(p.y), '·', dim(th.Text, 0.1+0.3*v))
	}

	// sun: centered behind the clock, sinking into the horizon with progress
	r := float64(lo.clockBottom-lo.clockTop)*0.75 + 3
	r = math.Min(r, float64(hz)*0.9)
	sunBase := float64(hz) + lo.progress*r*0.95 // the sun's equator line, moves down
	for y := 0; y < hz; y++ {
		dy := sunBase - float64(y) - 0.5
		if dy < 0 || dy > r {
			continue
		}
		rel := dy / r // 1 at the top of the sun
		k := int(sunBase) - y
		if (rel < 0.6 && k%3 == 1) || (rel < 0.3 && k%2 == 0) {
			continue // the classic stripes, tighter near the bottom
		}
		half := math.Sqrt(math.Max(r*r-dy*dy, 0)) * 2.1
		col := lerp(th.FocusA, th.FocusB, rel)
		// half blocks give the sun a scanline texture that never reads like a digit
		for x := int(cx - half); x <= int(cx+half); x++ {
			cv.set(x, y, '▀', dim(col, 0.4))
		}
	}

	// mountain ridge in front of the sun
	blocks := []rune(" ▁▂▃▄▅▆▇█")
	ridgeCol := dim(th.LongA, 0.35)
	for x := 0; x < b.w && x < len(b.ridge); x++ {
		hgt := b.ridge[x] * 0.5
		if math.Abs(float64(x)-cx) < r*1.6 {
			hgt *= math.Abs(float64(x)-cx) / (r * 1.6) // keep a valley for the sun
		}
		for k := 0; float64(k) < hgt; k++ {
			frac := math.Min(hgt-float64(k), 1)
			cv.set(x, hz-1-k, blocks[int(frac*8)], ridgeCol)
		}
	}

	// horizon
	for x := 0; x < b.w; x++ {
		cv.set(x, hz, '━', dim(th.FocusA, 0.85))
	}

	// floor: horizontal lines travel toward the viewer, verticals converge at the sun
	if b.h-hz < 3 {
		return
	}
	depthK := float64(b.h-hz) * 2.2
	off := math.Mod(b.t*0.6, 1)
	for y := hz + 1; y < b.h; y++ {
		d := float64(y - hz)
		near := math.Min(d/float64(b.h-hz), 1)
		zA, zB := depthK/d, depthK/(d+1)
		hline := d >= 2 && math.Floor(zA/2+off) != math.Floor(zB/2+off)
		for x := 0; x < b.w; x++ {
			fx := float64(x)
			const g = 5.0
			vline := math.Floor((fx-cx)/d/g) != math.Floor((fx+1-cx)/d/g)
			switch {
			case vline:
				ch := '│'
				if fx < cx-d*1.5 {
					ch = '╱'
				} else if fx > cx+d*1.5 {
					ch = '╲'
				}
				cv.set(x, y, ch, dim(th.LongA, 0.2+0.5*near))
			case hline:
				cv.set(x, y, '─', dim(th.FocusA, 0.15+0.55*near))
			}
		}
	}
}
