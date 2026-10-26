package main

// To-do list, stored as a plain Markdown checklist so it can be edited anywhere:
//
//   - [ ] Write investor update <!-- pomodoros: 2, current -->
//   - [x] Review NDA
//
// The comment keeps the pomodoro count and which task is the current focus.
// Changes made in another editor are picked up while the timer runs.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type todo struct {
	Text    string
	Done    bool
	Pomos   int
	Current bool
}

func todoPath() string { return filepath.Join(filepath.Dir(configPath()), "todos.md") }

var todoLine = regexp.MustCompile(`^\s*[-*]\s+\[([ xX])\]\s+(.*?)\s*(?:<!--(.*?)-->)?\s*$`)

func parseTodos(data string) []todo {
	var out []todo
	sc := bufio.NewScanner(strings.NewReader(data))
	for sc.Scan() {
		m := todoLine.FindStringSubmatch(sc.Text())
		if m == nil || strings.TrimSpace(m[2]) == "" {
			continue
		}
		t := todo{Text: strings.TrimSpace(m[2]), Done: m[1] != " "}
		for _, part := range strings.Split(m[3], ",") {
			part = strings.TrimSpace(part)
			switch {
			case part == "current":
				t.Current = true
			case strings.HasPrefix(part, "pomodoros:"):
				t.Pomos, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(part, "pomodoros:")))
			}
		}
		out = append(out, t)
	}
	return out
}

func formatTodos(list []todo) string {
	var sb strings.Builder
	sb.WriteString("# SuperPomodoro to-do list\n\n")
	for _, t := range list {
		box := " "
		if t.Done {
			box = "x"
		}
		var meta []string
		if t.Pomos > 0 {
			meta = append(meta, fmt.Sprintf("pomodoros: %d", t.Pomos))
		}
		if t.Current {
			meta = append(meta, "current")
		}
		text := strings.ReplaceAll(t.Text, "\n", " ")
		if len(meta) > 0 {
			fmt.Fprintf(&sb, "- [%s] %s <!-- %s -->\n", box, text, strings.Join(meta, ", "))
		} else {
			fmt.Fprintf(&sb, "- [%s] %s\n", box, text)
		}
	}
	return sb.String()
}

func loadTodos() []todo {
	data, err := os.ReadFile(todoPath())
	if err != nil {
		return nil
	}
	return parseTodos(string(data))
}

func saveTodos(list []todo) error {
	os.MkdirAll(filepath.Dir(todoPath()), 0o755)
	return os.WriteFile(todoPath(), []byte(formatTodos(list)), 0o644)
}

func todoStamp() string {
	st, err := os.Stat(todoPath())
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d:%d", st.ModTime().UnixNano(), st.Size())
}

// ---------- in the app ----------

func (a *App) saveTodos() {
	if err := saveTodos(a.todos); err != nil {
		a.setFlash("could not save to-dos: " + err.Error())
	}
	a.todoStamp = todoStamp()
}

func (a *App) reloadTodosIfChanged() {
	if time.Since(a.todoCheck) < time.Second {
		return
	}
	a.todoCheck = time.Now()
	if s := todoStamp(); s != a.todoStamp {
		a.todoStamp = s
		a.todos = loadTodos()
		a.todoSel = min(a.todoSel, max(len(a.todos)-1, 0))
	}
}

func (a *App) currentTask() *todo {
	for i := range a.todos {
		if a.todos[i].Current && !a.todos[i].Done {
			return &a.todos[i]
		}
	}
	return nil
}

func (a *App) setCurrent(i int) {
	was := a.todos[i].Current
	for j := range a.todos {
		a.todos[j].Current = false
	}
	a.todos[i].Current = !was && !a.todos[i].Done
	a.saveTodos()
	if a.todos[i].Current {
		a.setFlash("focusing on · " + a.todos[i].Text)
	}
}

// creditPomodoro counts a finished focus phase for the current task.
func (a *App) creditPomodoro() {
	if t := a.currentTask(); t != nil {
		t.Pomos++
		a.saveTodos()
	}
}

// ---------- command line: superpomodoro todo ... ----------

func todoCommand(args []string) int {
	list := loadTodos()
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	index := func() (int, bool) {
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "which one? give the number from: superpomodoro todo")
			return 0, false
		}
		n, err := strconv.Atoi(args[1])
		if err != nil || n < 1 || n > len(list) {
			fmt.Fprintf(os.Stderr, "no to-do number %s\n", args[1])
			return 0, false
		}
		return n - 1, true
	}
	switch sub {
	case "", "list", "ls":
		if len(list) == 0 {
			fmt.Println("no to-dos yet, add one with: superpomodoro todo add <text>")
		}
		for i, t := range list {
			box := "[ ]"
			if t.Done {
				box = "[x]"
			}
			extra := ""
			if t.Current {
				extra += "  ← current"
			}
			if t.Pomos > 0 {
				extra += fmt.Sprintf("  (%d pomodoros)", t.Pomos)
			}
			fmt.Printf("%2d %s %s%s\n", i+1, box, t.Text, extra)
		}
		return 0
	case "add", "a":
		text := strings.TrimSpace(strings.Join(args[1:], " "))
		if text == "" {
			fmt.Fprintln(os.Stderr, "usage: superpomodoro todo add <text>")
			return 2
		}
		list = append(list, todo{Text: text})
		fmt.Printf("added %d %s\n", len(list), text)
	case "done", "x":
		i, ok := index()
		if !ok {
			return 2
		}
		list[i].Done, list[i].Current = !list[i].Done, false
	case "focus", "f":
		i, ok := index()
		if !ok {
			return 2
		}
		for j := range list {
			list[j].Current = j == i && !list[i].Done
		}
	case "rm", "delete", "del":
		i, ok := index()
		if !ok {
			return 2
		}
		list = append(list[:i], list[i+1:]...)
	case "clear":
		var keep []todo
		for _, t := range list {
			if !t.Done {
				keep = append(keep, t)
			}
		}
		fmt.Printf("removed %d done to-do(s)\n", len(list)-len(keep))
		list = keep
	case "path":
		fmt.Println(todoPath())
		return 0
	default:
		fmt.Fprintln(os.Stderr, "usage: superpomodoro todo [list|add <text>|done <n>|focus <n>|rm <n>|clear|path]")
		return 2
	}
	if err := saveTodos(list); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
