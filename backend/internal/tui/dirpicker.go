package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
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
	title      string
	path       string
	parent     string
	home       string
	entries    []string // visible directory names (hidden excluded by default)
	cursor     int
	width      int
	height     int
	err        error
	showHidden bool

	// create-vault submode
	creating    bool
	createInput textinput.Model
	createErr   string
}

func newDirPicker(startPath string, w, h int) (dirPicker, tea.Cmd) {
	return newDirPickerWithTitle("Selecionar Pasta", startPath, w, h)
}

func newDirPickerWithTitle(title, startPath string, w, h int) (dirPicker, tea.Cmd) {
	if startPath == "" {
		home, _ := os.UserHomeDir()
		startPath = home
	}
	startPath = filepath.Clean(startPath)
	if info, err := os.Stat(startPath); err == nil && !info.IsDir() {
		startPath = filepath.Dir(startPath)
	}

	ti := textinput.New()
	ti.Placeholder = "nome-do-vault"
	ti.CharLimit = 64

	dp := dirPicker{
		title:       title,
		path:        startPath,
		width:       w,
		height:      h,
		createInput: ti,
	}
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
	// ── Create-vault submode ──────────────────────────────────────────────
	if d.creating {
		return d.updateCreating(msg)
	}

	// ── Normal navigation ─────────────────────────────────────────────────
	switch msg := msg.(type) {
	case dirLoadedMsg:
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

		case key.Matches(msg, keys.Right), key.Matches(msg, keys.Expand):
			if len(d.entries) > 0 {
				next := filepath.Join(d.path, d.entries[d.cursor])
				d.path = next
				return d, d.loadCmd(next)
			}

		case key.Matches(msg, keys.Left):
			if d.parent != "" {
				d.path = d.parent
				return d, d.loadCmd(d.parent)
			}

		case msg.String() == "~":
			if d.home != "" {
				d.path = d.home
				return d, d.loadCmd(d.home)
			}

		case msg.String() == "H":
			d.showHidden = !d.showHidden
			return d, d.loadCmd(d.path)

		case msg.String() == "c":
			// Enter create-vault submode.
			inputW := d.inputWidth()
			d.createInput.Width = inputW
			d.createInput.SetValue("")
			d.createErr = ""
			d.creating = true
			return d, d.createInput.Focus()
		}
	}
	return d, nil
}

func (d dirPicker) updateCreating(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Cancel):
			d.creating = false
			d.createErr = ""
			return d, nil

		case msg.String() == "enter", key.Matches(msg, keys.Confirm):
			name := strings.TrimSpace(d.createInput.Value())
			if name == "" {
				d.createErr = "informe um nome para o vault"
				return d, nil
			}
			newPath := filepath.Join(d.path, name)
			if err := createVaultStructure(newPath); err != nil {
				d.createErr = err.Error()
				return d, nil
			}
			return d, func() tea.Msg { return DirPickerDone{Path: newPath} }
		}
	}

	var cmd tea.Cmd
	d.createInput, cmd = d.createInput.Update(msg)
	return d, cmd
}

// inputWidth computes the text input width for the current modal dimensions.
func (d dirPicker) inputWidth() int {
	modalW := d.width - 8
	if modalW < 40 {
		modalW = 40
	}
	if modalW > 80 {
		modalW = 80
	}
	innerW := modalW - 4
	if innerW-4 < 10 {
		return 10
	}
	return innerW - 4
}

// ── Vault scaffolding ─────────────────────────────────────────────────────────

var vaultDirs = []string{".obsidian", "PRDs", "Projects", "Resources", "Daily", "Templates"}

func createVaultStructure(root string) error {
	for _, d := range vaultDirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0755); err != nil {
			return err
		}
	}
	// Minimal Obsidian marker so the app recognises the vault immediately.
	appJSON := []byte("{}")
	if err := os.WriteFile(filepath.Join(root, ".obsidian", "app.json"), appJSON, 0644); err != nil {
		return err
	}
	// Welcome note.
	welcome := "# " + filepath.Base(root) + "\n\nVault criado pelo quinoa.\n\n" +
		"## Estrutura\n\n" +
		"- **PRDs/** — documentos de requisitos gerados pelos agentes\n" +
		"- **Projects/** — notas de projetos\n" +
		"- **Resources/** — referências e documentação\n" +
		"- **Daily/** — notas diárias\n" +
		"- **Templates/** — templates de notas\n"
	_ = os.WriteFile(filepath.Join(root, "Index.md"), []byte(welcome), 0644)
	return nil
}

// ── Rendering ─────────────────────────────────────────────────────────────────

func (d dirPicker) View() string {
	return lipgloss.Place(d.width, d.height,
		lipgloss.Center, lipgloss.Center,
		d.renderModal(),
	)
}

func (d dirPicker) renderModal() string {
	if d.creating {
		return d.renderCreateModal()
	}
	return d.renderBrowseModal()
}

func (d dirPicker) modalDimensions() (modalW, innerW int) {
	modalW = d.width - 8
	if modalW < 40 {
		modalW = 40
	}
	if modalW > 80 {
		modalW = 80
	}
	innerW = modalW - 4
	return
}

func (d dirPicker) renderBrowseModal() string {
	modalW, innerW := d.modalDimensions()

	title := styleFormTitle.Render(d.title)

	displayPath := d.path
	if d.home != "" {
		displayPath = strings.Replace(displayPath, d.home, "~", 1)
	}
	pathLine := styleCardDesc.Render(truncate(displayPath, innerW))

	divider := lipgloss.NewStyle().
		Foreground(colorBorder).
		Render(strings.Repeat("─", innerW))

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

	hints := styleHintKey.Render("o") + styleHint.Render("/spc selecionar  ") +
		styleHintKey.Render("enter/→") + styleHint.Render(" entrar  ") +
		styleHintKey.Render("←") + styleHint.Render(" voltar  ") +
		styleHintKey.Render("~") + styleHint.Render(" home  ") +
		styleHintKey.Render("c") + styleHint.Render(" criar vault  ") +
		styleHintKey.Render("H") + styleHint.Render(" ocultos  ") +
		styleHintKey.Render("esc") + styleHint.Render(" cancelar")

	body := title + "\n\n" +
		pathLine + "\n" +
		divider + "\n" +
		list + "\n" +
		divider + "\n\n" +
		hints

	return styleFormBorder.Width(modalW).Render(body)
}

func (d dirPicker) renderCreateModal() string {
	modalW, innerW := d.modalDimensions()

	title := styleFormTitle.Render("Criar Novo Vault")

	displayPath := d.path
	if d.home != "" {
		displayPath = strings.Replace(displayPath, d.home, "~", 1)
	}
	location := styleCardDesc.Render("Em: " + truncate(displayPath, innerW-4))

	divider := lipgloss.NewStyle().
		Foreground(colorBorder).
		Render(strings.Repeat("─", innerW))

	nameLabel := styleFormLabel.Render("Nome do vault:")

	// Sync the input width in case the terminal was resized.
	inp := d.createInput
	inp.Width = innerW - 4
	inputView := inp.View()

	structureLines := []string{
		"  ├ .obsidian/   " + styleHint.Render("(reconhecido pelo Obsidian)"),
		"  ├ PRDs/        " + styleHint.Render("(documentos gerados pelos agentes)"),
		"  ├ Projects/",
		"  ├ Resources/",
		"  ├ Daily/",
		"  └ Templates/",
	}
	structure := lipgloss.NewStyle().Foreground(colorMuted).Render(
		strings.Join(structureLines, "\n"),
	)

	var errLine string
	if d.createErr != "" {
		errLine = "\n" + styleStatusError.Render("✗ "+d.createErr)
	}

	hints := styleHintKey.Render("enter") + styleHint.Render(" criar  ") +
		styleHintKey.Render("esc") + styleHint.Render(" cancelar")

	body := title + "\n\n" +
		location + "\n" +
		divider + "\n" +
		nameLabel + "\n" +
		inputView + "\n" +
		divider + "\n" +
		structure +
		errLine + "\n\n" +
		hints

	return styleFormBorder.Width(modalW).Render(body)
}

// ── Scroll helpers ────────────────────────────────────────────────────────────

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
