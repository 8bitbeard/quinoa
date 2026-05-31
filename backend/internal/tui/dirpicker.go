package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Messages ──────────────────────────────────────────────────────────────────

// DirPickerDone is returned when the user confirms a directory selection.
type DirPickerDone struct{ Path string }

// DirPickerCancelled is returned when the user presses Esc.
type DirPickerCancelled struct{}

// dirLoadedMsg carries the result of an async directory read.
type dirLoadedMsg struct {
	path    string
	parent  string
	home    string
	entries []string
	err     error
}

// ── dirPicker model ───────────────────────────────────────────────────────────

type dirPicker struct {
	path    string
	parent  string
	home    string
	entries []string // visible directory names (hidden excluded by default)
	cursor  int
	width   int
	height  int
	err     error
	showHidden bool
}

func newDirPicker(startPath string, w, h int) (dirPicker, tea.Cmd) {
	if startPath == "" {
		home, _ := os.UserHomeDir()
		startPath = home
	}
	startPath = filepath.Clean(startPath)
	// If startPath is a file, use its directory.
	if info, err := os.Stat(startPath); err == nil && !info.IsDir() {
		startPath = filepath.Dir(startPath)
	}
	dp := dirPicker{path: startPath, width: w, height: h}
	return dp, dp.loadCmd(startPath)
}

func (d dirPicker) loadCmd(path string) tea.Cmd {
	showHidden := d.showHidden
	return func() tea.Msg {
		path = filepath.Clean(path)
		rawEntries, err := os.ReadDir(path)

		var dirs []string
		for _, e := range rawEntries {
			if !e.IsDir() {
				continue
			}
			if !showHidden && strings.HasPrefix(e.Name(), ".") {
				continue
			}
			dirs = append(dirs, e.Name())
		}
		sort.Strings(dirs)

		parent := filepath.Dir(path)
		if parent == path {
			parent = ""
		}
		home, _ := os.UserHomeDir()

		return dirLoadedMsg{
			path:    path,
			parent:  parent,
			home:    home,
			entries: dirs,
			err:     err,
		}
	}
}

func (d dirPicker) Init() tea.Cmd { return nil }

func (d dirPicker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case dirLoadedMsg:
		// Only apply if this is for our current path (guard against stale loads).
		if msg.path != d.path {
			return d, nil
		}
		d.path = msg.path
		d.parent = msg.parent
		d.home = msg.home
		d.entries = msg.entries
		d.err = msg.err
		d.cursor = 0

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Cancel):
			return d, func() tea.Msg { return DirPickerCancelled{} }

		// Confirm: select current directory.
		case msg.String() == "o", msg.String() == " ", key.Matches(msg, keys.Confirm):
			path := d.path
			return d, func() tea.Msg { return DirPickerDone{Path: path} }

		case key.Matches(msg, keys.Up):
			if d.cursor > 0 {
				d.cursor--
			}

		case key.Matches(msg, keys.Down):
			if d.cursor < len(d.entries)-1 {
				d.cursor++
			}

		// Enter selected directory.
		case key.Matches(msg, keys.Right), key.Matches(msg, keys.Expand):
			if len(d.entries) > 0 {
				next := filepath.Join(d.path, d.entries[d.cursor])
				d.path = next
				return d, d.loadCmd(next)
			}

		// Go up to parent directory.
		case key.Matches(msg, keys.Left):
			if d.parent != "" {
				d.path = d.parent
				return d, d.loadCmd(d.parent)
			}

		// Jump to home directory.
		case msg.String() == "~":
			if d.home != "" {
				d.path = d.home
				return d, d.loadCmd(d.home)
			}

		// Toggle hidden directories.
		case msg.String() == "H":
			d.showHidden = !d.showHidden
			return d, d.loadCmd(d.path)
		}
	}
	return d, nil
}

// ── Rendering ─────────────────────────────────────────────────────────────────

func (d dirPicker) View() string {
	return lipgloss.Place(d.width, d.height,
		lipgloss.Center, lipgloss.Center,
		d.renderModal(),
	)
}

func (d dirPicker) renderModal() string {
	modalW := d.width - 8
	if modalW < 40 {
		modalW = 40
	}
	if modalW > 80 {
		modalW = 80
	}

	innerW := modalW - 4 // inside the border+padding

	// ── Title ──
	title := styleFormTitle.Render("Selecionar Pasta")

	// ── Current path ──
	displayPath := d.path
	if d.home != "" {
		displayPath = strings.Replace(displayPath, d.home, "~", 1)
	}
	pathLine := styleCardDesc.Render(truncate(displayPath, innerW))

	divider := lipgloss.NewStyle().
		Foreground(colorBorder).
		Render(strings.Repeat("─", innerW))

	// ── Directory list ──
	listHeight := d.height/2 - 8
	if listHeight < 4 {
		listHeight = 4
	}
	if listHeight > 20 {
		listHeight = 20
	}

	var listLines []string

	if d.err != nil {
		listLines = append(listLines, styleStatusError.Render("erro: "+d.err.Error()))
	} else if len(d.entries) == 0 {
		listLines = append(listLines, styleColEmpty.Render("(nenhum subdiretório)"))
	} else {
		// Scroll window so cursor is always visible.
		start, end := scrollWindow(d.cursor, len(d.entries), listHeight)
		for i := start; i < end; i++ {
			name := d.entries[i]
			entry := truncate(name+"/", innerW-3)
			var line string
			if i == d.cursor {
				line = styleHintKey.Render("▶ ") + lipgloss.NewStyle().
					Foreground(colorText).Bold(true).
					Render(entry)
			} else {
				line = styleHint.Render("  ") + styleCardDesc.Render(entry)
			}
			listLines = append(listLines, line)
		}
		// Scroll indicator
		if len(d.entries) > listHeight {
			shown := end - start
			indicator := styleHint.Render(
				strings.Repeat(" ", innerW-8) +
					lipgloss.NewStyle().Foreground(colorMuted).
						Render(formatScrollIndicator(d.cursor, len(d.entries), shown)),
			)
			listLines = append(listLines, indicator)
		}
	}

	list := strings.Join(listLines, "\n")

	// ── Hints ──
	hints := styleHintKey.Render("o") + styleHint.Render("/spc selecionar aqui  ") +
		styleHintKey.Render("enter/→") + styleHint.Render(" entrar  ") +
		styleHintKey.Render("←") + styleHint.Render(" voltar  ") +
		styleHintKey.Render("~") + styleHint.Render(" home  ") +
		styleHintKey.Render("H") + styleHint.Render(" ocultos  ") +
		styleHintKey.Render("esc") + styleHint.Render(" cancelar")

	body := title + "\n\n" +
		pathLine + "\n" +
		divider + "\n" +
		list + "\n" +
		divider + "\n\n" +
		hints

	hidden := ""
	if d.showHidden {
		hidden = styleHint.Render(" (+ ocultos)")
	}
	_ = hidden // shown via entries themselves

	return styleFormBorder.Width(modalW).Render(body)
}

// scrollWindow returns the [start, end) range of entries to display so the
// cursor is always within view.
func scrollWindow(cursor, total, height int) (start, end int) {
	if total <= height {
		return 0, total
	}
	start = cursor - height/2
	if start < 0 {
		start = 0
	}
	end = start + height
	if end > total {
		end = total
		start = end - height
		if start < 0 {
			start = 0
		}
	}
	return start, end
}

func formatScrollIndicator(cursor, total, shown int) string {
	_ = shown
	pct := 0
	if total > 1 {
		pct = (cursor * 100) / (total - 1)
	}
	return fmt.Sprintf("%d/%d (%d%%)", cursor+1, total, pct)
}
