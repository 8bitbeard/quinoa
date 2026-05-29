package templates

import (
	"fmt"
	"strings"

	"github.com/wiltsou/quinoa/internal/db"
)

// repoLabel extracts a short human-readable name from the task's repo source.
func repoLabel(t *db.Task) string {
	if t.RepoURL != "" {
		parts := strings.Split(strings.TrimSuffix(t.RepoURL, ".git"), "/")
		if len(parts) >= 2 {
			return fmt.Sprintf("%s/%s", parts[len(parts)-2], parts[len(parts)-1])
		}
		return t.RepoURL
	}
	if t.RepoPath != "" {
		parts := strings.Split(strings.TrimRight(t.RepoPath, "/"), "/")
		return parts[len(parts)-1]
	}
	return t.ID[:8]
}
