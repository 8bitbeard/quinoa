package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Messages ──────────────────────────────────────────────────────────────────

// FilePickerDone is sent when the user selects a file.
type FilePickerDone struct {
	Path    string // absolute host path
	Title   string // extracted from first # heading or filename
	Content string // full file content
}

// FilePickerCancelled is sent when the user presses Esc.
type FilePickerCancelled struct{}

// fileLoadedMsg carries the async result of reading a directory.
type fileLoadedMsg struct {
	path    string
	parent  string
	entries []fileEntry
	err     error
}

type fileEntry struct {
	name  string
	isDir bool
}

// ── filePicker model ──────────────────────────────────────────────────────────

type filePicker struct {
	root    string // vault root — navigation can't go above this
	path    string // current directory
	parent  string // "" when at root
	entries []fileEntry
	cursor  int
	width   int
	height  int
	err     error
}

func newFilePicker(root string, w, h int) (filePicker, tea.Cmd) {
	fp := filePicker{root: root, path: root, width: w, height: h}
	return fp, fp.loadCmd(root)
}

func (f filePicker) loadCmd(path string) tea.Cmd {
	root := f.root
	return func() tea.Msg {
		rawEntries, err := os.ReadDir(path)

		var entries []fileEntry
		for _, e := range rawEntries {
			name := e.Name()
			if strings.HasPrefix(name, ".") {
				continue // skip hidden
			}
			if e.IsDir() {
				entries = append(entries, fileEntry{name: name, isDir: true})
			} else if strings.HasSuffix(strings.ToLower(name), ".md") {
				entries = append(entries, fileEntry{name: name, isDir: false})
			}
		}
		// Directories first, then files, both alphabetically.
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].isDir != entries[j].isDir {
				return entries[i].isDir
			}
			return entries[i].name < entries[j].name
		})

		parent := filepath.Dir(path)
		if parent == path || path == root {
			parent = ""
		}

		return fileLoadedMsg{path: path, parent: parent, entries: entries, err: err}
	}
}

func (f filePicker) Init() tea.Cmd { return nil }

func (f filePicker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case fileLoadedMsg:
		if msg.path != f.path {
			return f, nil
		}
		f.entries = msg.entries
		f.parent = msg.parent
		f.err = msg.err
		f.cursor = 0

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Cancel):
			return f, func() tea.Msg { return FilePickerCancelled{} }

		case key.Matches(msg, keys.Up):
			if f.cursor > 0 {
				f.cursor--
			}

		case key.Matches(msg, keys.Down):
			if f.cursor < len(f.entries)-1 {
				f.cursor++
			}

		case key.Matches(msg, keys.Left):
			if f.parent != "" {
				f.path = f.parent
				return f, f.loadCmd(f.parent)
			}

		// Enter directory or select file.
		case key.Matches(msg, keys.Right), key.Matches(msg, keys.Expand),
			msg.String() == "o", msg.String() == " ":
			return f.activate()
		}
	}
	return f, nil
}

func (f filePicker) activate() (tea.Model, tea.Cmd) {
	if len(f.entries) == 0 || f.cursor >= len(f.entries) {
		return f, nil
	}
	entry := f.entries[f.cursor]
	if entry.isDir {
		next := filepath.Join(f.path, entry.name)
		f.path = next
		return f, f.loadCmd(next)
	}

	fullPath := filepath.Join(f.path, entry.name)
	name := entry.name
	return f, func() tea.Msg {
		raw, err := os.ReadFile(fullPath)
		if err != nil {
			return FilePickerCancelled{}
		}

		// Title: first "# Heading" line, or filename without extension.
		title := strings.TrimSuffix(name, filepath.Ext(name))
		for _, line := range strings.SplitN(string(raw), "\n", 20) {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "# ") {
				title = strings.TrimPrefix(line, "# ")
				break
			}
		}

		return FilePickerDone{
			Path:    fullPath,
			Title:   title,
			Content: string(raw),
		}
	}
}

// ── View ──────────────────────────────────────────────────────────────────────

func (f filePicker) View() string {
	return lipgloss.Place(f.width, f.height,
		lipgloss.Center, lipgloss.Center,
		f.renderModal(),
	)
}

func (f filePicker) renderModal() string {
	modalW := f.width - 8
	if modalW < 40 {
		modalW = 40
	}
	if modalW > 90 {
		modalW = 90
	}
	innerW := modalW - 4

	title := styleFormTitle.Render("Selecionar Nota do Vault")

	// Current path relative to root.
	rel, _ := filepath.Rel(f.root, f.path)
	if rel == "." {
		rel = "/"
	} else {
		rel = "/" + rel
	}
	pathLine := styleCardDesc.Render(truncate(rel, innerW))

	divider := lipgloss.NewStyle().
		Foreground(colorBorder).
		Render(strings.Repeat("─", innerW))

	listHeight := f.height/2 - 8
	if listHeight < 4 {
		listHeight = 4
	}
	if listHeight > 20 {
		listHeight = 20
	}

	var listLines []string
	if f.err != nil {
		listLines = append(listLines, styleStatusError.Render("erro: "+f.err.Error()))
	} else if len(f.entries) == 0 {
		listLines = append(listLines, styleColEmpty.Render("(nenhum arquivo .md)"))
	} else {
		start, end := scrollWindow(f.cursor, len(f.entries), listHeight)
		for i := start; i < end; i++ {
			e := f.entries[i]
			var icon, label string
			if e.isDir {
				icon = styleHint.Render("  ")
				label = styleCardDesc.Render(truncate(e.name+"/", innerW-4))
			} else {
				icon = styleHint.Render("  ")
				label = lipgloss.NewStyle().Foreground(colorText).Render(truncate(e.name, innerW-4))
			}
			if i == f.cursor {
				icon = styleHintKey.Render("▶ ")
				if e.isDir {
					label = lipgloss.NewStyle().Foreground(colorMuted).Bold(true).Render(truncate(e.name+"/", innerW-4))
				} else {
					label = lipgloss.NewStyle().Foreground(colorText).Bold(true).Render(truncate(e.name, innerW-4))
				}
			}
			listLines = append(listLines, icon+label)
		}
		if len(f.entries) > listHeight {
			listLines = append(listLines, styleHint.Render(
				strings.Repeat(" ", innerW-8)+
					formatScrollIndicator(f.cursor, len(f.entries), end-start),
			))
		}
	}

	list := strings.Join(listLines, "\n")

	hints := styleHintKey.Render("enter/→") + styleHint.Render(" selecionar  ") +
		styleHintKey.Render("←") + styleHint.Render(" voltar  ") +
		styleHintKey.Render("↑↓") + styleHint.Render(" navegar  ") +
		styleHintKey.Render("esc") + styleHint.Render(" cancelar")

	body := title + "\n\n" +
		pathLine + "\n" +
		divider + "\n" +
		list + "\n" +
		divider + "\n\n" +
		hints

	return styleFormBorder.Width(modalW).Render(body)
}
