#!/usr/bin/env bash
set -euo pipefail

export HOME="${HOME:-/home/node}"
WORK_DIR="${WORK_DIR:-/workspace/repo}"

# Copy Claude credentials from read-only host mount to writable home directory.
# Each container gets its own copy so concurrent agents don't conflict.
if [ -d /run/claude-host-creds/claude ]; then
    mkdir -p "$HOME/.claude"
    cp -rn /run/claude-host-creds/claude/. "$HOME/.claude/" 2>/dev/null || true
fi
if [ -f /run/claude-host-creds/claude.json ]; then
    cp /run/claude-host-creds/claude.json "$HOME/.claude.json" 2>/dev/null || true
fi

echo "[quinoa] configurando credenciais do agente..."

# Mark the workspace as trusted in ~/.claude.json so Claude Code does not
# show the "Do you trust the files in this folder?" dialog.
if [ -f "$HOME/.claude.json" ]; then
    jq --arg p "$WORK_DIR" \
        '(.projects[$p] // {}) + {"hasTrustDialogAccepted":true} as $proj
         | .projects = ((.projects // {}) + {($p): $proj})' \
        "$HOME/.claude.json" > /tmp/.cj.tmp && mv /tmp/.cj.tmp "$HOME/.claude.json" || true
else
    printf '{"projects":{"%s":{"hasTrustDialogAccepted":true}}}\n' "$WORK_DIR" > "$HOME/.claude.json"
fi

echo "[quinoa] iniciando setup..."

if [[ -n "${REPO_URL:-}" ]]; then
    echo "[quinoa] clonando $REPO_URL"
    if [[ -n "${REPO_BRANCH:-}" ]]; then
        git clone --depth=1 --branch "$REPO_BRANCH" "$REPO_URL" "$WORK_DIR"
    else
        git clone --depth=1 "$REPO_URL" "$WORK_DIR"
    fi
elif [[ -d "/workspace/repo" ]]; then
    echo "[quinoa] usando repo montado em /workspace/repo"
    WORK_DIR="/workspace/repo"
else
    echo "[quinoa] ERRO: nenhum REPO_URL definido e nenhum repo montado em /workspace/repo"
    exit 1
fi

cd "$WORK_DIR"
echo "[quinoa] setup concluído — iniciando agente..."
echo ""

# Inject task-completion signal instructions into CLAUDE.md so the agent
# can notify the quinoa board when it finishes.
cat >> CLAUDE.md 2>/dev/null << 'QUINOA_INSTRUCTIONS'

## Quinoa Board Integration (required)
When you have **fully completed** the requested task, you MUST execute the following
command as your very last action before stopping:
```bash
echo "[QUINOA:DONE]"
```
This signals the quinoa board to move the task card to the Review column.
If the user asks you to make corrections, work on them and run the command again when done.
QUINOA_INSTRUCTIONS

exec bash -c "$AGENT_COMMAND"
