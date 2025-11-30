#!/bin/bash

# EC2 Setup Script
echo "Setting up SBS Engine on EC2..."

# Create app directory
sudo mkdir -p /home/ec2-user/sbs-engine
sudo chown ec2-user:ec2-user /home/ec2-user/sbs-engine

# Copy files (run this after uploading)
# cp main-linux /home/ec2-user/sbs-engine/
# cp .env.production /home/ec2-user/sbs-engine/.env

# Make binary executable
chmod +x /home/ec2-user/sbs-engine/main-linux

# Install systemd service
sudo cp sbs-engine.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable sbs-engine
sudo systemctl start sbs-engine

echo "Service status:"
sudo systemctl status sbs-engine