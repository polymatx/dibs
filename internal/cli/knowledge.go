package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/polymatx/dibs/internal/lessons"
)

func cmdNote(args []string) error {
	fs, agent := newFlagSet("note")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return fmt.Errorf("usage: dibs note \"message for other agents\"")
	}
	m, err := manager(*agent)
	if err != nil {
		return err
	}
	if _, err := m.PostNote(strings.Join(pos, " ")); err != nil {
		return err
	}
	fmt.Println(green("✓ note posted"))
	return nil
}

func cmdNotes(args []string) error {
	fs, agent := newFlagSet("notes")
	jsonOut := fs.Bool("json", false, "JSON output")
	if _, err := parseMixed(fs, args); err != nil {
		return err
	}
	m, err := manager(*agent)
	if err != nil {
		return err
	}
	unread, err := m.UnreadNotes()
	if err != nil {
		return err
	}
	all, err := m.Notes()
	if err != nil {
		return err
	}
	if err := m.MarkNotesRead(); err != nil {
		return err
	}
	if *jsonOut {
		return emitJSON(os.Stdout, map[string]any{"unread": unread, "all": all})
	}
	if len(all) == 0 {
		fmt.Println(dim("no notes in the last 7 days"))
		return nil
	}
	isUnread := map[string]bool{}
	for _, n := range unread {
		isUnread[n.ID] = true
	}
	for _, n := range all {
		marker := " "
		line := fmt.Sprintf("[%s] %s: %s", n.CreatedAt.Format("Jan 02 15:04"), bold(n.From), n.Message)
		if isUnread[n.ID] {
			marker = yellow("*")
		} else {
			line = dim(line)
		}
		fmt.Printf("%s %s\n", marker, line)
	}
	return nil
}

func cmdLog(args []string) error {
	fs, agent := newFlagSet("log")
	n := fs.Int("n", 20, "number of events to show")
	jsonOut := fs.Bool("json", false, "JSON output")
	if _, err := parseMixed(fs, args); err != nil {
		return err
	}
	m, err := manager(*agent)
	if err != nil {
		return err
	}
	events, err := m.Journal(*n)
	if err != nil {
		return err
	}
	if *jsonOut {
		return emitJSON(os.Stdout, events)
	}
	if len(events) == 0 {
		fmt.Println(dim("journal is empty"))
		return nil
	}
	for _, e := range events {
		detail := ""
		if p, ok := e.Details["patterns"]; ok {
			detail = fmt.Sprintf(" %v", p)
		}
		if r, ok := e.Details["reason"].(string); ok && r != "" {
			detail += dim(" (" + r + ")")
		}
		if msg, ok := e.Details["message"].(string); ok {
			detail = " " + msg
		}
		fmt.Printf("%s %s %s%s\n", dim(e.At), pad(e.Event, 8), bold(e.Agent), detail)
	}
	return nil
}

func cmdLesson(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: dibs lesson add|list|show|search ...")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "add":
		return cmdLessonAdd(rest)
	case "list":
		return cmdLessonList(rest)
	case "show":
		return cmdLessonShow(rest)
	case "search":
		return cmdLessonSearch(rest)
	default:
		return fmt.Errorf("unknown lesson subcommand %q (want add, list, show or search)", sub)
	}
}

func lessonsDir(agent string) (string, string, error) {
	m, err := manager(agent)
	if err != nil {
		return "", "", err
	}
	return filepath.Join(m.St.Shared, "lessons"), m.Agent, nil
}

func cmdLessonAdd(args []string) error {
	fs, agent := newFlagSet("lesson add")
	body := fs.String("body", "", "lesson body (default: read from stdin)")
	tags := fs.String("tags", "", "comma-separated tags")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return fmt.Errorf("usage: dibs lesson add \"title\" [--body \"...\"] [--tags a,b]  (body may also come from stdin)")
	}
	title := strings.Join(pos, " ")
	text := *body
	if text == "" {
		if st, _ := os.Stdin.Stat(); st != nil && st.Mode()&os.ModeCharDevice == 0 {
			raw, err := io.ReadAll(os.Stdin)
			if err != nil {
				return err
			}
			text = string(raw)
		}
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("lesson body is empty — pass --body or pipe it on stdin")
	}
	dir, who, err := lessonsDir(*agent)
	if err != nil {
		return err
	}
	var tagList []string
	for _, t := range strings.Split(*tags, ",") {
		if t = strings.TrimSpace(t); t != "" {
			tagList = append(tagList, t)
		}
	}
	slug, err := lessons.Add(dir, lessons.Lesson{Title: title, Body: text, Tags: tagList, Agent: who})
	if err != nil {
		return err
	}
	fmt.Printf("%s .dibs/lessons/%s.md %s\n", green("✓ saved"), slug, dim("— commit it to share with your team"))
	return nil
}

func cmdLessonList(args []string) error {
	fs, agent := newFlagSet("lesson list")
	jsonOut := fs.Bool("json", false, "JSON output")
	if _, err := parseMixed(fs, args); err != nil {
		return err
	}
	dir, _, err := lessonsDir(*agent)
	if err != nil {
		return err
	}
	all, err := lessons.List(dir)
	if err != nil {
		return err
	}
	if *jsonOut {
		return emitJSON(os.Stdout, all)
	}
	if len(all) == 0 {
		fmt.Println(dim("no lessons yet — add one with: dibs lesson add \"title\" --body \"...\""))
		return nil
	}
	for _, l := range all {
		line := fmt.Sprintf("%s  %s", dim(l.CreatedAt.Format("2006-01-02")), bold(l.Title))
		if len(l.Tags) > 0 {
			line += dim("  [" + strings.Join(l.Tags, ", ") + "]")
		}
		fmt.Println(line)
		fmt.Println(dim("           .dibs/lessons/" + l.Slug + ".md"))
	}
	return nil
}

func cmdLessonShow(args []string) error {
	fs, agent := newFlagSet("lesson show")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf("usage: dibs lesson show <slug>")
	}
	dir, _, err := lessonsDir(*agent)
	if err != nil {
		return err
	}
	l, err := lessons.Get(dir, pos[0])
	if err != nil {
		return fmt.Errorf("no lesson %q (see: dibs lesson list)", pos[0])
	}
	fmt.Println(bold(l.Title))
	if len(l.Tags) > 0 {
		fmt.Println(dim("tags: " + strings.Join(l.Tags, ", ")))
	}
	fmt.Println()
	fmt.Println(l.Body)
	return nil
}

func cmdLessonSearch(args []string) error {
	fs, agent := newFlagSet("lesson search")
	limit := fs.Int("n", 5, "max results")
	jsonOut := fs.Bool("json", false, "JSON output")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return fmt.Errorf("usage: dibs lesson search <query>")
	}
	dir, _, err := lessonsDir(*agent)
	if err != nil {
		return err
	}
	hits, err := lessons.Search(dir, strings.Join(pos, " "), *limit)
	if err != nil {
		return err
	}
	if *jsonOut {
		return emitJSON(os.Stdout, hits)
	}
	if len(hits) == 0 {
		fmt.Println(dim("no matches"))
		return nil
	}
	for _, h := range hits {
		fmt.Printf("%s %s %s\n", dim(fmt.Sprintf("%5.2f", h.Score)), bold(h.Lesson.Title), dim("["+h.Lesson.Slug+"]"))
		if h.Snippet != "" {
			fmt.Println("       " + h.Snippet)
		}
	}
	return nil
}
