package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/amithgc/quarters/internal/config"
	"github.com/amithgc/quarters/internal/projects"
)

// ---- agent instructions (CLAUDE.md, AGENTS.md, …) and project notes ----

// maxInstructions is the largest file the editor opens.
const maxInstructions = 1 << 20

// InstructionFile is one file a coding agent reads its standing
// instructions from, in a project's folder.
type InstructionFile struct {
	Name   string   `json:"name"`
	Exists bool     `json:"exists"`
	Lines  int      `json:"lines"`
	Agents []string `json:"agents"` // the agents that read it
}

// InstructionsView is what the stats panel lists for a project.
type InstructionsView struct {
	Files []InstructionFile `json:"files"` // the ones there, and the project agent's own
	Notes string            `json:"notes"` // the notes' first line ("" = none)
}

// Text is a file opened in the editor. Hash is the text it had, so a save
// can tell when someone (the agent) changed the file in between.
type Text struct {
	Text string `json:"text"`
	Hash string `json:"hash"`
}

// instructionNames are the files the editor may open: the ones the known
// agents read, nothing else.
func instructionNames() []string {
	var out []string
	for _, h := range config.Harnesses {
		if h.Instructions != "" && !slices.Contains(out, h.Instructions) {
			out = append(out, h.Instructions)
		}
	}
	return out
}

// Instructions lists a project's instruction files: those it has, and the
// one its agent reads (to create), with how long each is and who reads it.
func (a *App) Instructions(path string) (InstructionsView, error) {
	p, ok := a.project(path)
	if !ok {
		return InstructionsView{}, errors.New("not one of your projects")
	}
	own := a.config().HarnessFor(p).Instructions
	v := InstructionsView{Files: []InstructionFile{}}
	for _, name := range instructionNames() {
		f := InstructionFile{Name: name, Agents: []string{}}
		for _, h := range config.Harnesses {
			if h.Instructions == name {
				f.Agents = append(f.Agents, h.Name)
			}
		}
		if b, err := os.ReadFile(filepath.Join(path, name)); err == nil {
			f.Exists = true
			f.Lines = strings.Count(string(b), "\n")
			if len(b) > 0 && b[len(b)-1] != '\n' {
				f.Lines++
			}
		}
		if f.Exists || name == own {
			v.Files = append(v.Files, f)
		}
	}
	if b, err := os.ReadFile(notesPath(path)); err == nil {
		first, _, _ := strings.Cut(strings.TrimSpace(string(b)), "\n")
		v.Notes = strings.TrimSpace(strings.TrimLeft(first, "# "))
		if v.Notes == "" && len(strings.TrimSpace(string(b))) > 0 {
			v.Notes = "…"
		}
	}
	return v, nil
}

// instructionPath checks name is an instruction file and returns where it
// is in project path.
func (a *App) instructionPath(path, name string) (string, error) {
	if _, ok := a.project(path); !ok {
		return "", errors.New("not one of your projects")
	}
	if !slices.Contains(instructionNames(), name) {
		return "", fmt.Errorf("%q is not an agent instructions file", name)
	}
	return filepath.Join(path, name), nil
}

// ReadInstructions opens an instruction file ("" text when it isn't there yet).
func (a *App) ReadInstructions(path, name string) (Text, error) {
	file, err := a.instructionPath(path, name)
	if err != nil {
		return Text{}, err
	}
	return loadText(file)
}

// SaveInstructions writes an instruction file. hash is the text the editor
// started from: when the file changed since, nothing is written.
func (a *App) SaveInstructions(path, name, text, hash string) (Text, error) {
	file, err := a.instructionPath(path, name)
	if err != nil {
		return Text{}, err
	}
	// CLAUDE.md is often a link to AGENTS.md: write the file it points
	// to (in the project), not over the link
	if real, err := filepath.EvalSymlinks(file); err == nil && real != file {
		if !a.insideProject(real) {
			return Text{}, fmt.Errorf("%s links to a file outside the project", name)
		}
		file = real
	}
	return writeText(file, text, hash, name+" changed since you opened it (the agent may have edited it): reopen it to see the new version")
}

// notesPath is where Quarters keeps a project's notes: with its own
// settings, not in the project (they are yours, not the agent's).
func notesPath(path string) string {
	return filepath.Join(filepath.Dir(config.Path()), "notes", projects.EncodeDir(path)+".md")
}

// Notes opens a project's notes.
func (a *App) Notes(path string) (Text, error) {
	if _, ok := a.project(path); !ok {
		return Text{}, errors.New("not one of your projects")
	}
	return loadText(notesPath(path))
}

// SaveNotes writes a project's notes (see SaveInstructions for hash).
func (a *App) SaveNotes(path, text, hash string) (Text, error) {
	if _, ok := a.project(path); !ok {
		return Text{}, errors.New("not one of your projects")
	}
	file := notesPath(path)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return Text{}, err
	}
	return writeText(file, text, hash, "the notes changed since you opened them (in another window?): reopen them to see the new version")
}

func textHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

func loadText(file string) (Text, error) {
	st, err := os.Stat(file)
	if errors.Is(err, fs.ErrNotExist) {
		return Text{Hash: textHash(nil)}, nil
	}
	if err != nil {
		return Text{}, err
	}
	if !st.Mode().IsRegular() || st.Size() > maxInstructions {
		return Text{}, fmt.Errorf("%s is too big to edit here", filepath.Base(file))
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return Text{}, err
	}
	if !utf8.Valid(b) {
		return Text{}, fmt.Errorf("%s is not text", filepath.Base(file))
	}
	return Text{Text: string(b), Hash: textHash(b)}, nil
}

// writeText replaces file with text (through a temporary file, so a crash
// never leaves half of it), unless it no longer has the text hash names.
func writeText(file, text, hash, changed string) (Text, error) {
	if len(text) > maxInstructions {
		return Text{}, errors.New("that is too long to save here")
	}
	cur, err := loadText(file)
	if err != nil {
		return Text{}, err
	}
	if cur.Hash != hash {
		return Text{}, errors.New(changed)
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	mode := fs.FileMode(0o644)
	if st, err := os.Stat(file); err == nil {
		mode = st.Mode().Perm()
	}
	tmp := file + ".quarters-tmp"
	if err := os.WriteFile(tmp, []byte(text), mode); err != nil {
		return Text{}, err
	}
	if err := os.Rename(tmp, file); err != nil {
		os.Remove(tmp)
		return Text{}, err
	}
	return Text{Text: text, Hash: textHash([]byte(text))}, nil
}
