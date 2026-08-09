VERSION ?= dev

.PHONY: build test release clean

build:
	mkdir -p bin
	go build -ldflags="-X github.com/R44VC0RP/cronctl/internal/cronctl.version=$(VERSION)" -o bin/cronctl ./cmd/cronctl

test:
	go test ./...
	mkdir -p bin/check
	GOOS=darwin GOARCH=arm64 go build -o bin/check/cronctl-darwin ./cmd/cronctl
	GOOS=linux GOARCH=amd64 go build -o bin/check/cronctl-linux ./cmd/cronctl
	GOOS=windows GOARCH=amd64 go build -o bin/check/cronctl.exe ./cmd/cronctl
	GOOS=windows GOARCH=amd64 go build -ldflags="-H=windowsgui" -o bin/check/cronctl-daemon.exe ./cmd/cronctl-daemon

release: clean
	./scripts/package-release.sh "$(VERSION)" dist

clean:
	rm -rf bin dist
