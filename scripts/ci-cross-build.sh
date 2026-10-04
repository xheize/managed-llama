#!/bin/sh
# Run on Linux; never treat compilation as a Windows test run.
set -eu

export GOOS=windows GOARCH=amd64 CGO_ENABLED=0

unformatted=$(git ls-files '*.go' | xargs gofmt -l)
if [ -n "$unformatted" ]; then
    printf 'Run gofmt on:\n%s\n' "$unformatted"
    exit 1
fi
git diff --check
go mod download
go mod verify
go vet ./...

mkdir -p dist/ci/tests
go test -c -o dist/ci/tests/ ./...
go build -buildvcs=false -trimpath \
    -ldflags '-H=windowsgui -X main.version=v0.0.0' \
    -o dist/ci/managed-llama-windows-amd64.exe .
