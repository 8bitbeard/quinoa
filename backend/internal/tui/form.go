package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// FormDone is sent when a form is successfully submitted.
type FormDone struct {
	Kind   string // "new-story" | "start-agent"
	Fields map[string]string
}

// FormCancelled is sent when the user presses Esc.
type FormCancelled struct{}

// ── New Story Form ────────────────────────────────────────────────────────────

type newStoryForm struct {
	fields  []textinput.Model
	focused int
	width   int
	height  int
}

func newNewStoryForm(width, height int) newStoryForm {
	title := textinput.New()
	title.Placeholder = "Ex: Implementar autenticação OAuth..."
	title.Focus()
	title.Width = width - 10

	desc := textinput.New()
	desc.Placeholder = "Descreva a tarefa em detalhes..."
	desc.Width = width - 10

	return newStoryForm{
		fields:  []textinput.Model{title, desc},
		focused: 0,
		width:   width,
		height:  height,
	}
}

func (f newStoryForm) Init() tea.Cmd { return textinput.Blink }

func (f newStoryForm) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Cancel):
			return f, func() tea.Msg { return FormCancelled{} }

		case key.Matches(msg, keys.Confirm):
			title := strings.TrimSpace(f.fields[0].Value())
			if title == "" {
				return f, nil
			}
			return f, func() tea.Msg {
				return FormDone{
					Kind: "new-story",
					Fields: map[string]string{
						"title":       title,
						"description": strings.TrimSpace(f.fields[1].Value()),
					},
				}
			}

		case key.Matches(msg, keys.NextField):
			f.fields[f.focused].Blur()
			f.focused = (f.focused + 1) % len(f.fields)
			f.fields[f.focused].Focus()
			return f, textinput.Blink

		case key.Matches(msg, keys.PrevField):
			f.fields[f.focused].Blur()
			f.focused = (f.focused - 1 + len(f.fields)) % len(f.fields)
			f.fields[f.focused].Focus()
			return f, textinput.Blink
		}
	}

	var cmd tea.Cmd
	f.fields[f.focused], cmd = f.fields[f.focused].Update(msg)
	return f, cmd
}

func (f newStoryForm) View() string {
	labels := []string{"Título *", "Descrição / Prompt"}
	var rows []string
	for i, fi := range f.fields {
		label := styleFormLabel.Render(labels[i])
		rows = append(rows, label+"\n"+fi.View())
	}

	hint := styleHint.Render("tab") + styleHint.Render(" campo  ") +
		styleHintKey.Render("ctrl+s") + styleHint.Render(" criar  ") +
		styleHintKey.Render("esc") + styleHint.Render(" cancelar")

	body := styleFormTitle.Render("Nova História") + "\n\n" +
		strings.Join(rows, "\n\n") + "\n\n" + hint

	w := f.width - 8
	if w < 40 {
		w = 40
	}
	return lipgloss.Place(f.width, f.height,
		lipgloss.Center, lipgloss.Center,
		styleFormBorder.Width(w).Render(body),
	)
}

// ── Start Agent Form ──────────────────────────────────────────────────────────

const (
	fieldRepoURL    = 0
	fieldRepoPath   = 1
	fieldRepoBranch = 2
	fieldAgentCmd   = 3
	fieldEnvExtra   = 4
	startFieldCount = 5
)

var agentPresets = []string{
	"claude --dangerously-skip-permissions",
	"aider --yes-always",
	"bash",
}

type startAgentForm struct {
	storyID    string
	storyTitle string
	fields     []textinput.Model
	focused    int
	preset     int
	width      int
	height     int
}

func newStartAgentForm(storyID, storyTitle string, width, height int) startAgentForm {
	mkInput := func(placeholder string, w int) textinput.Model {
		ti := textinput.New()
		ti.Placeholder = placeholder
		ti.Width = w
		return ti
	}
	fw := width - 12
	if fw < 30 {
		fw = 30
	}

	fields := []textinput.Model{
		mkInput("https://github.com/user/repo.git", fw),
		mkInput("/home/user/meu-projeto", fw),
		mkInput("main", fw),
		mkInput("claude --dangerously-skip-permissions", fw),
		mkInput("ANTHROPIC_API_KEY=sk-ant-...", fw),
	}
	fields[0].Focus()
	fields[fieldAgentCmd].SetValue(agentPresets[0])

	return startAgentForm{
		storyID:    storyID,
		storyTitle: storyTitle,
		fields:     fields,
		focused:    0,
		preset:     0,
		width:      width,
		height:     height,
	}
}

func (f startAgentForm) Init() tea.Cmd { return textinput.Blink }

func (f startAgentForm) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Cancel):
			return f, func() tea.Msg { return FormCancelled{} }

		case key.Matches(msg, keys.Confirm):
			agentCmd := strings.TrimSpace(f.fields[fieldAgentCmd].Value())
			if agentCmd == "" {
				return f, nil
			}
			repoURL := strings.TrimSpace(f.fields[fieldRepoURL].Value())
			repoPath := strings.TrimSpace(f.fields[fieldRepoPath].Value())
			if repoURL == "" && repoPath == "" {
				return f, nil
			}
			return f, func() tea.Msg {
				return FormDone{
					Kind: "start-agent",
					Fields: map[string]string{
						"story_id":      f.storyID,
						"agent_command": agentCmd,
						"repo_url":      repoURL,
						"repo_path":     repoPath,
						"repo_branch":   strings.TrimSpace(f.fields[fieldRepoBranch].Value()),
						"env_extra":     strings.TrimSpace(f.fields[fieldEnvExtra].Value()),
					},
				}
			}

		// Cycle agent presets with [ and ]
		case msg.String() == "]":
			f.preset = (f.preset + 1) % len(agentPresets)
			f.fields[fieldAgentCmd].SetValue(agentPresets[f.preset])
			return f, nil

		case msg.String() == "[":
			f.preset = (f.preset - 1 + len(agentPresets)) % len(agentPresets)
			f.fields[fieldAgentCmd].SetValue(agentPresets[f.preset])
			return f, nil

		case key.Matches(msg, keys.NextField):
			f.fields[f.focused].Blur()
			f.focused = (f.focused + 1) % startFieldCount
			f.fields[f.focused].Focus()
			return f, textinput.Blink

		case key.Matches(msg, keys.PrevField):
			f.fields[f.focused].Blur()
			f.focused = (f.focused - 1 + startFieldCount) % startFieldCount
			f.fields[f.focused].Focus()
			return f, textinput.Blink
		}
	}

	var cmd tea.Cmd
	f.fields[f.focused], cmd = f.fields[f.focused].Update(msg)
	return f, cmd
}

func (f startAgentForm) View() string {
	labels := []string{
		"URL do repositório (git clone)",
		"Pasta local (bind-mount)",
		"Branch (opcional)",
		fmt.Sprintf("Agente  [ ] ciclar preset (%d/%d)", f.preset+1, len(agentPresets)),
		"Env vars extras (KEY=VALUE, uma por linha)",
	}
	var rows []string
	for i, fi := range f.fields {
		label := styleFormLabel.Render(labels[i])
		rows = append(rows, label+"\n"+fi.View())
	}

	ref := styleHint.Render("▸ "+f.storyTitle)

	hint := styleHintKey.Render("tab") + styleHint.Render(" campo  ") +
		styleHintKey.Render("[/]") + styleHint.Render(" preset  ") +
		styleHintKey.Render("ctrl+s") + styleHint.Render(" iniciar  ") +
		styleHintKey.Render("esc") + styleHint.Render(" cancelar")

	body := styleFormTitle.Render("Iniciar Agente") + "\n" +
		ref + "\n\n" +
		strings.Join(rows, "\n\n") + "\n\n" + hint

	w := f.width - 8
	if w < 50 {
		w = 50
	}
	return lipgloss.Place(f.width, f.height,
		lipgloss.Center, lipgloss.Center,
		styleFormBorder.Width(w).Render(body),
	)
}
