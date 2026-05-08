#!/bin/bash
set -euo pipefail

# Deployment script for sbs-engine on Lightsail (or any EC2-style box).
#
# Usage:
#   ./deploy.sh                    # build only, package into ./deploy/
#   ./deploy.sh production push    # build, package, rsync to host, restart service
#
# Push mode requires:
#   SSH_KEY    path to .pem (default: ../sbs-engine.pem)
#   SSH_USER   remote user  (default: ec2-user)
#   SSH_HOST   remote host  (no default — must be set when push=push)

ENV=${1:-production}
ACTION=${2:-package}

echo "Building for Linux..."
make build-linux

echo "Creating deployment package..."
mkdir -p deploy
cp main-linux deploy/
cp load-env.sh deploy/
cp .env."$ENV" deploy/.env
[ -f Caddyfile ] && cp Caddyfile deploy/
[ -f sbs-engine.service ] && cp sbs-engine.service deploy/

echo "Deployment package ready in ./deploy/"

if [ "$ACTION" != "push" ]; then
    echo "Upload to host and run: chmod +x main-linux && sudo systemctl restart sbs-engine"
    exit 0
fi

: "${SSH_HOST:?SSH_HOST must be set when pushing (e.g. SSH_HOST=1.2.3.4 ./deploy.sh production push)}"
SSH_KEY=${SSH_KEY:-../sbs-engine.pem}
SSH_USER=${SSH_USER:-ec2-user}
REMOTE_DIR=/home/${SSH_USER}/sbs-engine

echo "Syncing ./deploy/ to ${SSH_USER}@${SSH_HOST}:${REMOTE_DIR}..."
rsync -avz --delete \
    -e "ssh -i ${SSH_KEY} -o StrictHostKeyChecking=accept-new" \
    --exclude='.env' \
    ./deploy/ "${SSH_USER}@${SSH_HOST}:${REMOTE_DIR}/"

# .env synced separately with 0600 perms so secrets are not world-readable.
echo "Syncing .env with restricted permissions..."
scp -i "${SSH_KEY}" -o StrictHostKeyChecking=accept-new \
    deploy/.env "${SSH_USER}@${SSH_HOST}:${REMOTE_DIR}/.env"
ssh -i "${SSH_KEY}" "${SSH_USER}@${SSH_HOST}" "chmod 600 ${REMOTE_DIR}/.env && chmod +x ${REMOTE_DIR}/main-linux"

echo "Restarting sbs-engine service..."
ssh -i "${SSH_KEY}" "${SSH_USER}@${SSH_HOST}" "sudo systemctl restart sbs-engine && sudo systemctl status sbs-engine --no-pager"

echo "Deploy complete."