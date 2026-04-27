# AI Agent Skills

Skills for AI coding agents that interact with agent2shell.

## Available Skills

| Skill | Description |
|-------|-------------|
| [reverse-shell-agent](reverse-shell-agent/SKILL.md) | How to use agent2shell to execute commands, transfer files, and query sessions on a compromised target |
| [dev-e2e-testing](dev-e2e-testing/SKILL.md) | E2E testing protocol using tmux sessions to simulate a real reverse shell engagement |

## Setup

Copy the skill directory to your AI agent's skills directory:

| Agent | Project path | Global path |
|-------|-------------|-------------|
| Claude Code | `.claude/skills/` | `~/.claude/skills/` |
| OpenCode | `.opencode/skills/` | `~/.config/opencode/skills/` |
| Gemini CLI | `.gemini/skills/` | `~/.gemini/skills/` |

```bash
cp -r skills/reverse-shell-agent ~/.claude/skills/
```

Your AI agent will automatically discover and use it when relevant.
