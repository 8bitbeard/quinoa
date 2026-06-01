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

# Mark a path as trusted in ~/.claude.json so Claude Code skips the
# "Do you trust the files in this folder?" dialog for that directory.
trust_path() {
    local p="$1"
    if [ -f "$HOME/.claude.json" ]; then
        jq --arg p "$p" \
            '(.projects[$p] // {}) + {"hasTrustDialogAccepted":true} as $proj
             | .projects = ((.projects // {}) + {($p): $proj})' \
            "$HOME/.claude.json" > /tmp/.cj.tmp && mv /tmp/.cj.tmp "$HOME/.claude.json" || true
    else
        printf '{"projects":{"%s":{"hasTrustDialogAccepted":true}}}\n' "$p" > "$HOME/.claude.json"
    fi
}

echo "[quinoa] iniciando setup..."

if [[ "${QUINOA_MODE:-}" == "refine" ]]; then
    # Refinement mode: work from the vault so the agent can read notes and write PRDs.
    if [[ -d "/vault" ]]; then
        WORK_DIR="/vault"
        echo "[quinoa] modo refinamento — diretório de trabalho: /vault"
    else
        WORK_DIR="/tmp/workspace"
        mkdir -p "$WORK_DIR"
        echo "[quinoa] modo refinamento — sem vault, usando diretório temporário"
    fi

    # Optionally clone a code repo so the agent can read the codebase for context.
    if [[ -n "${REPO_URL:-}" ]]; then
        echo "[quinoa] clonando repositório de código para contexto do refinamento..."
        if [[ -n "${REPO_BRANCH:-}" ]]; then
            git clone --depth=1 --branch "$REPO_BRANCH" "$REPO_URL" /workspace/repo 2>&1 \
                || echo "[quinoa] aviso: não foi possível clonar $REPO_URL"
        else
            git clone --depth=1 "$REPO_URL" /workspace/repo 2>&1 \
                || echo "[quinoa] aviso: não foi possível clonar $REPO_URL"
        fi
    fi
    if [[ -d "/workspace/repo" ]]; then
        echo "[quinoa] código disponível em /workspace/repo"
    fi

elif [[ -n "${REPO_URL:-}" ]]; then
    echo "[quinoa] clonando $REPO_URL"
    if [[ -n "${REPO_BRANCH:-}" ]]; then
        git clone --depth=1 --branch "$REPO_BRANCH" "$REPO_URL" "$WORK_DIR"
    else
        git clone --depth=1 "$REPO_URL" "$WORK_DIR"
    fi
elif [[ -d "/workspace/repo" ]]; then
    echo "[quinoa] usando repo montado em /workspace/repo"
    WORK_DIR="/workspace/repo"
elif [[ -d "/projects" ]]; then
    # Projects-folder mode: agent reads PRD and navigates to the right project.
    WORK_DIR="/tmp/quinoa-workspace"
    mkdir -p "$WORK_DIR"
    echo "[quinoa] modo projetos — diretório de trabalho: $WORK_DIR (projetos em /projects)"
else
    echo "[quinoa] ERRO: nenhum REPO_URL definido e nenhuma pasta montada em /workspace/repo ou /projects"
    exit 1
fi

cd "$WORK_DIR"

# Trust the actual working directory (resolved after all WORK_DIR reassignments above),
# plus any mounted volumes the agent may navigate into.
trust_path "$WORK_DIR"
[[ -d "/projects" ]] && trust_path "/projects"
[[ -d "/vault" ]]    && trust_path "/vault"

# Place a .claude/settings.json in the working directory so Claude Code enters
# bypassPermissions mode without showing the "--dangerously-skip-permissions" confirmation.
mkdir -p ".claude"
printf '{"defaultMode":"bypassPermissions"}\n' > ".claude/settings.json"

# Remove --dangerously-skip-permissions from the command: the flag triggers an
# interactive confirmation dialog that cannot be auto-accepted. The settings.json
# above already enables bypassPermissions mode, making the flag redundant.
AGENT_COMMAND="${AGENT_COMMAND//--dangerously-skip-permissions/}"

echo "[quinoa] setup concluído — iniciando agente..."
echo ""

# Inject task-completion signal instructions into CLAUDE.md so the agent
# can notify the quinoa board when it finishes.
# For refine mode, write to a temporary file (no project CLAUDE.md to append to).
CLAUDE_INSTRUCTIONS_FILE="CLAUDE.md"
if [[ "${QUINOA_MODE:-}" == "refine" ]]; then
    CLAUDE_INSTRUCTIONS_FILE="/tmp/CLAUDE-quinoa.md"
fi

cat >> "$CLAUDE_INSTRUCTIONS_FILE" 2>/dev/null << 'QUINOA_INSTRUCTIONS'

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
