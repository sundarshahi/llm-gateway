package llmgateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// PromptStore resolves a stored system prompt and optional JSON schema for a
// named stage. Implementations must be safe for concurrent use.
type PromptStore interface {
	// Prompt returns the effective system prompt for a stage. An error (e.g. the
	// stage doesn't exist) is surfaced to the caller as a 400/404.
	Prompt(name string) (string, error)
	// Schema returns the compact JSON schema for a stage, or "" if none. A stage
	// with a schema is auto-upgraded to native structured output.
	Schema(name string) string
}

// promptNameRe guards path-traversal: a stage name is a slug.
var promptNameRe = regexp.MustCompile(`^[a-z0-9-]+$`)

// ValidPromptName reports whether name is a safe stage slug.
func ValidPromptName(name string) bool { return promptNameRe.MatchString(name) }

// FilePromptStore loads prompts from <Dir>/<name>.md and schemas from
// <SchemaDir>/<name>.json. An optional learned addendum at <AddendumDir>/<name>.md
// is appended under AddendumHeader — this is how an external tuning loop layers
// rules onto a base prompt without editing it. Effective prompts and schemas are
// cached; call Bust after writing an addendum.
type FilePromptStore struct {
	Dir            string
	SchemaDir      string
	AddendumDir    string // "" disables the addendum merge
	AddendumHeader string // prepended above the addendum body when non-empty

	mu          sync.RWMutex
	promptCache map[string]string
	schemaCache map[string]string
}

// NewFilePromptStore builds a store. SchemaDir/AddendumDir may be empty to
// disable schemas / the addendum merge respectively.
func NewFilePromptStore(dir, schemaDir, addendumDir string) *FilePromptStore {
	return &FilePromptStore{
		Dir:         dir,
		SchemaDir:   schemaDir,
		AddendumDir: addendumDir,
		promptCache: map[string]string{},
		schemaCache: map[string]string{},
	}
}

func (s *FilePromptStore) Prompt(name string) (string, error) {
	if !promptNameRe.MatchString(name) {
		return "", fmt.Errorf("invalid prompt_name %q", name)
	}
	s.mu.RLock()
	v, ok := s.promptCache[name]
	s.mu.RUnlock()
	if ok {
		return v, nil
	}
	b, err := os.ReadFile(filepath.Join(s.Dir, name+".md"))
	if err != nil {
		return "", err
	}
	txt := string(b)
	if s.AddendumDir != "" {
		if lb, lerr := os.ReadFile(filepath.Join(s.AddendumDir, name+".md")); lerr == nil {
			if learned := strings.TrimSpace(string(lb)); learned != "" {
				header := s.AddendumHeader
				if header != "" {
					header += "\n"
				}
				txt += "\n\n" + header + learned + "\n"
			}
		}
	}
	s.mu.Lock()
	s.promptCache[name] = txt
	s.mu.Unlock()
	return txt, nil
}

func (s *FilePromptStore) Schema(name string) string {
	if s.SchemaDir == "" || !promptNameRe.MatchString(name) {
		return ""
	}
	s.mu.RLock()
	v, ok := s.schemaCache[name]
	s.mu.RUnlock()
	if ok {
		return v
	}
	out := ""
	if b, err := os.ReadFile(filepath.Join(s.SchemaDir, name+".json")); err == nil {
		var buf bytes.Buffer
		if json.Compact(&buf, b) == nil {
			out = buf.String()
		} else {
			out = strings.TrimSpace(string(b))
		}
	}
	s.mu.Lock()
	s.schemaCache[name] = out // negative result cached too (schemas are static)
	s.mu.Unlock()
	return out
}

// Bust drops a stage's cached prompt (call after writing an addendum).
func (s *FilePromptStore) Bust(name string) {
	s.mu.Lock()
	delete(s.promptCache, name)
	s.mu.Unlock()
}

// SetAddendum writes (or, with empty text, removes) the learned addendum for a
// stage and busts its cache. It is the primitive an external tuning loop uses.
// Requires AddendumDir to be set.
func (s *FilePromptStore) SetAddendum(name, text string) error {
	if s.AddendumDir == "" {
		return fmt.Errorf("addendum dir not configured")
	}
	if !promptNameRe.MatchString(name) {
		return fmt.Errorf("invalid prompt_name %q", name)
	}
	if err := os.MkdirAll(s.AddendumDir, 0o755); err != nil {
		return err
	}
	fp := filepath.Join(s.AddendumDir, name+".md")
	text = strings.TrimSpace(text)
	if text == "" {
		if _, err := os.Stat(fp); err == nil {
			if err := os.Remove(fp); err != nil {
				return err
			}
		}
	} else if err := os.WriteFile(fp, []byte(text+"\n"), 0o644); err != nil {
		return err
	}
	s.Bust(name)
	return nil
}
