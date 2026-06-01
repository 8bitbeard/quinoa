package tui

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
type termDetachedMsg struct{}
type vaultLoadedMsg struct{ path string }
type vaultInfoMsg struct{ noteCount int }
type projectsLoadedMsg struct{ path string }
type projectsInfoMsg struct{ count int }
type statusClearMsg struct{}

// ── Board columns ─────────────────────────────────────────────────────────────

const (
	colTodo   = 0
	colRefine = 1
	colDoing  = 2
	colReview = 3
	colDone   = 4
)

var columns = []string{"todo", "refine", "doing", "review", "done"}

// ── Model ─────────────────────────────────────────────────────────────────────

type Model struct {
	db      *db.DB
	runner  *Runner
	stories []*db.Story

	// board state
	col    int
	cursor int
	cols   [5][]*db.Story

	// overlay / forms
	showHelp   bool
	activeForm tea.Model
	formKind   string
	taskPicker *taskPicker // non-nil when terminal picker overlay is open

	// vault
	vaultPath      string
	vaultNoteCount int        // -1 = not yet counted
	vaultPicker    *dirPicker // non-nil when the V picker overlay is open

	// projects
	projectsPath   string
	projectsCount  int        // -1 = not yet counted
	projectsPicker *dirPicker // non-nil when the P picker overlay is open

	// dimensions
	width  int
	height int

	// expanded card description
	expanded bool

	// transient status/error banner (auto-clears after 5s)
	statusMsg string
}

func NewModel(database *db.DB, runner *Runner) Model {
	return Model{
		db:             database,
		runner:         runner,
		vaultNoteCount: -1,
		projectsCount:  -1,
	}
}

func countVaultNotes(vaultPath string) tea.Cmd {
	return func() tea.Msg {
		count := 0
		_ = filepath.Walk(vaultPath, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if !info.IsDir() && strings.HasSuffix(strings.ToLower(info.Name()), ".md") {
				count++
			}
			return nil
		})
		return vaultInfoMsg{noteCount: count}
	}
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.loadStories(),
		m.loadVaultConfig(),
		m.loadProjectsConfig(),
		tickEvery(3*time.Second),
	)
}

func (m Model) loadVaultConfig() tea.Cmd {
	return func() tea.Msg {
		path, _ := m.db.GetConfig(db.ConfigVaultPath)
		return vaultLoadedMsg{path: path}
	}
}

func (m Model) loadProjectsConfig() tea.Cmd {
	return func() tea.Msg {
		path, _ := m.db.GetConfig(db.ConfigProjectsPath)
		return projectsLoadedMsg{path: path}
	}
}

func countProjects(projectsPath string) tea.Cmd {
	return func() tea.Msg {
		entries, err := os.ReadDir(projectsPath)
		if err != nil {
			return projectsInfoMsg{count: 0}
		}
		count := 0
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				count++
			}
		}
		return projectsInfoMsg{count: count}
	}
}

func tickEvery(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func clearStatusAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return statusClearMsg{} })
}

func (m *Model) setStatus(msg string) tea.Cmd {
	m.statusMsg = msg
	return clearStatusAfter(5 * time.Second)
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
	// ── Task picker overlay (terminal selection) ──────────────────────────
	if m.taskPicker != nil {
		switch msg := msg.(type) {
		case taskPickerSelected:
			m.taskPicker = nil
			return m.openTerminalForTask(msg.task)
		case taskPickerCancelled:
			m.taskPicker = nil
			return m, nil
		default:
			pickerModel, cmd := m.taskPicker.Update(msg)
			p := pickerModel.(taskPicker)
			m.taskPicker = &p
			return m, cmd
		}
	}

	// ── Vault picker overlay ──────────────────────────────────────────────
	if m.vaultPicker != nil {
		switch msg := msg.(type) {
		case DirPickerDone:
			m.vaultPath = msg.Path
			m.vaultNoteCount = -1
			m.vaultPicker = nil
			cmds := []tea.Cmd{
				func() tea.Msg {
					_ = m.db.SetConfig(db.ConfigVaultPath, msg.Path)
					return nil
				},
				countVaultNotes(msg.Path),
			}
			// After vault is set, prompt for projects folder if not yet configured.
			if m.projectsPath == "" {
				dp, cmd := newDirPickerWithTitle("Selecionar Pasta de Projetos", "", m.width, m.height)
				m.projectsPicker = &dp
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		case DirPickerCancelled:
			m.vaultPicker = nil
			// Vault skipped — still prompt for projects if not configured.
			if m.projectsPath == "" {
				dp, cmd := newDirPickerWithTitle("Selecionar Pasta de Projetos", "", m.width, m.height)
				m.projectsPicker = &dp
				return m, cmd
			}
			return m, nil
		case tea.WindowSizeMsg:
			m.width = msg.Width
			m.height = msg.Height
			m.vaultPicker.width = msg.Width
			m.vaultPicker.height = msg.Height
		default:
			pickerModel, cmd := m.vaultPicker.Update(msg)
			p := pickerModel.(dirPicker)
			m.vaultPicker = &p
			return m, cmd
		}
		return m, nil
	}

	// ── Projects picker overlay ───────────────────────────────────────────
	if m.projectsPicker != nil {
		switch msg := msg.(type) {
		case DirPickerDone:
			m.projectsPath = msg.Path
			m.projectsCount = -1
			m.projectsPicker = nil
			return m, tea.Batch(
				func() tea.Msg {
					_ = m.db.SetConfig(db.ConfigProjectsPath, msg.Path)
					return nil
				},
				countProjects(msg.Path),
			)
		case DirPickerCancelled:
			m.projectsPicker = nil
			return m, nil
		case tea.WindowSizeMsg:
			m.width = msg.Width
			m.height = msg.Height
			m.projectsPicker.width = msg.Width
			m.projectsPicker.height = msg.Height
		default:
			pickerModel, cmd := m.projectsPicker.Update(msg)
			p := pickerModel.(dirPicker)
			m.projectsPicker = &p
			return m, cmd
		}
		return m, nil
	}

	// ── Form overlay (forms + file picker) ───────────────────────────────
	if m.activeForm != nil {
		switch msg := msg.(type) {
		case FormCancelled, FilePickerCancelled:
			m.activeForm = nil
			return m, nil
		case FormDone:
			return m.handleFormDone(msg)
		case FilePickerDone:
			return m.handleFilePickerDone(msg)
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

	// ── Common events handled in all views ────────────────────────────────
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

	case termDetachedMsg:
		return m, m.loadStories()

	case vaultLoadedMsg:
		m.vaultPath = msg.path
		if msg.path == "" {
			dp, cmd := newDirPickerWithTitle("Selecionar Vault Obsidian", "", m.width, m.height)
			m.vaultPicker = &dp
			return m, cmd
		}
		return m, countVaultNotes(msg.path)

	case vaultInfoMsg:
		m.vaultNoteCount = msg.noteCount

	case projectsLoadedMsg:
		m.projectsPath = msg.path
		if msg.path == "" {
			// Only auto-open projects picker if the vault picker isn't already shown.
			if m.vaultPicker == nil {
				dp, cmd := newDirPickerWithTitle("Selecionar Pasta de Projetos", "", m.width, m.height)
				m.projectsPicker = &dp
				return m, cmd
			}
		} else {
			return m, countProjects(msg.path)
		}

	case projectsInfoMsg:
		m.projectsCount = msg.count

	case errMsg:
		log.Printf("error: %v", msg.err)
		return m, m.setStatus("erro: " + msg.err.Error())

	case statusClearMsg:
		m.statusMsg = ""

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m *Model) rebuildCols() {
	m.cols = [5][]*db.Story{}
	for _, s := range m.stories {
		switch s.KanbanStatus {
		case "todo":
			m.cols[colTodo] = append(m.cols[colTodo], s)
		case "refine":
			m.cols[colRefine] = append(m.cols[colRefine], s)
		case "doing":
			m.cols[colDoing] = append(m.cols[colDoing], s)
		case "review":
			m.cols[colReview] = append(m.cols[colReview], s)
		case "done":
			m.cols[colDone] = append(m.cols[colDone], s)
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

// ── Board key handler ─────────────────────────────────────────────────────────

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, keys.SetVault):
		dp, cmd := newDirPickerWithTitle("Selecionar Vault Obsidian", m.vaultPath, m.width, m.height)
		m.vaultPicker = &dp
		return m, cmd

	case key.Matches(msg, keys.SetProjects):
		dp, cmd := newDirPickerWithTitle("Selecionar Pasta de Projetos", m.projectsPath, m.width, m.height)
		m.projectsPicker = &dp
		return m, cmd

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
		if m.col < colDone {
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
		if m.vaultPath != "" {
			fp, cmd := newFilePicker(m.vaultPath, m.width, m.height)
			m.activeForm = fp
			m.formKind = "file-picker"
			return m, cmd
		}
		f := newNewStoryForm(m.width, m.height)
		m.activeForm = f
		m.formKind = "new-story"
		return m, f.Init()

	case key.Matches(msg, keys.Refine):
		s := m.selectedStory()
		if s == nil || m.col != colTodo {
			break
		}
		prd := m.computePRDPath(s.ID, s.Title)
		f := newStartRefineForm(s.ID, s.Title, prd, m.width, m.height)
		m.activeForm = f
		m.formKind = "start-refine"
		return m, f.Init()

	case key.Matches(msg, keys.Start):
		s := m.selectedStory()
		if s == nil {
			break
		}
		if m.col == colTodo || m.col == colRefine {
			f := newStartAgentForm(s.ID, s.Title, m.width, m.height)
			m.activeForm = f
			m.formKind = "start-agent"
			return m, f.Init()
		}

	case key.Matches(msg, keys.Approve):
		s := m.selectedStory()
		if s != nil && m.col == colReview {
			triggerVaultUpdate(m.vaultPath, s.Title, s.Description, s.PrdPath, "Implementação aceita")
			return m, m.moveStory(s, "done")
		}

	case key.Matches(msg, keys.Fix):
		s := m.selectedStory()
		if s != nil && m.col == colReview {
			return m, m.moveStory(s, "doing")
		}

	case key.Matches(msg, keys.Reopen):
		s := m.selectedStory()
		if s != nil && (m.col == colReview || m.col == colDone) {
			return m, m.moveStory(s, "todo")
		}

	case key.Matches(msg, keys.Stop):
		s := m.selectedStory()
		if s != nil && (m.col == colDoing || m.col == colRefine) {
			return m, m.stopStory(s)
		}

	case key.Matches(msg, keys.Delete):
		s := m.selectedStory()
		if s != nil && (m.col == colTodo || m.col == colDone) {
			return m, m.deleteStory(s)
		}

	case key.Matches(msg, keys.Terminal):
		s := m.selectedStory()
		if s == nil {
			break
		}
		return m.openTerminalForStory(s)

	case key.Matches(msg, keys.AddAgent):
		s := m.selectedStory()
		if s == nil || m.col != colDoing {
			break
		}
		f := newStartAgentForm(s.ID, s.Title, m.width, m.height)
		f.kind = "add-agent"
		m.activeForm = f
		m.formKind = "add-agent"
		return m, f.Init()
	}

	return m, nil
}

// ── Actions ───────────────────────────────────────────────────────────────────

func (m Model) handleFilePickerDone(msg FilePickerDone) (tea.Model, tea.Cmd) {
	m.activeForm = nil
	return m, func() tea.Msg {
		s := &db.Story{
			ID:           uuid.New().String(),
			Title:        msg.Title,
			Description:  msg.Content,
			KanbanStatus: "todo",
		}
		if err := m.db.InsertStory(s); err != nil {
			return errMsg{err}
		}
		stories, _ := m.db.ListStories()
		return storiesLoadedMsg(stories)
	}
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

	case "start-refine":
		vaultPath := m.vaultPath
		projectsPath := m.projectsPath
		return m, func() tea.Msg {
			storyID := msg.Fields["story_id"]
			story, err := m.db.GetStory(storyID)
			if err != nil {
				return errMsg{err}
			}

			// Stop any existing task for this story before creating a new refine task.
			m.runner.StopStoryTasks(storyID)
			if story.TaskID != "" {
				if existing, err := m.db.GetTask(story.TaskID); err == nil {
					m.runner.StopTask(existing)
				}
			}

			hostPRD := msg.Fields["prd_path"]
			if hostPRD != "" {
				_ = os.MkdirAll(filepath.Dir(hostPRD), 0755)
				_ = m.db.UpdateStoryPRD(storyID, hostPRD)
			}

			// Compute the PRD path as seen from inside the container.
			containerPRD := ""
			if vaultPath != "" && hostPRD != "" {
				if rel := strings.TrimPrefix(hostPRD, vaultPath); rel != hostPRD {
					containerPRD = "/vault" + rel
				}
			}

			prompt := buildRefinementPrompt(story.Title, story.Description, containerPRD, vaultPath != "", projectsPath != "")
			baseCmd := msg.Fields["agent_command"]
			agentCmd := baseCmd + " " + shellQuote(prompt)

			var envExtra []string
			for _, line := range strings.Split(msg.Fields["env_extra"], "\n") {
				line = strings.TrimSpace(line)
				if strings.Contains(line, "=") {
					envExtra = append(envExtra, line)
				}
			}
			if containerPRD != "" {
				envExtra = append(envExtra, "PRD_PATH="+containerPRD)
			}
			envExtra = append(envExtra, "QUINOA_MODE=refine")

			taskID := uuid.New().String()
			task, err := m.runner.StartTask(docker.RunConfig{
				TaskID:       taskID,
				StoryID:      storyID,
				AgentCommand: agentCmd,
				BaseCommand:  baseCmd,
				EnvExtra:     envExtra,
				VaultPath:    vaultPath,
				ProjectsPath: projectsPath,
			})
			if err != nil {
				return errMsg{err}
			}

			if err := m.db.UpdateStoryKanban(storyID, "refine", task.ID); err != nil {
				return errMsg{err}
			}

			stories, _ := m.db.ListStories()
			return storiesLoadedMsg(stories)
		}

	case "start-agent":
		vaultPath := m.vaultPath
		projectsPath := m.projectsPath
		return m, func() tea.Msg {
			storyID := msg.Fields["story_id"]
			story, err := m.db.GetStory(storyID)
			if err != nil {
				return errMsg{err}
			}

			// If coming from refine, stop the refinement task first.
			if story.KanbanStatus == "refine" && story.TaskID != "" {
				if task, err := m.db.GetTask(story.TaskID); err == nil {
					m.runner.StopTask(task)
				}
			}

			// Compute PRD path inside the container (empty if vault not set).
			containerPRD := ""
			if vaultPath != "" && story.PrdPath != "" {
				if rel := strings.TrimPrefix(story.PrdPath, vaultPath); rel != story.PrdPath {
					containerPRD = "/vault" + rel
				}
			}

			prompt := buildTechLeadDoingPrompt(story.Title, story.Description, containerPRD, projectsPath != "", false)
			baseCmd := msg.Fields["agent_command"]
			agentCmd := baseCmd + " " + shellQuote(prompt)

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
				StoryID:      storyID,
				AgentCommand: agentCmd,
				BaseCommand:  baseCmd,
				EnvExtra:     envExtra,
				VaultPath:    vaultPath,
				ProjectsPath: projectsPath,
			})
			if err != nil {
				return errMsg{err}
			}

			if err := m.db.UpdateStoryKanban(storyID, "doing", task.ID); err != nil {
				return errMsg{err}
			}
			_ = m.db.IncrDoingCount(storyID)

			stories, _ := m.db.ListStories()
			return storiesLoadedMsg(stories)
		}

	case "add-agent":
		vaultPath := m.vaultPath
		projectsPath := m.projectsPath
		return m, func() tea.Msg {
			storyID := msg.Fields["story_id"]
			story, err := m.db.GetStory(storyID)
			if err != nil {
				return errMsg{err}
			}

			containerPRD := ""
			if vaultPath != "" && story.PrdPath != "" {
				if rel := strings.TrimPrefix(story.PrdPath, vaultPath); rel != story.PrdPath {
					containerPRD = "/vault" + rel
				}
			}

			prompt := buildTechLeadDoingPrompt(story.Title, story.Description, containerPRD, projectsPath != "", false)
			baseCmd := msg.Fields["agent_command"]
			agentCmd := baseCmd + " " + shellQuote(prompt)

			var envExtra []string
			for _, line := range strings.Split(msg.Fields["env_extra"], "\n") {
				line = strings.TrimSpace(line)
				if strings.Contains(line, "=") {
					envExtra = append(envExtra, line)
				}
			}

			taskID := uuid.New().String()
			_, err = m.runner.StartTask(docker.RunConfig{
				TaskID:       taskID,
				StoryID:      storyID,
				AgentCommand: agentCmd,
				BaseCommand:  baseCmd,
				EnvExtra:     envExtra,
				VaultPath:    vaultPath,
				ProjectsPath: projectsPath,
			})
			if err != nil {
				return errMsg{err}
			}

			// Story stays in "doing"; additional task is linked via story_id.
			stories, _ := m.db.ListStories()
			return storiesLoadedMsg(stories)
		}
	}

	return m, nil
}

// computePRDPath returns the host filesystem path for a story's PRD.
func (m Model) computePRDPath(storyID, storyTitle string) string {
	base := m.vaultPath
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".quinoa")
	}
	return filepath.Join(base, "PRDs", slugifyTitle(storyTitle)+".md")
}

// containerPRDPath converts a host PRD path to its path inside the container.
// The vault is mounted at /vault in the container.
func (m Model) containerPRDPath(hostPRD string) string {
	if hostPRD == "" || m.vaultPath == "" {
		return ""
	}
	rel := strings.TrimPrefix(hostPRD, m.vaultPath)
	return "/vault" + rel
}

// openTerminalForStory opens the terminal for a story's agent(s).
// Includes stopped/error containers so the user can inspect logs after a failure.
// If multiple tasks have containers it shows a picker; otherwise opens directly.
func (m Model) openTerminalForStory(s *db.Story) (tea.Model, tea.Cmd) {
	tasks, _ := m.db.ListTasksByStory(s.ID)

	// Collect any task that has a container, regardless of status.
	var active []*db.Task
	for _, t := range tasks {
		if t.ContainerID != "" {
			active = append(active, t)
		}
	}
	// Fallback: primary task_id (covers stories whose task_id isn't story-linked yet).
	if len(active) == 0 && s.TaskID != "" {
		if t, err := m.db.GetTask(s.TaskID); err == nil && t.ContainerID != "" {
			active = append(active, t)
		}
	}

	if len(active) == 0 {
		return m, nil
	}
	if len(active) == 1 {
		return m.openTerminalForTask(active[0])
	}

	// Multiple agents: show picker.
	p := newTaskPicker(active, m.width, m.height)
	m.taskPicker = &p
	return m, nil
}

func (m Model) openTerminalForTask(task *db.Task) (tea.Model, tea.Cmd) {
	if task.ContainerID == "" {
		return m, nil
	}
	var cmd *exec.Cmd
	if task.Status == "error" || task.Status == "stopped" {
		// Container has exited — show logs through a pager so the user can scroll.
		cmd = exec.Command("sh", "-c", "docker logs "+task.ContainerID+" 2>&1 | less -r")
	} else {
		// Container is (likely) running — attach interactively.
		cmd = exec.Command("docker", "attach",
			"--detach-keys=ctrl-q",
			task.ContainerID,
		)
	}
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return termDetachedMsg{}
	})
}

func (m Model) moveStory(s *db.Story, to string) tea.Cmd {
	return func() tea.Msg {
		taskID := s.TaskID
		switch to {
		case "todo":
			// Stop all agents linked to this story.
			m.runner.StopStoryTasks(s.ID)
			taskID = ""
		case "doing":
			if s.TaskID != "" {
				if task, err := m.db.GetTask(s.TaskID); err == nil && task.ContainerID != "" {
					_ = m.db.UpdateTaskStatus(task.ID, "running", task.ContainerID)
				}
			}
		case "done":
			// Stop all agents and keep primary task reference.
			m.runner.StopStoryTasks(s.ID)
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

// ── View ──────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.taskPicker != nil {
		return m.taskPicker.View()
	}
	if m.vaultPicker != nil {
		return m.vaultPicker.View()
	}
	if m.projectsPicker != nil {
		return m.projectsPicker.View()
	}
	if m.activeForm != nil {
		return m.activeForm.View()
	}
	if m.showHelp {
		return m.helpView()
	}
	return m.boardView()
}

// ── Board view ────────────────────────────────────────────────────────────────

func (m Model) boardView() string {
	if m.width == 0 {
		return "carregando..."
	}

	header := m.renderHeader()
	hint := m.renderHint()
	hintH := lipgloss.Height(hint)
	headerH := lipgloss.Height(header)

	var statusBar string
	statusBarH := 0
	if m.statusMsg != "" {
		statusBar = m.renderStatusBar()
		statusBarH = lipgloss.Height(statusBar)
	}

	vaultBar := m.renderVaultBar()
	vaultBarH := 0
	if vaultBar != "" {
		vaultBarH = lipgloss.Height(vaultBar)
	}

	projectsBar := m.renderProjectsBar()
	projectsBarH := 0
	if projectsBar != "" {
		projectsBarH = lipgloss.Height(projectsBar)
	}

	boardH := m.height - headerH - hintH - statusBarH - vaultBarH - projectsBarH - 1
	if boardH < 5 {
		boardH = 5
	}

	board := m.renderBoard(m.width, boardH)

	parts := []string{header}
	if statusBarH > 0 {
		parts = append(parts, statusBar)
	}
	if vaultBarH > 0 {
		parts = append(parts, vaultBar)
	}
	if projectsBarH > 0 {
		parts = append(parts, projectsBar)
	}
	parts = append(parts, board, hint)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// ── Status bars ───────────────────────────────────────────────────────────────

func (m Model) renderStatusBar() string {
	msg := truncate(m.statusMsg, m.width-4)
	return " " + styleStatusError.Render("✗ "+msg)
}

func (m Model) renderVaultBar() string {
	if m.vaultPath == "" {
		return ""
	}

	vaultName := filepath.Base(m.vaultPath)
	nameStyle := lipgloss.NewStyle().Foreground(colorPurple).Bold(true)
	name := nameStyle.Render("⌁ " + vaultName)

	sep := styleHint.Render("  ·  ")

	_, statErr := os.Stat(m.vaultPath)
	var status string
	if statErr != nil {
		status = styleStatusError.Render("✗ pasta não encontrada")
	} else {
		_, obsErr := os.Stat(filepath.Join(m.vaultPath, ".obsidian"))
		vaultType := "pasta"
		if obsErr == nil {
			vaultType = "obsidian"
		}

		noteStr := ""
		if m.vaultNoteCount >= 0 {
			noteStr = sep + styleHint.Render(fmt.Sprintf("%d notas", m.vaultNoteCount))
		}
		status = styleStatusRunning.Render("✓") +
			styleHint.Render(" "+vaultType) +
			noteStr
	}

	pathHint := styleCardDesc.Render(truncate(m.vaultPath, m.width/2-20))

	return " " + name + sep + pathHint + sep + status
}

func (m Model) renderProjectsBar() string {
	if m.projectsPath == "" {
		return ""
	}

	nameStyle := lipgloss.NewStyle().Foreground(colorCyan).Bold(true)
	name := nameStyle.Render("⌂ " + filepath.Base(m.projectsPath))
	sep := styleHint.Render("  ·  ")

	_, statErr := os.Stat(m.projectsPath)
	var status string
	if statErr != nil {
		status = styleStatusError.Render("✗ pasta não encontrada")
	} else {
		countStr := ""
		if m.projectsCount >= 0 {
			countStr = sep + styleHint.Render(fmt.Sprintf("%d projetos", m.projectsCount))
		}
		status = styleStatusRunning.Render("✓") + styleHint.Render(" projetos") + countStr
	}

	pathHint := styleCardDesc.Render(truncate(m.projectsPath, m.width/2-20))

	return " " + name + sep + pathHint + sep + status
}

// ── Shared header ─────────────────────────────────────────────────────────────

func (m Model) renderHeader() string {
	title := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("quinoa")
	sep := lipgloss.NewStyle().Foreground(colorBorder).Render(" · ")
	nav := lipgloss.NewStyle().Foreground(colorMuted).Render("board")

	var vaultChip string
	if m.vaultPath != "" {
		vaultChip = sep + lipgloss.NewStyle().Foreground(colorBg3).Render("⌁ "+filepath.Base(m.vaultPath))
	} else {
		vaultChip = sep + styleStatusStopped.Render("sem vault  ") +
			styleHintKey.Render("V") + styleHint.Render(" configurar")
	}

	var projectsChip string
	if m.projectsPath != "" {
		projectsChip = sep + lipgloss.NewStyle().Foreground(colorBg3).Render("⌂ "+filepath.Base(m.projectsPath))
	} else {
		projectsChip = sep + styleStatusStopped.Render("sem projetos  ") +
			styleHintKey.Render("P") + styleHint.Render(" configurar")
	}

	right := lipgloss.NewStyle().Foreground(colorMuted).Render(time.Now().Format("15:04:05"))
	middle := title + sep + nav + vaultChip + projectsChip
	gap := m.width - lipgloss.Width(middle) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	return " " + middle + strings.Repeat(" ", gap) + right
}

// ── Board rendering ───────────────────────────────────────────────────────────

func (m Model) renderHint() string {
	colStatus := columns[m.col]
	hasContainer := false
	if s := m.selectedStory(); s != nil {
		if s.TaskID != "" {
			if task, err := m.db.GetTask(s.TaskID); err == nil && task.ContainerID != "" {
				hasContainer = true
			}
		}
		// Also check story-linked tasks (covers error/stopped containers and
		// stories moved back to todo whose task_id was cleared).
		if !hasContainer {
			linked, _ := m.db.ListTasksByStory(s.ID)
			for _, t := range linked {
				if t.ContainerID != "" {
					hasContainer = true
					break
				}
			}
		}
	}
	return " " + helpText(colStatus, hasContainer)
}

func (m Model) renderBoard(width, height int) string {
	gap := 1
	colW := (width - gap*4) / 5

	colNames := []string{"Todo", "Refine", "Doing", "Review", "Done"}
	var rendered []string

	for i, name := range colNames {
		rendered = append(rendered, m.renderColumn(i, name, m.cols[i], i == m.col, colW, height))
	}

	return lipgloss.JoinHorizontal(lipgloss.Top,
		rendered[0],
		strings.Repeat(" ", gap),
		rendered[1],
		strings.Repeat(" ", gap),
		rendered[2],
		strings.Repeat(" ", gap),
		rendered[3],
		strings.Repeat(" ", gap),
		rendered[4],
	)
}

func (m Model) renderColumn(colIdx int, name string, cards []*db.Story, focused bool, width, height int) string {
	count := fmt.Sprintf("(%d)", len(cards))

	var headerStyle lipgloss.Style
	switch colIdx {
	case colRefine:
		headerStyle = styleColHeaderRefine
	case colReview:
		headerStyle = styleColHeaderReview
	default:
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
		cardLines = append(cardLines, m.renderCard(s, focused && i == m.cursor, colIdx, width))
	}

	innerH := height - 3
	if innerH < 1 {
		innerH = 1
	}
	body := strings.Join(cardLines, "\n")
	lines := strings.Split(body, "\n")
	if len(lines) > innerH {
		lines = lines[:innerH]
	}
	body = strings.Join(lines, "\n")

	return lipgloss.JoinVertical(lipgloss.Left, header, divider, body)
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

	// "Ready for human review" badge — shown in review column when all agents approved.
	if s.ReviewResult == "ready" {
		parts = append(parts, styleReadyBadge.Render("✓ pronto para revisão"))
	}

	if s.TaskStatus != "" {
		badge := statusStyle(s.TaskStatus).Render(statusLabel(s.TaskStatus))
		parts = append(parts, badge)
	}

	// Show PRD indicator for refine cards that have a PRD path.
	if colIdx == colRefine && s.PrdPath != "" {
		prdLabel := "⌁ " + filepath.Base(s.PrdPath)
		parts = append(parts, lipgloss.NewStyle().Foreground(colorPurple).Render(prdLabel))
	}

	// Cycle counters — shown when the card has been through at least one doing or review cycle.
	if s.DoingCount > 0 || s.ReviewCount > 0 {
		counter := lipgloss.NewStyle().Foreground(colorMuted).
			Render(fmt.Sprintf("D:%d  R:%d", s.DoingCount, s.ReviewCount))
		parts = append(parts, counter)
	}

	content := strings.Join(parts, "\n")
	if selected {
		return styleCardSelected.Width(width - 2).Render(content)
	}
	return styleCard.Width(width - 2).Render(content)
}

// ── Help overlay ──────────────────────────────────────────────────────────────

func (m Model) helpView() string {
	sections := []struct{ title, body string }{
		{"Navegação (board)", "h/← l/→     mover entre colunas\nk/↑ j/↓     mover entre cards\nenter        expandir descrição"},
		{"Ações Globais", "n            nova história\nr            refresh manual\nV            selecionar vault Obsidian\nP            selecionar pasta de projetos\n?            fechar ajuda\nq            sair"},
		{"Todo", "R            refinar → refine\ns            iniciar direto → doing\nx            deletar história"},
		{"Refine", "t            attach terminal (agente interativo)\ns            iniciar implementação → doing\np            parar agente → todo\nb            voltar → todo"},
		{"Doing", "p            parar agente → todo\nt            attach terminal (docker attach)"},
		{"Review", "a            aprovar → done\nf            corrigir → doing\nb            reabrir → todo\nt            attach terminal (docker attach)"},
		{"Done", "b            reabrir → todo\nx            deletar história\nt            attach terminal (docker attach)"},
		{"Terminal (attach)", "ctrl-q       desconectar e voltar ao board\n(tudo mais é enviado ao container)"},
		{"Formulários", "tab/shift+tab campo anterior/próximo\nctrl+s       confirmar\nesc          cancelar"},
	}

	var rows []string
	for _, s := range sections {
		rows = append(rows, styleFormTitle.Render(s.title)+"\n"+styleHint.Render(s.body))
	}

	content := styleFormTitle.Render("Ajuda — quinoa TUI") + "\n\n" +
		strings.Join(rows, "\n\n") + "\n\n" +
		styleHintKey.Render("?") + styleHint.Render(" para fechar")

	w := 50
	if m.width/2 > w {
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
				last := []rune(out[len(out)-1])
				if len(last) > 3 {
					out[len(out)-1] = string(last[:len(last)-3]) + "..."
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

// buildPRDTemplate returns an empty PRD skeleton with the story title and today's date.
func buildPRDTemplate(title string) string {
	date := time.Now().Format("2006-01-02")
	return "# PRD: " + title + "\n\n" +
		"**Status:** Draft\n" +
		"**Criado em:** " + date + "\n\n" +
		"---\n\n" +
		"## Contexto e Problema\n\n" +
		"[Descreva o contexto e o problema que esta história resolve]\n\n" +
		"## Objetivos\n\n" +
		"[Liste os objetivos mensuráveis desta história]\n\n" +
		"## Histórias de Usuário\n\n" +
		"[Histórias no formato: Como [perfil], quero [ação] para que [benefício]]\n\n" +
		"## Critérios de Aceitação\n\n" +
		"- [ ] [critério 1]\n" +
		"- [ ] [critério 2]\n\n" +
		"## Requisitos Técnicos\n\n" +
		"[Detalhes técnicos, APIs, modelos de dados, decisões de arquitetura, restrições]\n\n" +
		"## Fora do Escopo\n\n" +
		"[O que NÃO será implementado nesta história]\n\n" +
		"## Notas para o Agente de Desenvolvimento\n\n" +
		"[Orientações específicas, preferências de implementação, contexto adicional]\n\n" +
		"## Resumo do Refinamento\n\n" +
		"[Síntese da conversa, principais decisões e o raciocínio por trás delas]"
}

// buildTechLeadDoingPrompt returns the prompt for the Tech Lead agent in the Doing column.
// The Tech Lead reads the PRD, identifies parallel independent tasks, delegates all of them
// via [QUINOA:SPAWN:] signals, and signals [QUINOA:DONE] without implementing anything itself.
// hasReviewFeedback=true means the story is returning from review with feedback in the PRD.
func buildTechLeadDoingPrompt(storyTitle, storyDesc, containerPRDPath string, hasProjects, hasReviewFeedback bool) string {
	intro := "Você é o Tech Lead responsável por orquestrar a implementação desta história.\n\n" +
		"**Papel:** Analisar o PRD, decompor o trabalho em tarefas independentes e delegar cada uma " +
		"a um agente especializado via [QUINOA:SPAWN:]. Você não implementa código — apenas coordena.\n\n" +
		"**História:** " + storyTitle
	if storyDesc != "" {
		intro += "\n\n" + storyDesc
	}

	if containerPRDPath != "" {
		if hasReviewFeedback {
			intro += "\n\n⚠️  **Esta é uma iteração de correção pós-revisão.**\n" +
				"O PRD foi atualizado com seções de feedback (\"## Feedback Code Review\", " +
				"\"## Feedback QA\", \"## Feedback Segurança\"). " +
				"Leia todas essas seções antes de delegar — os agentes devem corrigir especificamente os pontos levantados."
		}
		intro += "\n\n**PRD:** " + containerPRDPath
		intro += "\n\n## Instruções\n\n" +
			"1. Leia o PRD completo em " + containerPRDPath + "\n" +
			"2. Identifique todas as tarefas técnicas necessárias (ex: endpoint de API, componente frontend, migration de banco, testes)\n" +
			"3. Para cada tarefa independente, emita um sinal de spawn **antes de emitir [QUINOA:DONE]**:\n\n" +
			"   ```\n" +
			"   echo \"[QUINOA:SPAWN:<instrução autocontida para o agente>]\"\n" +
			"   ```\n\n" +
			"   A instrução deve conter: o que implementar, em qual projeto dentro de /projects, e que o PRD está em " + containerPRDPath + ".\n" +
			"   Exemplo: `[QUINOA:SPAWN:Implemente o endpoint POST /api/users no projeto em /projects/backend. PRD em " + containerPRDPath + ". Adicione validação e testes unitários.]`\n\n" +
			"4. Após emitir todos os spawns, sinalize sua conclusão:\n" +
			"   ```\n" +
			"   echo \"[QUINOA:DONE]\"\n" +
			"   ```\n\n" +
			"Se o PRD não indicar projetos separados ou todas as tarefas forem interdependentes, delegue tudo a um único agente via spawn."
	} else {
		intro += "\n\nNão há PRD disponível. Delegue a implementação completa da história a um agente via:\n" +
			"   echo \"[QUINOA:SPAWN:<descrição completa da tarefa>]\"\n" +
			"E depois: echo \"[QUINOA:DONE]\""
	}

	if hasProjects {
		intro += "\n\nOs projetos de código estão disponíveis em /projects."
	}
	return intro
}

// buildRefinementPrompt returns the full system prompt for a refinement agent.
// containerPRDPath is the absolute path inside the container where the PRD must be written;
// if empty (no vault mounted), the agent is instructed to print the PRD to stdout instead.
// hasVault indicates whether the Obsidian vault is mounted at /vault.
// hasProjects indicates whether the local projects folder is mounted at /projects.
func buildRefinementPrompt(storyTitle, storyDesc, containerPRDPath string, hasVault, hasProjects bool) string {
	var descBlock string
	if storyDesc != "" {
		descBlock = "\n\nContexto fornecido:\n" + storyDesc
	}

	var saveStep string
	if containerPRDPath != "" {
		saveStep = "Salve o PRD no arquivo abaixo, certificando-se de que todas as seções estão preenchidas:\n" +
			containerPRDPath + "\n\n" +
			"Após salvar o arquivo com sucesso, execute:\n" +
			`echo "[QUINOA:DONE]"`
	} else {
		saveStep = "Imprima o PRD completo no terminal e depois execute:\n" +
			`echo "[QUINOA:DONE]"`
	}

	// Describe the context sources available inside the container.
	var sources []string
	if hasVault {
		sources = append(sources, "• **Vault Obsidian** em /vault — notas, documentação e decisões anteriores do projeto")
	}
	if hasProjects {
		sources = append(sources, "• **Projetos de código** em /projects — implementação atual, APIs, modelos de dados")
	}

	var contextSources string
	switch len(sources) {
	case 0:
		contextSources = "Nenhum contexto adicional está disponível nesta sessão.\n"
	case 1:
		contextSources = "Você tem acesso ao seguinte contexto:\n" + sources[0] + "\n\n" +
			"Antes de fazer qualquer pergunta ao usuário, consulte esse contexto.\n" +
			"Use read_file, search, grep e ferramentas similares para explorar.\n"
	default:
		contextSources = "Você tem acesso aos seguintes contextos:\n" +
			strings.Join(sources, "\n") + "\n\n" +
			"Antes de fazer qualquer pergunta ao usuário, consulte esses contextos.\n" +
			"Use read_file, search, grep e ferramentas similares para encontrar a resposta.\n"
	}

	tpl := buildPRDTemplate(storyTitle)

	return "Você é um product manager especialista em refinamento de histórias de software.\n\n" +
		"**História para refinar:** " + storyTitle + descBlock + "\n\n" +
		"## Contexto disponível\n\n" +
		contextSources + "\n" +
		"## Regra principal: contexto antes de perguntar\n\n" +
		"Só faça uma pergunta ao usuário se:\n" +
		"• O contexto disponível não deixar claro qual é a decisão correta, ou\n" +
		"• For necessária uma escolha de produto/negócio que o contexto não pode responder.\n" +
		"Quando encontrar a resposta no contexto disponível, mencione brevemente o que encontrou antes de prosseguir.\n\n" +
		"## Sessão de refinamento\n\n" +
		"Conduza uma sessão interativa explorando os seguintes pontos.\n" +
		"Para cada ponto, consulte o contexto disponível primeiro e só pergunte o que restar:\n" +
		"• O contexto e o problema que a história resolve\n" +
		"• Os objetivos e critérios de sucesso mensuráveis\n" +
		"• Os requisitos técnicos, restrições e decisões de arquitetura\n" +
		"• O que está dentro e fora do escopo\n" +
		"• Riscos, dependências e alternativas consideradas\n\n" +
		"Quando o usuário indicar que o refinamento está concluído, siga estes dois passos:\n\n" +
		"**Passo 1 — Escreva o PRD** usando EXATAMENTE a estrutura abaixo.\n" +
		"Substitua cada colchete pelo conteúdo real baseado na conversa e no contexto levantado:\n\n" +
		tpl + "\n\n" +
		"**Passo 2 — " + saveStep + "\n\n" +
		"Comece a sessão explorando o contexto disponível, depois apresente-se e inicie o refinamento."
}

// buildTechLeadReviewPrompt returns the prompt for the Tech Lead Review agent.
// The agent does code review then always spawns QA and Security agents in parallel.
// hasReviewFeedback=true means there were previous review issues; the agent should focus
// on verifying the feedback sections of the PRD were addressed before doing a full review.
func buildTechLeadReviewPrompt(storyTitle, containerPRDPath string, hasProjects, hasReviewFeedback bool) string {
	prdRef := ""
	if containerPRDPath != "" {
		prdRef = " O PRD está em " + containerPRDPath + "."
	}
	projectsRef := ""
	if hasProjects {
		projectsRef = " Os projetos de código estão em /projects."
	}

	focusNote := ""
	if hasReviewFeedback {
		focusNote = "\n\n⚠️  **Esta é uma revisão de iteração corretiva.**\n" +
			"Antes de fazer o review completo, verifique se cada ponto das seções " +
			"\"## Feedback Code Review\", \"## Feedback QA\" e \"## Feedback Segurança\" do PRD " +
			"foi devidamente corrigido. Documente explicitamente se cada ponto foi resolvido."
	}

	qaInstruction := "Você é um agente de QA." + prdRef + projectsRef + "\n\n" +
		"Sua tarefa:\n" +
		"1. Leia o PRD para entender os critérios de aceitação\n" +
		"2. Identifique o projeto modificado em /projects e execute os testes existentes\n" +
		"3. Verifique cobertura de testes e se todos os critérios de aceitação foram atendidos\n" +
		"4. Verifique casos de borda não tratados\n\n" +
		"Se encontrar problemas:\n" +
		"- Adicione ao final do PRD uma seção \"## Feedback QA\" descrevendo cada problema\n" +
		"- Execute: echo \"[QUINOA:RETURN_TO_DOING]\"\n\n" +
		"Execute ao final: echo \"[QUINOA:DONE]\""

	secInstruction := "Você é um agente de segurança." + prdRef + projectsRef + "\n\n" +
		"Sua tarefa:\n" +
		"1. Leia o PRD para entender o contexto da implementação\n" +
		"2. Revise o código em /projects buscando as seguintes vulnerabilidades:\n" +
		"   - Injeção SQL / NoSQL\n" +
		"   - XSS e CSRF\n" +
		"   - Secrets ou credenciais expostas no código\n" +
		"   - Autenticação ou autorização inadequada\n" +
		"   - OWASP Top 10 relevantes ao contexto\n\n" +
		"Se encontrar problemas:\n" +
		"- Adicione ao final do PRD uma seção \"## Feedback Segurança\" descrevendo cada vulnerabilidade\n" +
		"- Execute: echo \"[QUINOA:RETURN_TO_DOING]\"\n\n" +
		"Execute ao final: echo \"[QUINOA:DONE]\""

	return "Você é o Tech Lead responsável pela revisão desta história: **" + storyTitle + "**." +
		prdRef + projectsRef + focusNote + "\n\n" +
		"## Passos\n\n" +
		"1. **Leia o PRD** em " + containerPRDPath + " para entender o que deveria ter sido implementado\n" +
		"2. **Localize o código modificado**: navegue pelos projetos em /projects, use `git log --oneline -20` e `git diff` para ver as mudanças recentes\n" +
		"3. **Realize o code review** verificando:\n" +
		"   - Correção funcional em relação ao PRD\n" +
		"   - Qualidade e clareza do código\n" +
		"   - Padrões e arquitetura do projeto\n" +
		"   - Ausência de código morto ou debug\n" +
		"4. **Se encontrar problemas no code review**:\n" +
		"   - Adicione ao final do PRD uma seção \"## Feedback Code Review\" com os problemas encontrados\n" +
		"   - Execute: `echo \"[QUINOA:RETURN_TO_DOING]\"`\n" +
		"5. **Independentemente do resultado do code review**, spawne os agentes de QA e Segurança:\n" +
		"   ```\n" +
		"   echo \"[QUINOA:SPAWN:" + qaInstruction + "]\"\n" +
		"   echo \"[QUINOA:SPAWN:" + secInstruction + "]\"\n" +
		"   ```\n" +
		"6. Execute ao final: `echo \"[QUINOA:DONE]\"`"
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugifyTitle(s string) string {
	s = strings.ToLower(s)
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "prd"
	}
	return s
}
