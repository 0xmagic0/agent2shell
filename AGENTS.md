# agent2shell — Repository Guidelines

Programmable reverse shell interface for AI agents. Go 1.22+, single static binary, Unix domain sockets for IPC.

## Skills

| Skill | Description |
|-------|-------------|
| [reverse-shell-agent](skills/reverse-shell-agent/SKILL.md) | Teaches AI agents how to use agent2shell with reverse shells |
| [dev-e2e-testing](skills/dev-e2e-testing/SKILL.md) | Development E2E testing protocol with tmux sessions |

## Code Review Rules

## Go Conventions

- Return errors, never panic. Wrap with context: `fmt.Errorf("action on %s: %w", thing, err)`
- Use sentinel errors for known conditions
- No naked returns
- Context propagation for cancellation — pass `ctx context.Context` as first parameter
- Prefer stdlib over external dependencies

## Naming

- Packages: short, lowercase, no underscores
- Interfaces: verb-er suffix (`Reader`, `Listener`, `Recorder`)
- Unexported unless the package API requires it

## Concurrency

- Protect shared state with mutex, not channels (unless fan-out)
- Always defer `mu.Unlock()` immediately after `mu.Lock()`
- No goroutine leaks — every goroutine must have a shutdown path via context or done channel

## Error Handling

- No swallowed errors — if you ignore an error, comment why
- Exit codes: 0-125 forwarded from remote, 126 for agent2shell errors, 127 for CLI errors
- Timeouts must be configurable, never hardcoded

## Security

- Base64 transfer checksums are mandatory, not optional

## CI/CD

- Pin GitHub Actions to full SHA, never mutable tags — add version comment: `actions/checkout@<sha> # v4.2.2`
- Workflow permissions: `contents: read` at workflow level, `contents: write` scoped only to jobs that publish

## Testing

- Table-driven tests with testify
- Unit tests: no networking, no filesystem side effects
- Test file naming: `foo_test.go` next to `foo.go`
- Integration tests under `tests/integration/`, e2e under `tests/e2e/`

## Structure

- One Cobra command per file in `cmd/agent2shell/`
- Public API in `pkg/`, private helpers in `internal/`
- Types package for shared request/response structs — no circular imports

## EC2 Testing Lab

For E2E testing over a real network with SSH tunnel:

```bash
scripts/ec2-create.sh   # spin up attacker + victim EC2 instances
scripts/ec2-destroy.sh  # tear down everything
```

Requires AWS CLI configured. See [ec2-create.sh](scripts/ec2-create.sh) for details.
