#!/bin/bash

ENV=${1:-development}
ENV_FILE=".env.${ENV}"

if [ -f "$ENV_FILE" ]; then
    set -a
    source "$ENV_FILE"
    set +a
    echo "Loaded environment: $ENV"
else
    echo "Environment file $ENV_FILE not found"
    exit 1
fi