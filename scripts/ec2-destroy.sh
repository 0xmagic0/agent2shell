#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
STATE_FILE="$SCRIPT_DIR/.ec2-lab-state"
KEY_DIR="$HOME/.ssh/agent2shell"

if [ ! -f "$STATE_FILE" ]; then
    echo "[!] No lab state found at $STATE_FILE"
    echo "[!] Nothing to destroy."
    exit 1
fi

source "$STATE_FILE"

echo "[*] agent2shell E2E lab — destroying infrastructure"
echo "[*] Region: $REGION"

# --- Terminate instances ---
echo "[*] Terminating attacker: $ATTACKER_ID"
aws ec2 terminate-instances --instance-ids "$ATTACKER_ID" --region "$REGION" > /dev/null 2>&1 || true

echo "[*] Terminating victim: $VICTIM_ID"
aws ec2 terminate-instances --instance-ids "$VICTIM_ID" --region "$REGION" > /dev/null 2>&1 || true

echo "[*] Waiting for termination..."
aws ec2 wait instance-terminated --instance-ids "$ATTACKER_ID" "$VICTIM_ID" --region "$REGION" 2>/dev/null || true

# --- Delete security group (may fail if instances still shutting down) ---
echo "[*] Deleting security group: $SG_ID"
for i in $(seq 1 6); do
    if aws ec2 delete-security-group --group-id "$SG_ID" --region "$REGION" 2>/dev/null; then
        echo "[*] Security group deleted"
        break
    fi
    echo "[*] Waiting for dependencies to clear..."
    sleep 10
done

# --- Delete key pairs ---
echo "[*] Deleting key pairs..."
aws ec2 delete-key-pair --key-name "a2s-attacker" --region "$REGION" 2>/dev/null || true
aws ec2 delete-key-pair --key-name "a2s-victim" --region "$REGION" 2>/dev/null || true
rm -f "$KEY_DIR/a2s-attacker.pem" "$KEY_DIR/a2s-victim.pem"
rmdir "$KEY_DIR" 2>/dev/null || true

# --- Clean up local artifacts ---
rm -f "$SCRIPT_DIR/agent2shell-linux-amd64"
rm -f "$SCRIPT_DIR/.ec2-lab-state"
rm -f /tmp/a2s-ec2.sock 2>/dev/null || true

echo ""
echo "[*] Lab destroyed. All resources cleaned up."
