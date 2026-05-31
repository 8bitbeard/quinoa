package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"

	"github.com/wiltsou/quinoa/internal/db"
	"github.com/wiltsou/quinoa/internal/docker"
)

// ── Messages ──────────────────────────────────────────────────────────────────

type tickMsg time.Time
type storiesLoadedMsg []*db.Story
type errMsg struct{ err error }

// ── Board columns ─────────────────────────────────────────────────────────────

var columns = []string{"todo", "doing", "review", "done"}

// ── Model ─────────────────────────────────────────────────────────────────────

type Model struct {
	db      *db.DB
	runner  *Runner
	stories []*db.Story

	// board state
	col    int // focused column index (0-3)
	cursor int // focused card index within column

	// derived per-render
	cols [4][]*db.Story

	// overlay
	showHelp   bool
	activeForm tea.Model // nil = no form open
	formKind   string    // "new-story" | "start-agent"

	// dimensions
	width  int
	height int

	// expanded card
	expanded bool
}

func NewModel(database *db.DB, runner *Runner) Model {
	return Model{
		db:     database,
		runner: runner,
	}
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.loadStories(),
		tickEvery(3*time.Second),
	)
}

func tickEvery(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) loadStories() tea.Cmd {
	return func() tea.Msg {
		stories, err := m.db.ListStories()
		if err != nil {
			return errMsg{err}
		}
		return storiesLoadedMsg(stories)
	}
}

// ── Update ────────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// If a form is open, forward most events to it.
	if m.activeForm != nil {
		switch msg := msg.(type) {
		case FormCancelled:
			m.activeForm = nil
			return m, nil

		case FormDone:
			return m.handleFormDone(msg)

		case tea.WindowSizeMsg:
			m.width = msg.Width
			m.height = msg.Height

		case tickMsg:
			return m, tea.Batch(m.loadStories(), tickEvery(3*time.Second))

		case storiesLoadedMsg:
			m.stories = msg
			m.rebuildCols()
			return m, nil
		}

		var cmd tea.Cmd
		m.activeForm, cmd = m.activeForm.Update(msg)
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tickMsg:
		return m, tea.Batch(m.loadStories(), tickEvery(3*time.Second))

	case storiesLoadedMsg:
		m.stories = msg
		m.rebuildCols()
		m.clampCursor()

	case errMsg:
		// TODO: surface in status bar
		_ = msg.err

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m *Model) rebuildCols() {
	m.cols = [4][]*db.Story{}
	for _, s := range m.stories {
		switch s.KanbanStatus {
		case "todo":
			m.cols[0] = append(m.cols[0], s)
		case "doing":
			m.cols[1] = append(m.cols[1], s)
		case "review":
			m.cols[2] = append(m.cols[2], s)
		case "done":
			m.cols[3] = append(m.cols[3], s)
		}
	}
}

func (m *Model) clampCursor() {
	cards := m.cols[m.col]
	if m.cursor >= len(cards) {
		m.cursor = len(cards) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *Model) selectedStory() *db.Story {
	cards := m.cols[m.col]
	if len(cards) == 0 || m.cursor >= len(cards) {
		return nil
	}
	return cards[m.cursor]
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, keys.Help):
		m.showHelp = !m.showHelp

	case key.Matches(msg, keys.Refresh):
		return m, m.loadStories()

	case key.Matches(msg, keys.Left):
		m.expanded = false
		if m.col > 0 {
			m.col--
			m.clampCursor()
		}

	case key.Matches(msg, keys.Right):
		m.expanded = false
		if m.col < 3 {
			m.col++
			m.clampCursor()
		}

	case key.Matches(msg, keys.Up):
		if m.cursor > 0 {
			m.cursor--
		}

	case key.Matches(msg, keys.Down):
		cards := m.cols[m.col]
		if m.cursor < len(cards)-1 {
			m.cursor++
		}

	case key.Matches(msg, keys.Expand):
		m.expanded = !m.expanded

	case key.Matches(msg, keys.New):
		f := newNewStoryForm(m.width, m.height)
		m.activeForm = f
		m.formKind = "new-story"
		return m, f.Init()

	case key.Matches(msg, keys.Start):
		s := m.selectedStory()
		if s == nil || m.col != 0 {
			break
		}
		f := newStartAgentForm(s.ID, s.Title, m.width, m.height)
		m.activeForm = f
		m.formKind = "start-agent"
		return m, f.Init()

	case key.Matches(msg, keys.Approve):
		s := m.selectedStory()
		if s != nil && m.col == 2 { // review
			return m, m.moveStory(s, "done")
		}

	case key.Matches(msg, keys.Fix):
		s := m.selectedStory()
		if s != nil && m.col == 2 { // review → doing
			return m, m.moveStory(s, "doing")
		}

	case key.Matches(msg, keys.Reopen):
		s := m.selectedStory()
		if s != nil && (m.col == 2 || m.col == 3) {
			return m, m.moveStory(s, "todo")
		}

	case key.Matches(msg, keys.Stop):
		s := m.selectedStory()
		if s != nil && m.col == 1 { // doing
			return m, m.stopStory(s)
		}

	case key.Matches(msg, keys.Delete):
		s := m.selectedStory()
		if s != nil && (m.col == 0 || m.col == 3) {
			return m, m.deleteStory(s)
		}

	case key.Matches(msg, keys.Terminal):
		s := m.selectedStory()
		if s == nil || s.TaskID == "" {
			break
		}
		return m, m.openTerminal(s)
	}

	return m, nil
}

func (m Model) handleFormDone(msg FormDone) (tea.Model, tea.Cmd) {
	m.activeForm = nil

	switch msg.Kind {
	case "new-story":
		return m, func() tea.Msg {
			s := &db.Story{
				ID:           uuid.New().String(),
				Title:        msg.Fields["title"],
				Description:  msg.Fields["description"],
				KanbanStatus: "todo",
			}
			if err := m.db.InsertStory(s); err != nil {
				return errMsg{err}
			}
			stories, _ := m.db.ListStories()
			return storiesLoadedMsg(stories)
		}

	case "start-agent":
		return m, func() tea.Msg {
			storyID := msg.Fields["story_id"]
			story, err := m.db.GetStory(storyID)
			if err != nil {
				return errMsg{err}
			}

			prompt := story.Title
			if story.Description != "" {
				prompt += "\n\n" + story.Description
			}
			agentCmd := msg.Fields["agent_command"] + " " + shellQuote(prompt)

			var envExtra []string
			for _, line := range strings.Split(msg.Fields["env_extra"], "\n") {
				line = strings.TrimSpace(line)
				if strings.Contains(line, "=") {
					envExtra = append(envExtra, line)
				}
			}

			taskID := uuid.New().String()
			task, err := m.runner.StartTask(docker.RunConfig{
				TaskID:       taskID,
				RepoURL:      msg.Fields["repo_url"],
				RepoPath:     msg.Fields["repo_path"],
				RepoBranch:   msg.Fields["repo_branch"],
				AgentCommand: agentCmd,
				EnvExtra:     envExtra,
			})
			if err != nil {
				return errMsg{err}
			}

			if err := m.db.UpdateStoryKanban(storyID, "doing", task.ID); err != nil {
				return errMsg{err}
			}

			stories, _ := m.db.ListStories()
			return storiesLoadedMsg(stories)
		}
	}

	return m, nil
}

func (m Model) moveStory(s *db.Story, to string) tea.Cmd {
	return func() tea.Msg {
		taskID := s.TaskID
		switch to {
		case "todo":
			if s.TaskID != "" {
				if task, err := m.db.GetTask(s.TaskID); err == nil && task.ContainerID != "" {
					m.runner.StopTask(task)
				}
			}
			taskID = ""
		case "doing":
			if s.TaskID != "" {
				if task, err := m.db.GetTask(s.TaskID); err == nil && task.ContainerID != "" {
					_ = m.db.UpdateTaskStatus(task.ID, "running", task.ContainerID)
				}
			}
		case "done":
			if s.TaskID != "" {
				if task, err := m.db.GetTask(s.TaskID); err == nil && task.ContainerID != "" {
					m.runner.StopTask(task)
					taskID = task.ID
				}
			}
		}
		_ = m.db.UpdateStoryKanban(s.ID, to, taskID)
		stories, _ := m.db.ListStories()
		return storiesLoadedMsg(stories)
	}
}

func (m Model) stopStory(s *db.Story) tea.Cmd {
	return m.moveStory(s, "todo")
}

func (m Model) deleteStory(s *db.Story) tea.Cmd {
	return func() tea.Msg {
		_ = m.db.DeleteStory(s.ID)
		stories, _ := m.db.ListStories()
		return storiesLoadedMsg(stories)
	}
}

func (m Model) openTerminal(s *db.Story) tea.Cmd {
	task, err := m.db.GetTask(s.TaskID)
	if err != nil || task.ContainerID == "" {
		return nil
	}
	cmd := newAttachExec(task.ContainerID, m.runner.Docker())
	return tea.Exec(cmd, func(err error) tea.Msg {
		// After detach, reload board to reflect any status changes.
		stories, _ := m.db.ListStories()
		return storiesLoadedMsg(stories)
	})
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.activeForm != nil {
		return m.activeForm.View()
	}
	if m.showHelp {
		return m.helpView()
	}
	return m.boardView()
}

func (m Model) boardView() string {
	if m.width == 0 {
		return "carregando..."
	}

	header := m.renderHeader()
	hint := m.renderHint()
	hintH := lipgloss.Height(hint)
	headerH := lipgloss.Height(header)

	boardH := m.height - headerH - hintH - 1
	if boardH < 5 {
		boardH = 5
	}

	board := m.renderBoard(m.width, boardH)

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		board,
		hint,
	)
}

func (m Model) renderHeader() string {
	title := lipgloss.NewStyle().
		Foreground(colorAccent).
		Bold(true).
		Render("quinoa")

	nav := lipgloss.NewStyle().
		Foreground(colorMuted).
		Render("board")

	sep := lipgloss.NewStyle().
		Foreground(colorBorder).
		Render(" · ")

	right := lipgloss.NewStyle().
		Foreground(colorMuted).
		Render(time.Now().Format("15:04:05"))

	middle := title + sep + nav
	gap := m.width - lipgloss.Width(middle) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	return " " + middle + strings.Repeat(" ", gap) + right
}

func (m Model) renderHint() string {
	colStatus := columns[m.col]
	hasContainer := false
	if s := m.selectedStory(); s != nil && s.TaskID != "" {
		if task, err := m.db.GetTask(s.TaskID); err == nil && task.ContainerID != "" {
			hasContainer = true
		}
	}
	return " " + helpText(colStatus, hasContainer)
}

func (m Model) renderBoard(width, height int) string {
	gap := 1
	colW := (width - gap*3) / 4

	colNames := []string{"Todo", "Doing", "Review", "Done"}
	var rendered []string

	for i, name := range colNames {
		cards := m.cols[i]
		focused := i == m.col

		rendered = append(rendered, m.renderColumn(i, name, cards, focused, colW, height))
	}

	return lipgloss.JoinHorizontal(lipgloss.Top,
		rendered[0],
		strings.Repeat(" ", gap),
		rendered[1],
		strings.Repeat(" ", gap),
		rendered[2],
		strings.Repeat(" ", gap),
		rendered[3],
	)
}

func (m Model) renderColumn(colIdx int, name string, cards []*db.Story, focused bool, width, height int) string {
	count := fmt.Sprintf("(%d)", len(cards))

	var headerStyle lipgloss.Style
	if colIdx == 2 { // review
		headerStyle = styleColHeaderReview
	} else {
		headerStyle = styleColHeader
	}
	if focused {
		headerStyle = styleColFocused
	}

	header := headerStyle.Render(name) + " " +
		lipgloss.NewStyle().Foreground(colorBg3).Render(count)

	divider := lipgloss.NewStyle().
		Foreground(colorBorder).
		Render(strings.Repeat("─", width))

	var cardLines []string
	if len(cards) == 0 {
		cardLines = append(cardLines, styleColEmpty.Width(width).Render("(vazio)"))
	}

	for i, s := range cards {
		sel := focused && i == m.cursor
		cardLines = append(cardLines, m.renderCard(s, sel, colIdx, width))
	}

	innerH := height - 3
	if innerH < 1 {
		innerH = 1
	}
	body := strings.Join(cardLines, "\n")
	// Clip or pad height
	lines := strings.Split(body, "\n")
	if len(lines) > innerH {
		lines = lines[:innerH]
	}
	body = strings.Join(lines, "\n")

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		divider,
		body,
	)
}

func (m Model) renderCard(s *db.Story, selected bool, colIdx int, width int) string {
	innerW := width - 4
	if innerW < 4 {
		innerW = 4
	}

	title := styleCardTitle.Width(innerW).Render(truncate(s.Title, innerW))

	var parts []string
	parts = append(parts, title)

	if s.Description != "" {
		maxDescLines := 2
		if selected && m.expanded {
			maxDescLines = 8
		}
		desc := wrapText(s.Description, innerW, maxDescLines)
		parts = append(parts, styleCardDesc.Width(innerW).Render(desc))
	}

	if s.TaskStatus != "" {
		badge := statusStyle(s.TaskStatus).Render(statusLabel(s.TaskStatus))
		parts = append(parts, badge)
	}

	content := strings.Join(parts, "\n")

	if selected {
		return styleCardSelected.Width(width - 2).Render(content)
	}
	return styleCard.Width(width - 2).Render(content)
}

func (m Model) helpView() string {
	sections := []struct{ title, body string }{
		{"Navegação", "h/← l/→   mover entre colunas\nk/↑ j/↓   mover entre cards\nenter     expandir descrição"},
		{"Ações Globais", "n         nova história\nr         refresh manual\n?         fechar ajuda\nq         sair"},
		{"Todo", "s         iniciar agente\nx         deletar história"},
		{"Doing", "p         parar agente → todo"},
		{"Review", "a         aprovar → done\nf         corrigir → doing\nb         reabrir → todo"},
		{"Done", "b         reabrir → todo\nx         deletar história"},
		{"Formulários", "tab       próximo campo\nshift+tab campo anterior\nctrl+s    confirmar\nesc       cancelar"},
	}

	var rows []string
	for _, s := range sections {
		title := styleFormTitle.Render(s.title)
		body := styleHint.Render(s.body)
		rows = append(rows, title+"\n"+body)
	}

	content := styleFormTitle.Render("Ajuda — quinoa TUI") + "\n\n" +
		strings.Join(rows, "\n\n") + "\n\n" +
		styleHintKey.Render("?") + styleHint.Render(" para fechar")

	w := 50
	if m.width-8 > w {
		w = m.width / 2
	}

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		styleFormBorder.Width(w).Render(content),
	)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

func wrapText(s string, width, maxLines int) string {
	if width <= 0 {
		return s
	}
	var out []string
	for _, paragraph := range strings.Split(s, "\n") {
		runes := []rune(paragraph)
		for len(runes) > 0 {
			if len(out) >= maxLines {
				if len(out) > 0 {
					last := []rune(out[len(out)-1])
					if len(last) > 3 {
						out[len(out)-1] = string(last[:len(last)-3]) + "..."
					}
				}
				return strings.Join(out, "\n")
			}
			end := width
			if end > len(runes) {
				end = len(runes)
			}
			out = append(out, string(runes[:end]))
			runes = runes[end:]
		}
	}
	if len(out) > maxLines {
		out = out[:maxLines]
		last := []rune(out[len(out)-1])
		if len(last) > 3 {
			out[len(out)-1] = string(last[:len(last)-3]) + "..."
		}
	}
	return strings.Join(out, "\n")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
