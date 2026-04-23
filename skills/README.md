# AI Agent Skills

Skills for AI coding agents that interact with agent2shell.

## Available Skills

| Skill | Description |
|-------|-------------|
| [ai-agent-usage](ai-agent-usage/SKILL.md) | How to use agent2shell to execute commands, transfer files, and query sessions on a compromised target |
| [e2e-testing](e2e-testing/SKILL.md) | E2E testing protocol using tmux sessions to simulate a real reverse shell engagement |

## Setup

Copy the skill directory to your AI agent's skills directory:

| Agent | Project path | Global path |
|-------|-------------|-------------|
| Claude Code | `.claude/skills/` | `~/.claude/skills/` |
| Cursor | `.cursor/skills/` | `~/.cursor/skills/` |
| OpenCode | `.opencode/skills/` | `~/.config/opencode/skills/` |
| GitHub Copilot | `.github/skills/` | `~/.copilot/skills/` |
| Windsurf | `.windsurf/skills/` | `~/.codeium/windsurf/skills/` |
| Cline | `.cline/skills/` | `~/.cline/skills/` |

All tools also recognize `.claude/skills/` as a compatibility path.

```bash
cp -r skills/ai-agent-usage ~/.claude/skills/
```

Your AI agent will automatically discover and use it when relevant.
