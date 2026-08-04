// Package lessons stores what agents learn as plain markdown files under
// .dibs/lessons/ — committed, diffed, and reviewed like any other file in
// the repository. Memory that travels with the repo: your team and your CI
// agents get it through `git pull`, not through a database.
//
// Search is classic BM25 over title, tags, and body. For the few hundred
// lessons a real repository accumulates, a lexical index built on demand is
// instant, deterministic, and needs no embedding model, no vector store,
// and no background process.
package lessons

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// Lesson is one piece of durable knowledge.
type Lesson struct {
	Slug      string    `yaml:"-" json:"slug"`
	Title     string    `yaml:"title" json:"title"`
	Tags      []string  `yaml:"tags,omitempty" json:"tags,omitempty"`
	Agent     string    `yaml:"agent,omitempty" json:"agent,omitempty"`
	CreatedAt time.Time `yaml:"created" json:"created_at"`
	Body      string    `yaml:"-" json:"body"`
}

// Hit is a search result.
type Hit struct {
	Lesson  Lesson  `json:"lesson"`
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet,omitempty"`
}

// Add writes a lesson to dir and returns its slug.
func Add(dir string, l Lesson) (string, error) {
	if strings.TrimSpace(l.Title) == "" {
		return "", fmt.Errorf("lesson needs a title")
	}
	if l.CreatedAt.IsZero() {
		l.CreatedAt = time.Now()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	fm, err := yaml.Marshal(l)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(fm)
	buf.WriteString("---\n\n")
	buf.WriteString(strings.TrimSpace(l.Body))
	buf.WriteString("\n")

	// O_EXCL makes slug collision handling race-free without a lock: two
	// agents adding same-titled lessons concurrently get distinct files.
	slug := Slugify(l.Title)
	for i := 1; i <= 99; i++ {
		name := slug
		if i > 1 {
			name = fmt.Sprintf("%s-%d", slug, i)
		}
		f, err := os.OpenFile(filepath.Join(dir, name+".md"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := f.Write(buf.Bytes())
		cerr := f.Close()
		if werr != nil {
			return "", werr
		}
		if cerr != nil {
			return "", cerr
		}
		return name, nil
	}
	return "", fmt.Errorf("too many lessons titled %q", l.Title)
}

// List reads every lesson in dir, newest first.
func List(dir string) ([]Lesson, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Lesson
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		l, err := parseFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue // a malformed lesson should not break the library
		}
		out = append(out, l)
	}
	slices.SortFunc(out, func(a, b Lesson) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return out, nil
}

// Get returns one lesson by slug.
func Get(dir, slug string) (Lesson, error) {
	return parseFile(filepath.Join(dir, slug+".md"))
}

// Search ranks lessons against query with BM25 and returns the top k hits.
func Search(dir, query string, k int) ([]Hit, error) {
	docs, err := List(dir)
	if err != nil {
		return nil, err
	}
	qTerms := tokenize(query)
	if len(docs) == 0 || len(qTerms) == 0 {
		return nil, nil
	}
	if k <= 0 {
		k = 5
	}

	// Weighted term frequencies: a title match should beat a body mention.
	freqs := make([]map[string]float64, len(docs))
	lengths := make([]float64, len(docs))
	var totalLen float64
	for i, d := range docs {
		tf := map[string]float64{}
		add := func(text string, w float64) {
			for _, t := range tokenize(text) {
				tf[t] += w
				lengths[i] += w
			}
		}
		add(d.Title, 3)
		add(strings.Join(d.Tags, " "), 2)
		add(d.Body, 1)
		freqs[i] = tf
		totalLen += lengths[i]
	}
	avgLen := totalLen / float64(len(docs))

	df := map[string]int{}
	for _, tf := range freqs {
		for _, t := range qTerms {
			if tf[t] > 0 {
				df[t]++
			}
		}
	}

	const k1, b = 1.2, 0.75
	n := float64(len(docs))
	var hits []Hit
	for i, d := range docs {
		var score float64
		for _, t := range qTerms {
			tf := freqs[i][t]
			if tf == 0 {
				continue
			}
			idf := math.Log(1 + (n-float64(df[t])+0.5)/(float64(df[t])+0.5))
			score += idf * (tf * (k1 + 1)) / (tf + k1*(1-b+b*lengths[i]/avgLen))
		}
		if score > 0 {
			hits = append(hits, Hit{Lesson: d, Score: score, Snippet: snippet(d.Body, qTerms)})
		}
	}
	sort.Slice(hits, func(a, z int) bool { return hits[a].Score > hits[z].Score })
	if len(hits) > k {
		hits = hits[:k]
	}
	return hits, nil
}

var nonWord = regexp.MustCompile(`[^a-z0-9]+`)

func tokenize(s string) []string {
	parts := nonWord.Split(strings.ToLower(s), -1)
	out := parts[:0]
	for _, p := range parts {
		if len(p) > 1 {
			out = append(out, stem(p))
		}
	}
	return out
}

// stem strips the few English suffixes that actually matter for matching
// ("webhooks"→"webhook", "retries"→"retry", "panics"→"panic"). A crude
// stemmer beats no stemmer, and a real one isn't worth a dependency here.
func stem(t string) string {
	switch {
	case len(t) > 4 && strings.HasSuffix(t, "ies"):
		return t[:len(t)-3] + "y"
	case len(t) > 5 && strings.HasSuffix(t, "ing"):
		return t[:len(t)-3]
	case len(t) > 4 && strings.HasSuffix(t, "ed") && !strings.HasSuffix(t, "eed"):
		return t[:len(t)-2]
	case len(t) > 3 && strings.HasSuffix(t, "s") && !strings.HasSuffix(t, "ss") &&
		!strings.HasSuffix(t, "us") && !strings.HasSuffix(t, "is"):
		return t[:len(t)-1]
	}
	return t
}

// Slugify turns a title into a filesystem- and URL-safe slug.
func Slugify(title string) string {
	s := nonWord.ReplaceAllString(strings.ToLower(title), "-")
	s = strings.Trim(s, "-")
	if len(s) > 60 {
		s = strings.Trim(s[:60], "-")
	}
	if s == "" {
		s = "lesson"
	}
	return s
}

// snippet returns the first body line containing a query term, truncated
// on a rune boundary so multi-byte text stays valid UTF-8.
func snippet(body string, terms []string) string {
	for _, line := range strings.Split(body, "\n") {
		low := strings.ToLower(line)
		for _, t := range terms {
			if strings.Contains(low, t) {
				line = strings.TrimSpace(line)
				if len(line) > 160 {
					cut := 157
					for cut > 0 && !utf8.RuneStart(line[cut]) {
						cut--
					}
					line = line[:cut] + "..."
				}
				return line
			}
		}
	}
	return ""
}

func parseFile(path string) (Lesson, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Lesson{}, err
	}
	l := Lesson{Slug: strings.TrimSuffix(filepath.Base(path), ".md")}
	// Normalize CRLF so frontmatter survives Windows checkouts and editors.
	body := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if strings.HasPrefix(body, "---\n") {
		if end := strings.Index(body[4:], "\n---"); end >= 0 {
			fm := body[4 : 4+end]
			rest := body[4+end+4:]
			if err := yaml.Unmarshal([]byte(fm), &l); err == nil {
				body = rest
			}
		}
	}
	l.Body = strings.TrimSpace(body)
	if l.Title == "" { // tolerate hand-written files without frontmatter
		l.Title = strings.ReplaceAll(l.Slug, "-", " ")
	}
	return l, nil
}
