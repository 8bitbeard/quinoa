package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorBg      = lipgloss.Color("#1a1b26")
	colorBg2     = lipgloss.Color("#24283b")
	colorBg3     = lipgloss.Color("#2f3549")
	colorBorder  = lipgloss.Color("#3b4261")
	colorMuted   = lipgloss.Color("#565f89")
	colorText    = lipgloss.Color("#c0caf5")
	colorAccent  = lipgloss.Color("#7aa2f7")
	colorGreen   = lipgloss.Color("#9ece6a")
	colorYellow  = lipgloss.Color("#e0af68")
	colorRed     = lipgloss.Color("#f7768e")
	colorOrange  = lipgloss.Color("#ff9e64")
	colorCyan    = lipgloss.Color("#7dcfff")

	styleBase = lipgloss.NewStyle().
			Foreground(colorText).
			Background(colorBg)

	styleColHeader = lipgloss.NewStyle().
			Foreground(colorMuted).
			Bold(true)

	styleColHeaderReview = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	styleCard = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	styleCardSelected = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorAccent).
				Padding(0, 1)

	styleCardTitle = lipgloss.NewStyle().
			Foreground(colorText).
			Bold(true)

	styleCardDesc = lipgloss.NewStyle().
			Foreground(colorMuted)

	styleStatusRunning = lipgloss.NewStyle().
				Foreground(colorGreen)

	styleStatusIdle = lipgloss.NewStyle().
			Foreground(colorYellow)

	styleStatusError = lipgloss.NewStyle().
			Foreground(colorRed)

	styleStatusPending = lipgloss.NewStyle().
				Foreground(colorMuted)

	styleStatusStopped = lipgloss.NewStyle().
				Foreground(colorOrange)

	styleStatusDone = lipgloss.NewStyle().
			Foreground(colorCyan)

	styleHint = lipgloss.NewStyle().
			Foreground(colorMuted)

	styleHintKey = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	styleColEmpty = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true)

	styleFormLabel = lipgloss.NewStyle().
			Foreground(colorMuted)

	styleFormTitle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	styleFormBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorAccent).
			Padding(1, 2)

	styleBadge = lipgloss.NewStyle().
			Padding(0, 1).
			Bold(true)

	styleColFocused = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)
)

func statusStyle(status string) lipgloss.Style {
	switch status {
	case "running":
		return styleStatusRunning
	case "idle":
		return styleStatusIdle
	case "error":
		return styleStatusError
	case "pending":
		return styleStatusPending
	case "stopped":
		return styleStatusStopped
	case "done":
		return styleStatusDone
	default:
		return styleStatusPending
	}
}

func statusLabel(status string) string {
	switch status {
	case "running":
		return "● running"
	case "idle":
		return "○ idle"
	case "error":
		return "✗ error"
	case "pending":
		return "· pending"
	case "stopped":
		return "■ stopped"
	case "done":
		return "✓ done"
	default:
		return status
	}
}
