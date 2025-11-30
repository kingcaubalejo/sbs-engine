#!/bin/bash

echo "Checking Environment Variables"
echo "=============================="

# Check if .env file exists
if [ -f "/home/ec2-user/sbs-engine/.env" ]; then
    echo "✓ .env file exists"
    echo "Contents:"
    cat /home/ec2-user/sbs-engine/.env
else
    echo "✗ .env file missing"
fi

echo ""
echo "Current Environment Variables:"
echo "PORT: $PORT"
echo "APP_ENV: $APP_ENV"
echo "BLUEPRINT_DB_HOST: $BLUEPRINT_DB_HOST"
echo "BLUEPRINT_DB_USERNAME: $BLUEPRINT_DB_USERNAME"
echo "BLUEPRINT_DB_NAME: $BLUEPRINT_DB_NAME"

echo ""
echo "Testing manual load:"
source /home/ec2-user/sbs-engine/.env 2>/dev/null && echo "✓ Can load .env" || echo "✗ Cannot load .env"

echo ""
echo "Service environment test:"
sudo systemctl show sbs-engine --property=Environment