package main

// `superpomodoro themegen` opens a theme editor in the browser.
//
// It is a tiny web server bound to 127.0.0.1 on a random port. Every API call
// needs a random token that only the URL printed/opened here contains, so other
// websites can't talk to it. Saved themes land in the normal theme folder, and a
// running timer reloads them live.

import (
	"context"
	"crypto/rand"
	_ "embed"
	hexenc "encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

//go:embed themegen.html
var themegenHTML string

var themeNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

func themeImagesDir() string { return filepath.Join(themeDir(), "images") }

type themeJSON struct {
	Name            string    `json:"name"`
	Custom          bool      `json:"custom"`
	Builtin         bool      `json:"builtin"` // a built-in theme with this name exists
	Extends         string    `json:"extends,omitempty"`
	Author          string    `json:"author,omitempty"`
	Description     string    `json:"description,omitempty"`
	Focus           [2]string `json:"focus"`
	Break           [2]string `json:"break"`
	Long            [2]string `json:"long"`
	Text            string    `json:"text"`
	Muted           string    `json:"muted"`
	Surface         string    `json:"surface"`
	Scene           string    `json:"scene"`
	Image           string    `json:"image"`
	ImageBrightness float64   `json:"imageBrightness"`
	ImageFit        string    `json:"imageFit,omitempty"`
	ImageScale      float64   `json:"imageScale,omitempty"`
	ImagePosition   string    `json:"imagePosition,omitempty"`
}

func toJSON(t Theme) themeJSON {
	_, builtin := findTheme(builtinThemes, t.Name)
	return themeJSON{
		Name: t.Name, Custom: t.Custom, Builtin: builtin, Extends: t.Extends, Author: t.Author, Description: t.Description,
		Focus: [2]string{t.FocusA.hexStr(), t.FocusB.hexStr()},
		Break: [2]string{t.BreakA.hexStr(), t.BreakB.hexStr()},
		Long:  [2]string{t.LongA.hexStr(), t.LongB.hexStr()},
		Text:  t.Text.hexStr(), Muted: t.Muted.hexStr(), Surface: t.Surface.hexStr(),
		Scene: t.Scene, Image: expandHome(t.Image), ImageBrightness: t.ImageBrightness,
		ImageFit: t.ImageFit, ImageScale: t.ImageScale, ImagePosition: t.ImagePosition,
	}
}

func (j themeJSON) fileContent() (string, error) {
	if !themeNameRe.MatchString(j.Name) {
		return "", errors.New("name: use lowercase letters, digits and dashes")
	}
	colors := []string{j.Focus[0], j.Focus[1], j.Break[0], j.Break[1], j.Long[0], j.Long[1], j.Text, j.Muted, j.Surface}
	for _, c := range colors {
		if !hexColor.MatchString(c) {
			return "", fmt.Errorf("%q is not a #rrggbb color", c)
		}
	}
	switch j.Scene {
	case "rain", "waves", "grid", "off":
	default:
		return "", errors.New("scene must be rain, waves, grid or off")
	}
	if j.ImageBrightness < 0 || j.ImageBrightness > 1 {
		return "", errors.New("image brightness must be between 0 and 1")
	}
	switch j.ImageFit {
	case "", "auto", "cover", "contain":
	default:
		return "", errors.New("image fit must be auto, cover or contain")
	}
	switch j.ImagePosition {
	case "", "top", "center", "bottom":
	default:
		return "", errors.New("image position must be top, center or bottom")
	}
	if j.ImageScale < 0 || j.ImageScale > 1 {
		return "", errors.New("image scale must be between 0 and 1")
	}
	for _, v := range []string{j.Image, j.Author, j.Description} {
		if strings.ContainsAny(v, "\"\n\r") {
			return "", errors.New("text fields can't contain quotes or line breaks")
		}
	}
	if len(j.Author) > 80 || len(j.Description) > 200 {
		return "", errors.New("author or description too long")
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "# SuperPomodoro theme %q, made with superpomodoro themegen\n\n", j.Name)
	if j.Description != "" {
		fmt.Fprintf(&sb, "description = %q\n", j.Description)
	}
	if j.Author != "" {
		fmt.Fprintf(&sb, "author = %q\n", j.Author)
	}
	if j.Description != "" || j.Author != "" {
		sb.WriteString("\n")
	}
	if j.Extends != "" && themeNameRe.MatchString(j.Extends) && j.Extends != j.Name {
		fmt.Fprintf(&sb, "extends = %q\n\n", j.Extends)
	}
	fmt.Fprintf(&sb, "focus   = \"%s %s\"\nbreak   = \"%s %s\"\nlong    = \"%s %s\"\n\n",
		j.Focus[0], j.Focus[1], j.Break[0], j.Break[1], j.Long[0], j.Long[1])
	fmt.Fprintf(&sb, "text    = %q\nmuted   = %q\nsurface = %q\n\nscene   = %q\n", j.Text, j.Muted, j.Surface, j.Scene)
	if j.Image != "" {
		fmt.Fprintf(&sb, "\nimage   = %q\n", j.Image)
		if j.ImageBrightness > 0 {
			fmt.Fprintf(&sb, "image_brightness = %g\n", j.ImageBrightness)
		}
		if j.ImageFit != "" && j.ImageFit != "auto" {
			fmt.Fprintf(&sb, "image_fit = %q\n", j.ImageFit)
		}
		if j.ImageScale > 0 {
			fmt.Fprintf(&sb, "image_scale = %g\n", j.ImageScale)
		}
		if j.ImagePosition != "" && j.ImagePosition != "center" {
			fmt.Fprintf(&sb, "image_position = %q\n", j.ImagePosition)
		}
	}
	return sb.String(), nil
}

func openInOS(target string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", target).Start()
	case "windows":
		return exec.Command("cmd", "/c", "start", "", target).Start()
	default:
		return exec.Command("xdg-open", target).Start()
	}
}

type themegen struct {
	token string
	stop  chan struct{}
}

func (g *themegen) authorized(r *http.Request) bool {
	return r.Header.Get("X-Token") == g.token
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	w.WriteHeader(code)
	writeJSON(w, map[string]string{"error": msg})
}

func listImages() []string {
	entries, _ := os.ReadDir(themeImagesDir())
	out := []string{}
	for _, e := range entries {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if !e.IsDir() && (ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".gif") {
			out = append(out, filepath.Join(themeImagesDir(), e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

func (g *themegen) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" || r.URL.Query().Get("t") != g.token {
			http.Error(w, "open the link printed by superpomodoro themegen", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' blob: data:; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'")
		io.WriteString(w, strings.Replace(themegenHTML, "__TOKEN__", g.token, 1))
	})

	api := func(path, method string, h func(http.ResponseWriter, *http.Request)) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if !g.authorized(r) {
				fail(w, http.StatusForbidden, "bad token")
				return
			}
			if r.Method != method {
				fail(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			h(w, r)
		})
	}

	api("/api/state", http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		list, err := loadThemes()
		out := struct {
			Themes   []themeJSON `json:"themes"`
			Images   []string    `json:"images"`
			Dir      string      `json:"dir"`
			Default  string      `json:"default"`
			Warning  string      `json:"warning,omitempty"`
			Platform string      `json:"platform"`
		}{Images: listImages(), Dir: themeDir(), Default: loadConfig().Theme, Platform: runtime.GOOS}
		for _, t := range list {
			out.Themes = append(out.Themes, toJSON(t))
		}
		if err != nil {
			out.Warning = err.Error()
		}
		writeJSON(w, out)
	})

	api("/api/save", http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		var j themeJSON
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&j); err != nil {
			fail(w, http.StatusBadRequest, "invalid request")
			return
		}
		content, err := j.fileContent()
		if err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		os.MkdirAll(themeDir(), 0o755)
		path := filepath.Join(themeDir(), j.Name+".theme")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		// make sure what we wrote parses back cleanly
		if _, err := parseThemeFile(path, withScenes(builtinThemes)); err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, map[string]string{"path": path})
	})

	api("/api/delete", http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		var j struct{ Name string }
		json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&j)
		if !themeNameRe.MatchString(j.Name) {
			fail(w, http.StatusBadRequest, "invalid name")
			return
		}
		if err := os.Remove(filepath.Join(themeDir(), j.Name+".theme")); err != nil {
			fail(w, http.StatusBadRequest, "no custom theme file with that name")
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})

	api("/api/default", http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		var j struct{ Name string }
		json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&j)
		list, _ := loadThemes()
		if _, ok := findTheme(list, j.Name); !ok {
			fail(w, http.StatusBadRequest, "unknown theme, save it first")
			return
		}
		c := loadConfig()
		c.Theme = j.Name
		saveConfig(c)
		writeJSON(w, map[string]bool{"ok": true})
	})

	api("/api/upload", http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 30<<20)
		file, hdr, err := r.FormFile("image")
		if err != nil {
			fail(w, http.StatusBadRequest, "no image received (max 30 MB)")
			return
		}
		defer file.Close()
		if _, _, err := image.DecodeConfig(file); err != nil {
			fail(w, http.StatusBadRequest, "unsupported image, use png, jpg or gif")
			return
		}
		file.Seek(0, io.SeekStart)
		base := strings.ToLower(strings.TrimSuffix(filepath.Base(hdr.Filename), filepath.Ext(hdr.Filename)))
		base = regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(base, "-")
		base = strings.Trim(base, "-")
		if base == "" {
			base = "image"
		}
		ext := strings.ToLower(filepath.Ext(hdr.Filename))
		if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".gif" {
			ext = ".png"
		}
		os.MkdirAll(themeImagesDir(), 0o755)
		dst := filepath.Join(themeImagesDir(), base+ext)
		for i := 2; ; i++ {
			if _, err := os.Stat(dst); errors.Is(err, os.ErrNotExist) {
				break
			}
			dst = filepath.Join(themeImagesDir(), fmt.Sprintf("%s-%d%s", base, i, ext))
		}
		out, err := os.Create(dst)
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer out.Close()
		if _, err := io.Copy(out, file); err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]string{"path": dst})
	})

	// image previews: only files in the images folder or images used by a theme
	mux.HandleFunc("/img", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") != g.token {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		p := filepath.Clean(r.URL.Query().Get("p"))
		allowed := strings.HasPrefix(p, themeImagesDir()+string(os.PathSeparator))
		if list, _ := loadThemes(); !allowed {
			for _, t := range list {
				if t.Image != "" && filepath.Clean(expandHome(t.Image)) == p {
					allowed = true
				}
			}
		}
		if !allowed {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		http.ServeFile(w, r, p)
	})

	api("/api/export", http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Themes []themeJSON `json:"themes"`
			All    bool        `json:"all"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			fail(w, http.StatusBadRequest, "invalid request")
			return
		}
		sel := req.Themes
		if req.All {
			list, _ := loadThemes()
			for _, t := range list {
				if t.Custom {
					sel = append(sel, toJSON(t))
				}
			}
			if len(sel) == 0 {
				fail(w, http.StatusBadRequest, "you have no custom themes yet")
				return
			}
		}
		for _, j := range sel {
			if _, err := j.fileContent(); err != nil {
				fail(w, http.StatusBadRequest, j.Name+": "+err.Error())
				return
			}
		}
		pkg, err := buildPackage(sel)
		if err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		data, _ := marshalPackage(pkg)
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	})

	api("/api/import", http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(io.LimitReader(r.Body, 64<<20))
		if err != nil {
			fail(w, http.StatusBadRequest, "could not read the file")
			return
		}
		res, err := importThemes(data, r.Header.Get("X-Filename"), r.Header.Get("X-Overwrite") == "1")
		if err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, res)
	})

	api("/api/open", http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		var j struct{ What string }
		json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&j)
		dir := themeDir()
		if j.What == "images" {
			dir = themeImagesDir()
		}
		os.MkdirAll(dir, 0o755)
		if err := openInOS(dir); err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]string{"dir": dir})
	})

	api("/api/quit", http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]bool{"ok": true})
		go func() { time.Sleep(200 * time.Millisecond); close(g.stop) }()
	})

	return mux
}

func themegenCommand(args []string) int {
	noOpen := len(args) > 0 && args[0] == "--no-open"
	buf := make([]byte, 16)
	rand.Read(buf)
	g := &themegen{token: hexenc.EncodeToString(buf), stop: make(chan struct{})}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, "could not start the theme editor:", err)
		return 1
	}
	url := fmt.Sprintf("http://%s/?t=%s", ln.Addr().String(), g.token)
	srv := &http.Server{Handler: g.handler(), ReadHeaderTimeout: 5 * time.Second}
	go srv.Serve(ln)

	fmt.Println("SuperPomodoro theme editor is running at")
	fmt.Println("  " + url)
	fmt.Println("Themes are saved to", themeDir())
	fmt.Println("Press Ctrl+C or the Quit button in the editor to stop.")
	if !noOpen {
		openInOS(url)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	select {
	case <-sig:
	case <-g.stop:
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	fmt.Println("theme editor stopped")
	return 0
}
