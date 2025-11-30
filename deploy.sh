#!/bin/bash

# EC2 Deployment Script
ENV=${1:-production}

echo "Building for Linux..."
make build-linux

echo "Creating deployment package..."
mkdir -p deploy
cp main-linux deploy/
cp load-env.sh deploy/
cp .env.$ENV deploy/.env
cp -r internal deploy/ 2>/dev/null || true

echo "Deployment package ready in ./deploy/"
echo "Upload to EC2 and run: chmod +x main-linux && ./main-linux"