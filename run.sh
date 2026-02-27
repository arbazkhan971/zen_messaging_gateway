#!/bin/bash
set -e

echo "🚀 Starting ZEN Messaging Gateway..."
echo "Working directory: $(pwd)"
echo "Go version: $(go version)"

# Check if Keys.json exists
if [ ! -f "Keys.json" ]; then
    echo "❌ Keys.json not found! Please create it or copy from example."
    exit 1
fi

echo "✅ Keys.json found"

# Run with go run (avoids the dyld issue)
exec go run main.go
