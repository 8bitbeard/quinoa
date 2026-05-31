package tui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Left        key.Binding
	Right       key.Binding
	Up          key.Binding
	Down        key.Binding
	New         key.Binding
	Start       key.Binding
	AddAgent    key.Binding
	Approve     key.Binding
	Fix         key.Binding
	Reopen      key.Binding
	Stop        key.Binding
	Delete      key.Binding
	Expand      key.Binding
	Terminal    key.Binding
	Refresh     key.Binding
	Help        key.Binding
	Quit        key.Binding
	Refine      key.Binding
	SetVault    key.Binding
	SetProjects key.Binding
	Confirm     key.Binding
	Cancel      key.Binding
	Tab         key.Binding
	ShiftTab    key.Binding
	NextField   key.Binding
	PrevField   key.Binding
	OpenPicker  key.Binding
}

var keys = keyMap{
	Left: key.NewBinding(
		key.WithKeys("h", "left"),
		key.WithHelp("h/←", "coluna esq"),
	),
	Right: key.NewBinding(
		key.WithKeys("l", "right"),
		key.WithHelp("l/→", "coluna dir"),
	),
	Up: key.NewBinding(
		key.WithKeys("k", "up"),
		key.WithHelp("k/↑", "card acima"),
	),
	Down: key.NewBinding(
		key.WithKeys("j", "down"),
		key.WithHelp("j/↓", "card abaixo"),
	),
	New: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "nova história"),
	),
	Start: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "iniciar agente"),
	),
	AddAgent: key.NewBinding(
		key.WithKeys("A"),
		key.WithHelp("A", "adicionar agente"),
	),
	Approve: key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "aprovar → done"),
	),
	Fix: key.NewBinding(
		key.WithKeys("f"),
		key.WithHelp("f", "corrigir → doing"),
	),
	Reopen: key.NewBinding(
		key.WithKeys("b"),
		key.WithHelp("b", "reabrir → todo"),
	),
	Stop: key.NewBinding(
		key.WithKeys("p"),
		key.WithHelp("p", "parar agente"),
	),
	Delete: key.NewBinding(
		key.WithKeys("x", "delete"),
		key.WithHelp("x", "deletar"),
	),
	Expand: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "expandir"),
	),
	Terminal: key.NewBinding(
		key.WithKeys("t"),
		key.WithHelp("t", "abrir terminal"),
	),
	Refresh: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "refresh"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "ajuda"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "sair"),
	),
	Refine: key.NewBinding(
		key.WithKeys("R"),
		key.WithHelp("R", "refinar história"),
	),
	SetVault: key.NewBinding(
		key.WithKeys("V"),
		key.WithHelp("V", "selecionar vault"),
	),
	SetProjects: key.NewBinding(
		key.WithKeys("P"),
		key.WithHelp("P", "selecionar projetos"),
	),
	Confirm: key.NewBinding(
		key.WithKeys("ctrl+s"),
		key.WithHelp("ctrl+s", "confirmar"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancelar"),
	),
	Tab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "próximo campo"),
	),
	ShiftTab: key.NewBinding(
		key.WithKeys("shift+tab"),
		key.WithHelp("shift+tab", "campo anterior"),
	),
	NextField: key.NewBinding(
		key.WithKeys("tab"),
	),
	PrevField: key.NewBinding(
		key.WithKeys("shift+tab"),
	),
	OpenPicker: key.NewBinding(
		key.WithKeys("ctrl+o"),
		key.WithHelp("ctrl+o", "selecionar pasta"),
	),
}

// helpText returns the bottom hint bar for the given column.
// hasContainer indicates whether the focused card has a running container.
func helpText(colStatus string, hasContainer bool) string {
	base := styleHintKey.Render("n") + styleHint.Render(" nova  ") +
		styleHintKey.Render("r") + styleHint.Render(" refresh  ") +
		styleHintKey.Render("?") + styleHint.Render(" ajuda  ") +
		styleHintKey.Render("q") + styleHint.Render(" sair")

	termHint := ""
	if hasContainer {
		termHint = styleHintKey.Render("t") + styleHint.Render(" terminal  ")
	}

	switch colStatus {
	case "todo":
		return styleHintKey.Render("R") + styleHint.Render(" refinar  ") +
			styleHintKey.Render("s") + styleHint.Render(" iniciar  ") +
			styleHintKey.Render("x") + styleHint.Render(" deletar  ") +
			base
	case "refine":
		return termHint +
			styleHintKey.Render("s") + styleHint.Render(" implementar  ") +
			styleHintKey.Render("p") + styleHint.Render(" parar  ") +
			styleHintKey.Render("b") + styleHint.Render(" voltar  ") +
			base
	case "doing":
		return termHint +
			styleHintKey.Render("A") + styleHint.Render(" add agente  ") +
			styleHintKey.Render("p") + styleHint.Render(" parar  ") +
			base
	case "review":
		return termHint +
			styleHintKey.Render("a") + styleHint.Render(" aprovar  ") +
			styleHintKey.Render("f") + styleHint.Render(" corrigir  ") +
			styleHintKey.Render("b") + styleHint.Render(" reabrir  ") +
			base
	case "done":
		return termHint +
			styleHintKey.Render("b") + styleHint.Render(" reabrir  ") +
			styleHintKey.Render("x") + styleHint.Render(" deletar  ") +
			base
	}
	return base
}
