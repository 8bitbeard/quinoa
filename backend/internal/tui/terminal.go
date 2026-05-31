package tui

import (
	"context"
	"regexp"

	dtypes "github.com/docker/docker/api/types"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/wiltsou/quinoa/internal/docker"
)

// ── Messages ──────────────────────────────────────────────────────────────────

// termOutputMsg carries a chunk of raw PTY bytes for a specific container.
type termOutputMsg struct {
	containerID string
	data        []byte
}

// termClosedMsg is sent when the PTY reader reaches EOF (container exited).
type termClosedMsg struct{ containerID string }

// ── terminalPane ──────────────────────────────────────────────────────────────

const maxLines = 5000

// terminalPane holds the live state of one open terminal session.
// It survives view switches (board ↔ terminal) so the attach stays open.
type terminalPane struct {
	storyTitle  string
	containerID string
	docker      *docker.Client

	// Bidirectional Docker PTY attach kept open across view switches.
	attach dtypes.HijackedResponse
	closed bool // true when the container's PTY has reached EOF

	// Completed output lines (ANSI colours preserved, cursor movements stripped).
	lines   []string
	curLine []byte // accumulator for the current incomplete line

	// scrollOffset > 0 means the user has scrolled up; 0 = follow latest.
	scrollOffset int

	// Pane dimensions (set on every WindowSizeMsg).
	width  int
	height int
}

// newTerminalPane opens a Docker attach and returns the pane + the first
// listening command that feeds PTY output into the bubbletea loop.
func newTerminalPane(storyTitle, containerID string, d *docker.Client, w, h int) (*terminalPane, tea.Cmd, error) {
	attach, err := d.AttachTerminal(context.Background(), containerID)
	if err != nil {
		return nil, nil, err
	}

	// Replay buffered log history into the line buffer immediately.
	pane := &terminalPane{
		storyTitle:  storyTitle,
		containerID: containerID,
		docker:      d,
		attach:      attach,
		width:       w,
		height:      h,
	}

	if logs, err := d.GetLogs(context.Background(), containerID); err == nil {
		buf := make([]byte, 32*1024)
		for {
			n, readErr := logs.Read(buf)
			if n > 0 {
				pane.feed(buf[:n])
			}
			if readErr != nil {
				break
			}
		}
		logs.Close()
	}

	return pane, pane.listenCmd(), nil
}

// listenCmd returns a tea.Cmd that blocks until the next PTY chunk arrives,
// then sends a termOutputMsg (or termClosedMsg on EOF).
func (p *terminalPane) listenCmd() tea.Cmd {
	containerID := p.containerID
	reader := p.attach.Reader
	return func() tea.Msg {
		buf := make([]byte, 4096)
		n, err := reader.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])
			return termOutputMsg{containerID: containerID, data: data}
		}
		if err != nil {
			return termClosedMsg{containerID: containerID}
		}
		return termOutputMsg{containerID: containerID, data: nil}
	}
}

// feed appends raw PTY bytes to the line buffer.
// Handles \n (newline) and \r (carriage return / line overwrite).
func (p *terminalPane) feed(data []byte) {
	for _, b := range data {
		switch b {
		case '\n':
			line := stripMovement(string(p.curLine))
			p.lines = append(p.lines, line)
			if len(p.lines) > maxLines {
				p.lines = p.lines[len(p.lines)-maxLines:]
			}
			p.curLine = p.curLine[:0]
		case '\r':
			// Carriage return: discard current line content so the next
			// bytes overwrite from the beginning (progress-bar behaviour).
			p.curLine = p.curLine[:0]
		default:
			p.curLine = append(p.curLine, b)
		}
	}
}

// sendInput writes raw bytes to the container's stdin.
func (p *terminalPane) sendInput(data []byte) {
	if len(data) > 0 && !p.closed {
		_, _ = p.attach.Conn.Write(data)
	}
}

// resize notifies the container of new pane dimensions.
func (p *terminalPane) resize(w, h int) {
	p.width = w
	p.height = h
	if !p.closed {
		_ = p.docker.ResizeTerminal(context.Background(), p.containerID, uint(h), uint(w))
	}
}

// close shuts down the Docker attach connection.
func (p *terminalPane) close() {
	p.attach.Close()
	p.closed = true
}

// visibleLines returns the slice of lines that should be rendered given the
// current scroll offset and available height.
func (p *terminalPane) visibleLines(height int) []string {
	// Include the (possibly incomplete) current line as a preview.
	all := p.lines
	if len(p.curLine) > 0 {
		all = append(all, stripMovement(string(p.curLine)))
	}

	total := len(all)
	if total == 0 {
		return nil
	}

	// Clamp scroll offset so we never scroll past the beginning.
	maxOffset := total - 1
	if p.scrollOffset > maxOffset {
		p.scrollOffset = maxOffset
	}

	end := total - p.scrollOffset
	start := end - height
	if start < 0 {
		start = 0
	}
	return all[start:end]
}

// ── ANSI filtering ────────────────────────────────────────────────────────────

// movementRe matches ANSI escape sequences that move/position the cursor or
// alter terminal modes, but NOT SGR (colour/style) sequences ending in 'm'.
var movementRe = regexp.MustCompile(
	`\x1b\[[0-9;]*[ABCDEFJKH]` + // cursor up/down/left/right/position/erase
		`|\x1b\[[0-9;]*[hl]` + // mode set/reset (e.g. ?25h cursor hide)
		`|\x1b\[[0-9;]*r` + // scrolling region
		`|\x1b[=>]` + // keypad modes
		`|\x1b\][^\x07\x1b]*[\x07]` + // OSC sequences (title etc.)
		`|\x1b\][^\x1b]*\x1b\\`, // OSC with ST terminator
)

func stripMovement(s string) string {
	return movementRe.ReplaceAllString(s, "")
}

// ── Key → bytes ───────────────────────────────────────────────────────────────

// keyToBytes converts a bubbletea KeyMsg into the raw byte sequence that a
// terminal would normally emit for that keystroke, so it can be forwarded
// verbatim to the container's PTY stdin.
func keyToBytes(msg tea.KeyMsg) []byte {
	alt := msg.Alt

	switch msg.Type {
	case tea.KeyRunes:
		raw := []byte(string(msg.Runes))
		if alt {
			return append([]byte{0x1b}, raw...)
		}
		return raw

	case tea.KeySpace:
		if alt {
			return []byte{0x1b, ' '}
		}
		return []byte{' '}

	// Cursor keys
	case tea.KeyUp:
		if alt {
			return []byte{0x1b, 0x1b, '[', 'A'}
		}
		return []byte{0x1b, '[', 'A'}
	case tea.KeyDown:
		if alt {
			return []byte{0x1b, 0x1b, '[', 'B'}
		}
		return []byte{0x1b, '[', 'B'}
	case tea.KeyRight:
		if alt {
			return []byte{0x1b, 0x1b, '[', 'C'}
		}
		return []byte{0x1b, '[', 'C'}
	case tea.KeyLeft:
		if alt {
			return []byte{0x1b, 0x1b, '[', 'D'}
		}
		return []byte{0x1b, '[', 'D'}

	// Navigation
	case tea.KeyHome:
		return []byte{0x1b, '[', 'H'}
	case tea.KeyEnd:
		return []byte{0x1b, '[', 'F'}
	case tea.KeyPgUp:
		return []byte{0x1b, '[', '5', '~'}
	case tea.KeyPgDown:
		return []byte{0x1b, '[', '6', '~'}
	case tea.KeyDelete:
		return []byte{0x1b, '[', '3', '~'}
	case tea.KeyInsert:
		return []byte{0x1b, '[', '2', '~'}
	case tea.KeyShiftTab:
		return []byte{0x1b, '[', 'Z'}

	// Function keys
	case tea.KeyF1:
		return []byte{0x1b, 'O', 'P'}
	case tea.KeyF2:
		return []byte{0x1b, 'O', 'Q'}
	case tea.KeyF3:
		return []byte{0x1b, 'O', 'R'}
	case tea.KeyF4:
		return []byte{0x1b, 'O', 'S'}
	case tea.KeyF5:
		return []byte{0x1b, '[', '1', '5', '~'}
	case tea.KeyF6:
		return []byte{0x1b, '[', '1', '7', '~'}
	case tea.KeyF7:
		return []byte{0x1b, '[', '1', '8', '~'}
	case tea.KeyF8:
		return []byte{0x1b, '[', '1', '9', '~'}
	case tea.KeyF9:
		return []byte{0x1b, '[', '2', '0', '~'}
	case tea.KeyF10:
		return []byte{0x1b, '[', '2', '1', '~'}
	case tea.KeyF11:
		return []byte{0x1b, '[', '2', '3', '~'}
	case tea.KeyF12:
		return []byte{0x1b, '[', '2', '4', '~'}

	default:
		// Control characters 0–127: the KeyType value IS the byte.
		if msg.Type >= 0 && int(msg.Type) < 128 {
			b := byte(msg.Type)
			if alt {
				return []byte{0x1b, b}
			}
			return []byte{b}
		}
	}
	return nil
}

// renderTermLine truncates a line to fit within the given display width,
// using ANSI-aware measurement so colour codes don't count as characters.
func renderTermLine(line string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(line, width, "")
}
