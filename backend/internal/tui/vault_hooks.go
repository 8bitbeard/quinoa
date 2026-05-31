package tui

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	vaultDirDaily        = "Daily"
	vaultDirArchitecture = "Architecture"
)

// triggerVaultUpdate runs a non-interactive Claude session to update the vault
// after a kanban event (refinement done or review accepted). Runs asynchronously.
func triggerVaultUpdate(vaultPath, storyTitle, storyDesc, prdPath, event string) {
	if vaultPath == "" {
		return
	}
	go func() {
		if err := doVaultUpdate(vaultPath, storyTitle, storyDesc, prdPath, event); err != nil {
			log.Printf("vault-hook (%s): %v", event, err)
		}
	}()
}

func doVaultUpdate(vaultPath, storyTitle, storyDesc, prdPath, event string) error {
	var ctx strings.Builder
	ctx.WriteString("Evento: " + event + "\n")
	ctx.WriteString("História: " + storyTitle + "\n")
	if storyDesc != "" {
		ctx.WriteString("Descrição: " + storyDesc + "\n")
	}
	if prdPath != "" {
		if data, err := os.ReadFile(prdPath); err == nil {
			ctx.WriteString("\nConteúdo do PRD:\n")
			ctx.Write(data)
		}
	}

	prompt := `Você é um assistente de documentação de software. Com base nas informações abaixo, gere dois blocos.

**Bloco 1 — Daily Note**: Um registro conciso (3 a 5 linhas) do que foi feito ou decidido, para a nota diária do Obsidian. Use linguagem de passado e seja direto. Não use bullet points neste bloco.

**Bloco 2 — Decisões de Arquitetura**: Liste as principais decisões técnicas e de design tomadas, no formato "- **Decisão**: justificativa breve". Se não houver decisões claras, escreva apenas a linha: "Nenhuma decisão de arquitetura registrada."

Responda EXATAMENTE no formato abaixo, sem texto extra fora dos marcadores:

===DAILY===
[conteúdo da daily note aqui]
===ADR===
[lista de decisões de arquitetura aqui]
===END===

Informações:
` + ctx.String()

	cmd := exec.Command("claude", "-p")
	cmd.Stdin = strings.NewReader(prompt)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("claude -p: %w", err)
	}

	daily, adr := parseVaultHookOutput(string(out))
	now := time.Now()

	if dailyErr := appendDailyNote(vaultPath, now, storyTitle, event, daily); dailyErr != nil {
		log.Printf("vault-hook: daily note: %v", dailyErr)
	}
	if adrErr := appendADR(vaultPath, now, storyTitle, adr); adrErr != nil {
		log.Printf("vault-hook: ADR: %v", adrErr)
	}
	return nil
}

// parseVaultHookOutput extracts the daily and ADR sections from Claude's output.
func parseVaultHookOutput(raw string) (daily, adr string) {
	after := func(s, marker string) (string, bool) {
		i := strings.Index(s, marker)
		if i < 0 {
			return s, false
		}
		return s[i+len(marker):], true
	}
	before := func(s, marker string) string {
		i := strings.Index(s, marker)
		if i < 0 {
			return s
		}
		return s[:i]
	}

	rest, ok := after(raw, "===DAILY===")
	if !ok {
		return strings.TrimSpace(raw), ""
	}
	daily = strings.TrimSpace(before(rest, "===ADR==="))

	rest, ok = after(rest, "===ADR===")
	if !ok {
		return daily, ""
	}
	adr = strings.TrimSpace(before(rest, "===END==="))
	return
}

// appendDailyNote creates or appends to today's daily note in the vault.
func appendDailyNote(vaultPath string, t time.Time, storyTitle, event, content string) error {
	dir := filepath.Join(vaultPath, vaultDirDaily)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	dayFile := filepath.Join(dir, t.Format("2006-01-02")+".md")

	if _, err := os.Stat(dayFile); os.IsNotExist(err) {
		header := "# " + t.Format("2006-01-02") + "\n"
		if writeErr := os.WriteFile(dayFile, []byte(header), 0644); writeErr != nil {
			return writeErr
		}
	}

	entry := fmt.Sprintf("\n## %s — %s\n*%s*\n\n%s\n",
		event, storyTitle, t.Format("15:04"), content)

	f, err := os.OpenFile(dayFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(entry)
	return err
}

// appendADR appends architecture decisions to the vault's decisions log.
func appendADR(vaultPath string, t time.Time, storyTitle, content string) error {
	if content == "" || content == "Nenhuma decisão de arquitetura registrada." {
		return nil
	}

	dir := filepath.Join(vaultPath, vaultDirArchitecture)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	adrFile := filepath.Join(dir, "decisions.md")

	if _, err := os.Stat(adrFile); os.IsNotExist(err) {
		header := "# Decisões de Arquitetura\n"
		if writeErr := os.WriteFile(adrFile, []byte(header), 0644); writeErr != nil {
			return writeErr
		}
	}

	entry := fmt.Sprintf("\n## %s — %s\n*%s*\n\n%s\n",
		t.Format("2006-01-02"), storyTitle, t.Format("15:04"), content)

	f, err := os.OpenFile(adrFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(entry)
	return err
}
