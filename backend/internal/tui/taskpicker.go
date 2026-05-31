package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/wiltsou/quinoa/internal/db"
)

// taskPickerSelected is sent when the user chooses a task.
type taskPickerSelected struct{ task *db.Task }

// taskPickerCancelled is sent when the user dismisses the picker.
type taskPickerCancelled struct{}

type taskPicker struct {
	tasks    []*db.Task
	selected int
	width    int
	height   int
}

func newTaskPicker(tasks []*db.Task, width, height int) taskPicker {
	return taskPicker{tasks: tasks, width: width, height: height}
}

func (p taskPicker) Init() tea.Cmd { return nil }

func (p taskPicker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Cancel):
			return p, func() tea.Msg { return taskPickerCancelled{} }
		case key.Matches(msg, keys.Up):
			if p.selected > 0 {
				p.selected--
			}
		case key.Matches(msg, keys.Down):
			if p.selected < len(p.tasks)-1 {
				p.selected++
			}
		case key.Matches(msg, keys.Expand): // enter
			task := p.tasks[p.selected]
			return p, func() tea.Msg { return taskPickerSelected{task: task} }
		}
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
	}
	return p, nil
}

func (p taskPicker) View() string {
	var rows []string
	for i, t := range p.tasks {
		icon, iconStyle := taskStatusIcon(t.Status)
		cursor := "  "
		rowStyle := lipgloss.NewStyle()
		if i == p.selected {
			cursor = styleHintKey.Render("▶ ")
			rowStyle = lipgloss.NewStyle().Bold(true)
		}

		label := fmt.Sprintf("Agente %d", i+1)
		agentCmd := truncate(t.AgentCommand, 40)
		containerHint := ""
		if t.ContainerID != "" {
			containerHint = "  " + styleCardDesc.Render(t.ContainerID[:min8(t.ContainerID)])
		}

		row := cursor + iconStyle.Render(icon+" "+label) +
			styleHint.Render("  "+agentCmd) +
			containerHint

		rows = append(rows, rowStyle.Render(row))
	}

	hint := styleHintKey.Render("↑↓") + styleHint.Render(" navegar  ") +
		styleHintKey.Render("enter") + styleHint.Render(" abrir  ") +
		styleHintKey.Render("esc") + styleHint.Render(" cancelar")

	body := styleFormTitle.Render("Selecionar Terminal") + "\n\n" +
		strings.Join(rows, "\n") + "\n\n" + hint

	w := p.width - 8
	if w < 50 {
		w = 50
	}
	return lipgloss.Place(p.width, p.height,
		lipgloss.Center, lipgloss.Center,
		styleFormBorder.Width(w).Render(body),
	)
}

func taskStatusIcon(status string) (string, lipgloss.Style) {
	switch status {
	case "running":
		return "▶", lipgloss.NewStyle().Foreground(colorAccent)
	case "idle":
		return "⏸", lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	case "error":
		return "✗", lipgloss.NewStyle().Foreground(colorRed)
	case "stopped":
		return "⏹", lipgloss.NewStyle().Foreground(colorRed)
	default:
		return "⧖", lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	}
}

func min8(s string) int {
	if len(s) < 8 {
		return len(s)
	}
	return 8
}
