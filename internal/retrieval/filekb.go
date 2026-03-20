package retrieval

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/infercore/infercore/internal/types"
)

// FileKB is a simple file-backed knowledge base: loads text from .md and .txt under root.
type FileKB struct {
	name string
	root string
	docs []docChunk
}

type docChunk struct {
	text   string
	source string
}

// NewFileKB walks root and indexes plain text chunks (by file, split on blank lines).
func NewFileKB(name, root string) (*FileKB, error) {
	f := &FileKB{name: name, root: root}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		lower := strings.ToLower(path)
		if !strings.HasSuffix(lower, ".md") && !strings.HasSuffix(lower, ".txt") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		text := string(b)
		parts := strings.Split(text, "\n\n")
		for i, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			rel, _ := filepath.Rel(root, path)
			src := rel
			if len(parts) > 1 {
				src = fmt.Sprintf("%s#%d", filepath.ToSlash(rel), i)
			}
			f.docs = append(f.docs, docChunk{text: p, source: src})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return f, nil
}

// Name implements RetrievalAdapter.
func (f *FileKB) Name() string { return f.name }

// Retrieve scores chunks by token overlap with query (v1 demo).
func (f *FileKB) Retrieve(_ context.Context, query string, _ map[string]any) (types.RetrievalResult, error) {
	qTokens := tokenize(query)
	if len(qTokens) == 0 {
		return types.RetrievalResult{}, nil
	}
	type scored struct {
		ch types.RetrievalChunk
		s  float64
	}
	var out []scored
	for _, d := range f.docs {
		dt := tokenize(d.text)
		if len(dt) == 0 {
			continue
		}
		s := overlapScore(qTokens, dt)
		if s <= 0 {
			continue
		}
		out = append(out, scored{
			ch: types.RetrievalChunk{Text: d.text, Source: d.source, Score: s},
			s:  s,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].s > out[j].s })
	top := 5
	if len(out) < top {
		top = len(out)
	}
	chunks := make([]types.RetrievalChunk, 0, top)
	for i := 0; i < top; i++ {
		chunks = append(chunks, out[i].ch)
	}
	return types.RetrievalResult{Chunks: chunks}, nil
}

func tokenize(s string) []string {
	s = strings.ToLower(s)
	var cur strings.Builder
	var toks []string
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		toks = append(toks, cur.String())
		cur.Reset()
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return toks
}

func overlapScore(q, doc []string) float64 {
	set := make(map[string]struct{}, len(doc))
	for _, t := range doc {
		set[t] = struct{}{}
	}
	var hit float64
	for _, t := range q {
		if _, ok := set[t]; ok {
			hit++
		}
	}
	if hit == 0 {
		return 0
	}
	return hit / float64(len(q))
}
