# Quinoa — Instruções para Claude Code

## Commits

Após cada alteração funcional, crie um commit seguindo o padrão de **Conventional Commits**:

```
<tipo>(<escopo>): <descrição curta em português>

[corpo opcional explicando o porquê da mudança]
```

### Tipos permitidos

| Tipo | Quando usar |
|------|-------------|
| `feat` | Nova funcionalidade |
| `fix` | Correção de bug |
| `refactor` | Refatoração sem mudança de comportamento |
| `chore` | Tarefas de build, dependências, configs |
| `docs` | Documentação |
| `test` | Adição ou correção de testes |
| `style` | Formatação, espaços, ponto e vírgula |

### Escopos comuns

- `tui` — interface de terminal (Bubbletea)
- `docker` — cliente e integração Docker
- `db` — banco de dados SQLite
- `runner` — execução de tarefas/containers
- `sandbox` — scripts dentro do container
- `form` — formulários da TUI

### Regras

- Um commit por alteração lógica — não agrupe mudanças não relacionadas
- A descrição deve ser em português, imperativo, sem ponto final
- O corpo deve explicar o **porquê**, não o **o quê** (o código já explica o quê)
- Nunca use `--no-verify` para pular hooks

### Exemplo

```
fix(runner): passar RunConfig completo para evitar perda de VaultPath e ProjectsPath

O método StartTask recebia o RunConfig mas ao chamar runTask passava apenas
o db.Task, que não armazena vault/projects. Os bind mounts nunca eram criados.
```
