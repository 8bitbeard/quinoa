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

// stageOrder defines the display order and label for each stage.
var stageOrder = []struct {
	key   string
	label string
}{
	{"refine", "Refinamento"},
	{"doing", "Implementação"},
	{"review", "Revisão"},
}

// stageLabel returns the human-readable label for a stage key.
func stageLabel(stage string) string {
	for _, s := range stageOrder {
		if s.key == stage {
			return s.label
		}
	}
	return "Outros"
}

// agentDisplayName returns a display name for the task, falling back to
// parsing the agent command if agent_name is not stored.
func agentDisplayName(t *db.Task) string {
	if t.AgentName != "" {
		return t.AgentName
	}
	return inferAgentName(t.AgentCommand)
}

// inferAgentName derives a role label from the agent command text (backward compat).
func inferAgentName(agentCommand string) string {
	lower := strings.ToLower(agentCommand)
	switch {
	case strings.Contains(lower, "product manager especialista em refinamento"):
		return "PM"
	case strings.Contains(lower, "tech lead responsável por orquestrar"):
		return "TechLead"
	case strings.Contains(lower, "tech lead responsável pela revisão"):
		return "TechLead"
	case strings.Contains(lower, "você é um agente de qa"):
		return "QA"
	case strings.Contains(lower, "você é um agente de segurança"):
		return "Segurança"
	case strings.Contains(lower, "frontend") || strings.Contains(lower, "front-end"):
		return "Frontend"
	case strings.Contains(lower, "backend") || strings.Contains(lower, "back-end"):
		return "Backend"
	case strings.Contains(lower, "banco de dados") || strings.Contains(lower, "migration"):
		return "Banco de Dados"
	default:
		return "Agente"
	}
}

// inferStage derives the stage from the agent command (backward compat for old tasks).
func inferStage(agentCommand string) string {
	lower := strings.ToLower(agentCommand)
	switch {
	case strings.Contains(lower, "product manager especialista em refinamento"):
		return "refine"
	case strings.Contains(lower, "tech lead responsável pela revisão") ||
		strings.Contains(lower, "você é um agente de qa") ||
		strings.Contains(lower, "você é um agente de segurança"):
		return "review"
	default:
		return "doing"
	}
}

func (p taskPicker) View() string {
	// Group tasks by stage in display order.
	type group struct {
		stage string
		tasks []*db.Task
	}
	stageMap := map[string][]*db.Task{}
	for _, t := range p.tasks {
		stage := t.Stage
		if stage == "" {
			stage = inferStage(t.AgentCommand)
		}
		stageMap[stage] = append(stageMap[stage], t)
	}

	// Build ordered groups (only stages that have tasks).
	var groups []group
	for _, s := range stageOrder {
		if ts, ok := stageMap[s.key]; ok {
			groups = append(groups, group{s.key, ts})
		}
	}
	// Append any unknown stages.
	for stage, ts := range stageMap {
		known := false
		for _, s := range stageOrder {
			if s.key == stage {
				known = true
				break
			}
		}
		if !known {
			groups = append(groups, group{stage, ts})
		}
	}

	// Track which flat index maps to which task for selection.
	// (p.selected indexes into p.tasks which is already ordered by created_at)
	// We need to map task ID → flat index in p.tasks.
	taskIdx := map[string]int{}
	for i, t := range p.tasks {
		taskIdx[t.ID] = i
	}

	// Count agents per name within each group for sequential labeling.
	var rows []string
	for _, g := range groups {
		headerStyle := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
		rows = append(rows, headerStyle.Render("── "+stageLabel(g.stage)+" ──"))

		nameCount := map[string]int{}
		for _, t := range g.tasks {
			nameCount[agentDisplayName(t)]++
		}
		nameSeq := map[string]int{}

		for _, t := range g.tasks {
			idx := taskIdx[t.ID]
			icon, iconStyle := taskStatusIcon(t.Status)
			cursor := "  "
			rowStyle := lipgloss.NewStyle()
			if idx == p.selected {
				cursor = styleHintKey.Render("▶ ")
				rowStyle = lipgloss.NewStyle().Bold(true)
			}

			name := agentDisplayName(t)
			label := name
			if nameCount[name] > 1 {
				nameSeq[name]++
				label = fmt.Sprintf("%s %d", name, nameSeq[name])
			}

			containerHint := ""
			if t.ContainerID != "" {
				containerHint = "  " + styleCardDesc.Render(t.ContainerID[:min8(t.ContainerID)])
			}

			row := cursor + iconStyle.Render(icon+" "+label) + containerHint
			rows = append(rows, rowStyle.Render(row))
		}
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
