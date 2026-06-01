package db

import (
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct{ *sql.DB }

type Task struct {
	ID           string    `json:"id"`
	StoryID      string    `json:"story_id"`
	RepoURL      string    `json:"repo_url"`
	RepoPath     string    `json:"repo_path"`
	RepoBranch   string    `json:"repo_branch"`
	AgentCommand string    `json:"agent_command"`
	BaseCommand  string    `json:"base_command"` // agent binary + flags, without the prompt
	ContainerID  string    `json:"container_id"`
	Status       string    `json:"status"` // pending, running, done, error, stopped
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Story struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	KanbanStatus string    `json:"kanban_status"` // todo, refine, doing, review, done
	TaskID       string    `json:"task_id"`
	TaskStatus   string    `json:"task_status"` // populated via LEFT JOIN tasks
	PrdPath      string    `json:"prd_path"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func New(path string) (*DB, error) {
	sqldb, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, err
	}
	d := &DB{sqldb}
	return d, d.migrate()
}

func (d *DB) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS tasks (
			id            TEXT PRIMARY KEY,
			repo_url      TEXT NOT NULL DEFAULT '',
			repo_path     TEXT NOT NULL DEFAULT '',
			repo_branch   TEXT NOT NULL DEFAULT '',
			agent_command TEXT NOT NULL,
			container_id  TEXT NOT NULL DEFAULT '',
			status        TEXT NOT NULL DEFAULT 'pending',
			created_at    DATETIME NOT NULL,
			updated_at    DATETIME NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS stories (
			id            TEXT PRIMARY KEY,
			title         TEXT NOT NULL,
			description   TEXT NOT NULL DEFAULT '',
			kanban_status TEXT NOT NULL DEFAULT 'todo',
			task_id       TEXT NOT NULL DEFAULT '',
			created_at    DATETIME NOT NULL,
			updated_at    DATETIME NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS config (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL DEFAULT ''
		)`,
	}
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			return err
		}
	}
	// Additive column migrations (idempotent – ignore error if column exists).
	d.Exec(`ALTER TABLE stories ADD COLUMN prd_path TEXT NOT NULL DEFAULT ''`)
	d.Exec(`ALTER TABLE tasks ADD COLUMN story_id TEXT NOT NULL DEFAULT ''`)
	d.Exec(`ALTER TABLE tasks ADD COLUMN base_command TEXT NOT NULL DEFAULT ''`)
	return nil
}

const (
	ConfigVaultPath    = "vault_path"
	ConfigProjectsPath = "projects_path"
)

func (d *DB) GetConfig(key string) (string, error) {
	var value string
	err := d.QueryRow(`SELECT value FROM config WHERE key=?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (d *DB) SetConfig(key, value string) error {
	_, err := d.Exec(
		`INSERT INTO config (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, value,
	)
	return err
}

func (d *DB) InsertTask(t *Task) error {
	now := time.Now().UTC()
	t.CreatedAt = now
	t.UpdatedAt = now
	_, err := d.Exec(`
		INSERT INTO tasks (id, story_id, repo_url, repo_path, repo_branch, agent_command, base_command, container_id, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.StoryID, t.RepoURL, t.RepoPath, t.RepoBranch, t.AgentCommand, t.BaseCommand, t.ContainerID, t.Status, t.CreatedAt, t.UpdatedAt,
	)
	return err
}

func (d *DB) UpdateTaskStatus(id, status, containerID string) error {
	_, err := d.Exec(`UPDATE tasks SET status=?, container_id=?, updated_at=? WHERE id=?`,
		status, containerID, time.Now().UTC(), id)
	return err
}

func (d *DB) GetTask(id string) (*Task, error) {
	row := d.QueryRow(`
		SELECT id, story_id, repo_url, repo_path, repo_branch, agent_command, base_command, container_id, status, created_at, updated_at
		FROM tasks WHERE id=?`, id)
	return scanTask(row)
}

func (d *DB) ListTasks() ([]*Task, error) {
	rows, err := d.Query(`
		SELECT id, story_id, repo_url, repo_path, repo_branch, agent_command, base_command, container_id, status, created_at, updated_at
		FROM tasks ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (d *DB) ListTasksByStory(storyID string) ([]*Task, error) {
	rows, err := d.Query(`
		SELECT id, story_id, repo_url, repo_path, repo_branch, agent_command, base_command, container_id, status, created_at, updated_at
		FROM tasks WHERE story_id=? ORDER BY created_at ASC`, storyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// AllStoryTasksDone returns true when no task linked to storyID is still
// running or pending. Used to avoid moving a story to "review" while parallel
// agents are still executing.
func (d *DB) AllStoryTasksDone(storyID string) bool {
	var count int
	err := d.QueryRow(
		`SELECT COUNT(*) FROM tasks WHERE story_id=? AND status IN ('running','pending')`,
		storyID,
	).Scan(&count)
	return err == nil && count == 0
}

type scanner interface {
	Scan(dest ...any) error
}

func scanTask(s scanner) (*Task, error) {
	t := &Task{}
	err := s.Scan(&t.ID, &t.StoryID, &t.RepoURL, &t.RepoPath, &t.RepoBranch, &t.AgentCommand, &t.BaseCommand, &t.ContainerID, &t.Status, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

func (d *DB) InsertStory(s *Story) error {
	now := time.Now().UTC()
	s.CreatedAt = now
	s.UpdatedAt = now
	_, err := d.Exec(`
		INSERT INTO stories (id, title, description, kanban_status, task_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.Title, s.Description, s.KanbanStatus, s.TaskID, s.CreatedAt, s.UpdatedAt,
	)
	return err
}

func (d *DB) ListStories() ([]*Story, error) {
	rows, err := d.Query(`
		SELECT s.id, s.title, s.description, s.kanban_status, s.task_id,
		       COALESCE(t.status, '') AS task_status,
		       s.prd_path,
		       s.created_at, s.updated_at
		FROM stories s
		LEFT JOIN tasks t ON s.task_id = t.id AND s.task_id != ''
		ORDER BY s.created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var stories []*Story
	for rows.Next() {
		s := &Story{}
		if err := rows.Scan(&s.ID, &s.Title, &s.Description, &s.KanbanStatus, &s.TaskID,
			&s.TaskStatus, &s.PrdPath, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		stories = append(stories, s)
	}
	return stories, rows.Err()
}

func (d *DB) GetStory(id string) (*Story, error) {
	row := d.QueryRow(`
		SELECT s.id, s.title, s.description, s.kanban_status, s.task_id,
		       COALESCE(t.status, '') AS task_status,
		       s.prd_path,
		       s.created_at, s.updated_at
		FROM stories s
		LEFT JOIN tasks t ON s.task_id = t.id AND s.task_id != ''
		WHERE s.id = ?`, id)
	s := &Story{}
	err := row.Scan(&s.ID, &s.Title, &s.Description, &s.KanbanStatus, &s.TaskID,
		&s.TaskStatus, &s.PrdPath, &s.CreatedAt, &s.UpdatedAt)
	return s, err
}

func (d *DB) UpdateStoryKanban(id, kanbanStatus, taskID string) error {
	_, err := d.Exec(`UPDATE stories SET kanban_status=?, task_id=?, updated_at=? WHERE id=?`,
		kanbanStatus, taskID, time.Now().UTC(), id)
	return err
}

func (d *DB) DeleteStory(id string) error {
	_, err := d.Exec(`DELETE FROM stories WHERE id=?`, id)
	return err
}

func (d *DB) GetStoryByTaskID(taskID string) (*Story, error) {
	row := d.QueryRow(`
		SELECT s.id, s.title, s.description, s.kanban_status, s.task_id,
		       COALESCE(t.status, '') AS task_status,
		       s.prd_path,
		       s.created_at, s.updated_at
		FROM stories s
		LEFT JOIN tasks t ON s.task_id = t.id AND s.task_id != ''
		WHERE s.task_id = ?`, taskID)
	s := &Story{}
	err := row.Scan(&s.ID, &s.Title, &s.Description, &s.KanbanStatus, &s.TaskID,
		&s.TaskStatus, &s.PrdPath, &s.CreatedAt, &s.UpdatedAt)
	return s, err
}

func (d *DB) UpdateStoryPRD(id, prdPath string) error {
	_, err := d.Exec(`UPDATE stories SET prd_path=?, updated_at=? WHERE id=?`,
		prdPath, time.Now().UTC(), id)
	return err
}
