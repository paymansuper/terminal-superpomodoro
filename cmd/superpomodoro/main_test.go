package main

import (
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCycle(t *testing.T) {
	cfg := defaultConfig()
	cfg.Notify, cfg.Bell = false, false
	a := &App{cfg: cfg, round: 1}
	a.setPhase(Focus)
	want := []Phase{Short, Focus, Short, Focus, Short, Focus, Long, Focus}
	for i, w := range want {
		a.advance(true)
		if a.phase != w {
			t.Fatalf("step %d: got %v want %v", i, a.phase, w)
		}
	}
	if a.round != 1 || a.cfg.StatsSessions != 4 || a.cfg.StatsMinutes != 100 {
		t.Fatalf("round=%d sessions=%d minutes=%d", a.round, a.cfg.StatsSessions, a.cfg.StatsMinutes)
	}
	if fmtClock(90*time.Second) != "01:30" {
		t.Fatal("clock format")
	}
}

func TestOSCParseAndContrast(t *testing.T) {
	c, ok := parseOSCColor("\x1b]11;rgb:2b2b/2a2a/6e6e\x1b\\")
	if !ok || c != (rgb{0x2b, 0x2a, 0x6e}) {
		t.Fatalf("parse: %v %v", c, ok)
	}
	if _, ok := parseOSCColor("garbage"); ok {
		t.Fatal("garbage parsed")
	}
	m := adapt(themes[themeIndex("matrix")], c, true)
	if contrast(m.Muted, c) < 4.5 || contrast(m.Text, c) < 7 {
		t.Fatalf("contrast too low: muted %.2f text %.2f", contrast(m.Muted, c), contrast(m.Text, c))
	}
	light := adapt(themes[themeIndex("tokyo-night")], rgb{250, 250, 250}, true)
	if contrast(light.Muted, rgb{250, 250, 250}) < 4.5 {
		t.Fatal("light bg muted")
	}
}

func TestMiniCommands(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	cfg := defaultConfig()
	a := &App{cfg: cfg, round: 1, mini: true}
	a.setPhase(Focus)
	os.MkdirAll(miniDir(), 0o755)
	os.WriteFile(cmdPath(), []byte("start\nskip\n"), 0o644)
	a.pollMiniCommands()
	if a.phase != Short {
		t.Fatalf("skip not applied, phase %v", a.phase)
	}
	data, err := os.ReadFile(statePath())
	if err != nil || !strings.Contains(string(data), `"phase":"SHORT BREAK"`) {
		t.Fatalf("state not written: %s %v", data, err)
	}
	if _, err := os.Stat(cmdPath()); err == nil {
		t.Fatal("cmd file not consumed")
	}
}

func TestThemeFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	os.MkdirAll(themeDir(), 0o755)
	os.WriteFile(filepath.Join(themeDir(), "ocean.theme"), []byte(`
# my theme
extends = "nord"
focus = "#112233, #445566"
break = "#abcdef"   # single color
scene = "waves"
image = "~/x.jpg"
image_brightness = 0.3
`), 0o644)
	os.WriteFile(filepath.Join(themeDir(), "matrix.theme"), []byte(`text = "#ffffff"`), 0o644)
	list, err := loadThemes()
	if err != nil {
		t.Fatal(err)
	}
	o, ok := findTheme(list, "ocean")
	if !ok || o.FocusA != hex("#112233") || o.BreakB != hex("#abcdef") || o.Scene != "waves" ||
		o.ImageBrightness != 0.3 || o.Muted != hex("#4c566a") {
		t.Fatalf("ocean parsed wrong: %+v", o)
	}
	m, _ := findTheme(list, "matrix")
	if m.Text != hex("#ffffff") || m.Scene != "rain" || len(list) != len(builtinThemes)+1 {
		t.Fatalf("override wrong: %+v (%d themes)", m, len(list))
	}
	os.WriteFile(filepath.Join(themeDir(), "bad.theme"), []byte(`focus = "red"`), 0o644)
	if _, err := loadThemes(); err == nil {
		t.Fatal("expected error for bad color")
	}
}

func TestImageLayer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "img.png")
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for x := 0; x < 40; x++ {
		for y := 0; y < 20; y++ {
			img.Set(x, y, color.RGBA{uint8(x * 6), 0, 200, 255})
		}
	}
	f, _ := os.Create(path)
	png.Encode(f, img)
	f.Close()
	var l imageLayer
	cv := newCanvas(30, 10)
	if err := l.draw(cv, imgOpts{Path: path, Bright: 1, Fit: "cover"}); err != nil {
		t.Fatal(err)
	}
	if !cv.cells[0].hasBg || cv.cells[0].c.r > cv.cells[29].c.r || cv.cells[5].c.b < 150 {
		t.Fatalf("unexpected pixels %+v %+v", cv.cells[0], cv.cells[29])
	}
	if err := l.draw(cv, imgOpts{Path: "/nope.png", Bright: 1}); err == nil {
		t.Fatal("expected error")
	}
}

func TestThemegenAPI(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	g := &themegen{token: "secret", stop: make(chan struct{})}
	srv := httptest.NewServer(g.handler())
	defer srv.Close()
	do := func(method, path, body string, token string) (*http.Response, string) {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("X-Token", token)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		return res, string(b)
	}
	if res, _ := do("GET", "/api/state", "", "wrong"); res.StatusCode != 403 {
		t.Fatal("token not enforced")
	}
	if res, _ := do("GET", "/?t=nope", "", ""); res.StatusCode != 403 {
		t.Fatal("page without token served")
	}
	if res, body := do("GET", "/?t=secret", "", ""); res.StatusCode != 200 || !strings.Contains(body, `"secret"`) {
		t.Fatal("page not served with token")
	}
	theme := `{"name":"neon","extends":"synthwave","focus":["#ff00aa","#00ffee"],"break":["#112233","#445566"],
	  "long":["#778899","#aabbcc"],"text":"#ffffff","muted":"#888888","surface":"#222222","scene":"grid","image":"","imageBrightness":0.5}`
	if res, body := do("POST", "/api/save", theme, "secret"); res.StatusCode != 200 {
		t.Fatalf("save failed: %s", body)
	}
	list, err := loadThemes()
	n, ok := findTheme(list, "neon")
	if err != nil || !ok || n.FocusB != hex("#00ffee") || n.Scene != "grid" {
		t.Fatalf("saved theme wrong: %+v %v", n, err)
	}
	if res, _ := do("POST", "/api/save", strings.Replace(theme, "neon", "../evil", 1), "secret"); res.StatusCode != 400 {
		t.Fatal("bad name accepted")
	}
	if res, _ := do("POST", "/api/default", `{"name":"neon"}`, "secret"); res.StatusCode != 200 || loadConfig().Theme != "neon" {
		t.Fatal("default not set")
	}
	if res, _ := do("GET", "/img?t=secret&p=/etc/passwd", "", ""); res.StatusCode != 403 {
		t.Fatal("arbitrary file served")
	}
	if res, _ := do("POST", "/api/delete", `{"name":"neon"}`, "secret"); res.StatusCode != 200 {
		t.Fatal("delete failed")
	}
}

func TestRaiseOnEnd(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "")
	capture := func(a *App) string {
		r, w, _ := os.Pipe()
		old := os.Stdout
		os.Stdout = w
		a.finish()
		w.Close()
		os.Stdout = old
		b, _ := io.ReadAll(r)
		return string(b)
	}
	cfg := defaultConfig()
	cfg.Sound, cfg.Notify = false, false
	a := &App{cfg: cfg, round: 1}
	a.setPhase(Short)
	if out := capture(a); !strings.Contains(out, "\x1b[1t\x1b[5t") {
		t.Fatalf("terminal not raised: %q", out)
	}
	// mini window marked active but its process is gone: fall back to raising
	a.mini, a.floatPID = true, 999999
	if out := capture(a); !strings.Contains(out, "\x1b[5t") || a.mini {
		t.Fatal("no fallback when the mini window died")
	}
	a.cfg.RaiseOnEnd = false
	if out := capture(a); strings.Contains(out, "\x1b[5t") {
		t.Fatal("raised although disabled")
	}
}

func TestThemeExchangeFormat(t *testing.T) {
	src := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", src)
	os.MkdirAll(themeImagesDir(), 0o755)
	imgPath := filepath.Join(themeImagesDir(), "pic.png")
	f, _ := os.Create(imgPath)
	png.Encode(f, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	f.Close()
	os.WriteFile(filepath.Join(themeDir(), "fjord.theme"), []byte(
		"extends = \"nord\"\ndescription = \"calm\"\nauthor = \"P\"\nfocus = \"#010203 #040506\"\nscene = \"waves\"\nimage = \""+imgPath+"\"\nimage_brightness = 0.3\n"), 0o644)
	list, err := loadThemes()
	if err != nil {
		t.Fatal(err)
	}
	ft, _ := findTheme(list, "fjord")
	pkg, err := buildPackage([]themeJSON{toJSON(ft)})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := marshalPackage(pkg)
	if !strings.Contains(string(data), `"format": "superpomodoro-theme"`) || pkg.Themes[0].Colors.Muted != "#4c566a" {
		t.Fatalf("package wrong: %s", data[:200])
	}

	// import on a "different machine"
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	res, err := importThemes(data, "fjord.spomotheme", false)
	if err != nil || len(res.Imported) != 1 || res.Imported[0] != "fjord" {
		t.Fatalf("import failed: %+v %v", res, err)
	}
	list, _ = loadThemes()
	got, _ := findTheme(list, "fjord")
	if got.FocusA != hex("#010203") || got.Scene != "waves" || got.Author != "P" || got.ImageBrightness != 0.3 {
		t.Fatalf("imported theme differs: %+v", got)
	}
	if _, err := os.Stat(got.Image); err != nil || !strings.HasPrefix(got.Image, themeImagesDir()) {
		t.Fatalf("image not restored: %q", got.Image)
	}
	// second import: renamed instead of overwritten
	res, _ = importThemes(data, "x", false)
	if res.Imported[0] != "fjord-2" || res.Renamed["fjord"] != "fjord-2" {
		t.Fatalf("clash not renamed: %+v", res)
	}
	// importing a built-in name is renamed too
	nordPkg, _ := buildPackage([]themeJSON{toJSON(themes[themeIndex("nord")])})
	nd, _ := marshalPackage(nordPkg)
	if res, _ := importThemes(nd, "", false); res.Imported[0] != "nord-2" {
		t.Fatalf("builtin clash: %+v", res)
	}
	// plain .theme files work as well
	if res, err := importThemes([]byte(`focus = "#ffffff"`), "/dl/My Theme.theme", false); err != nil || res.Imported[0] != "my-theme" {
		t.Fatalf("theme file import: %+v %v", res, err)
	}
	for _, bad := range []string{`{"format":"other","version":1,"themes":[]}`, `{"format":"superpomodoro-theme","version":9,"themes":[]}`,
		`{"format":"superpomodoro-theme","version":1,"themes":[{"name":"x","colors":{"focus":["red","#000000"]}}]}`, `focus = "nope"`} {
		if _, err := importThemes([]byte(bad), "bad.theme", false); err == nil {
			t.Fatalf("accepted invalid input %s", bad)
		}
	}
}

func TestSharpImages(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for env, want := range map[string]sharpKind{"ghostty": sharpKitty, "iTerm.app": sharpITerm, "Apple_Terminal": sharpNone} {
		t.Setenv("TERM_PROGRAM", env)
		t.Setenv("TERM", "xterm-256color")
		t.Setenv("TMUX", "")
		t.Setenv("KITTY_WINDOW_ID", "")
		t.Setenv("GHOSTTY_RESOURCES_DIR", "")
		if got := detectSharp("auto"); got != want {
			t.Fatalf("%s: got %v want %v", env, got, want)
		}
	}
	t.Setenv("TERM_PROGRAM", "ghostty")
	if detectSharp("pixel") != sharpNone {
		t.Fatal("image_mode pixel not respected")
	}
	path := filepath.Join(t.TempDir(), "p.png")
	f, _ := os.Create(path)
	png.Encode(f, image.NewRGBA(image.Rect(0, 0, 64, 32)))
	f.Close()

	s := sharpLayer{kind: sharpKitty}
	var img imageLayer
	out := s.update(&img, imgOpts{Path: path, Bright: 0.5, Fit: "cover"}, 80, 24)
	if !strings.Contains(out, "\x1b_Ga=T,f=100,t=f") || !strings.Contains(out, "z=-1073741825") {
		t.Fatalf("kitty placement missing: %q", out)
	}
	if again := s.update(&img, imgOpts{Path: path, Bright: 0.5, Fit: "cover"}, 80, 24); again != "" {
		t.Fatal("re-sent unchanged image")
	}
	if resized := s.update(&img, imgOpts{Path: path, Bright: 0.5, Fit: "cover"}, 100, 30); !strings.Contains(resized, "a=d") || !strings.Contains(resized, "a=T") {
		t.Fatal("resize did not replace the image")
	}
	if off := s.update(&img, imgOpts{}, 100, 30); !strings.Contains(off, "a=d,d=I") {
		t.Fatal("image not removed")
	}
	it := sharpLayer{kind: sharpITerm}
	if out := it.update(&img, imgOpts{Path: path, Bright: 0.5, Fit: "cover"}, 80, 24); !strings.Contains(out, "]1337;SetBackgroundImageFile=") {
		t.Fatal("iTerm2 sequence missing")
	}
	if !strings.HasSuffix(it.clear(), "SetBackgroundImageFile=\x07") {
		t.Fatal("iTerm2 reset missing")
	}
}

func TestImageFitAndTransparency(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	// a wide logo: opaque red bar in the middle, transparent around it
	path := filepath.Join(t.TempDir(), "logo.png")
	img := image.NewNRGBA(image.Rect(0, 0, 100, 20))
	for x := 10; x < 90; x++ {
		for y := 5; y < 15; y++ {
			img.Set(x, y, color.NRGBA{220, 40, 40, 255})
		}
	}
	f, _ := os.Create(path)
	png.Encode(f, img)
	f.Close()

	var l imageLayer
	if l.load(path); !l.hasAlpha || l.resolvedFit(imgOpts{Fit: "auto"}) != "contain" {
		t.Fatal("transparent logo should default to contain")
	}
	cv := newCanvas(40, 20)
	if err := l.draw(cv, imgOpts{Path: path, Bright: 1, Fit: "auto", Scale: 1, Pos: "center"}); err != nil {
		t.Fatal(err)
	}
	// corners stay transparent, the middle shows the red bar
	if cv.cells[0].on || cv.cells[0].hasBg {
		t.Fatal("transparent corner was painted")
	}
	mid := cv.cells[10*40+20]
	if !mid.on || mid.c.r < 150 {
		t.Fatalf("logo not visible in the middle: %+v", mid)
	}
	// the whole logo is visible: left and right parts of the bar are drawn
	if !cv.cells[10*40+6].on || !cv.cells[10*40+33].on {
		t.Fatal("contain cut the logo")
	}
	// cover would crop it
	var c imageLayer
	cv2 := newCanvas(40, 20)
	c.draw(cv2, imgOpts{Path: path, Bright: 1, Fit: "cover"})
	if !cv2.cells[10*40+0].on {
		t.Fatal("cover should fill the width")
	}
	// sharp rendering keeps alpha
	var s imageLayer
	s.load(path)
	file, err := renderSharpFile(&s, imgOpts{Path: path, Bright: 1, Fit: "contain", Scale: 1, Pos: "center"}, 200, 100, "k")
	if err != nil {
		t.Fatal(err)
	}
	rf, _ := os.Open(file)
	out, _ := png.Decode(rf)
	rf.Close()
	if _, _, _, a := out.At(1, 1).RGBA(); a != 0 {
		t.Fatal("sharp image lost transparency")
	}
	if _, _, _, a := out.At(100, 50).RGBA(); a == 0 {
		t.Fatal("sharp image lost the logo")
	}
}
