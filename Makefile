.PHONY: setup init dev run generate check-generated check test test-web build build-linux verify clean check-privacy
setup:
	go mod download
	pnpm --dir web install --frozen-lockfile
	pnpm --dir web exec playwright install chromium
init:
	./scripts/init
dev:
	./scripts/dev
run: build
	./scripts/run
generate:
	./scripts/generate
check-generated:
	./scripts/check-generated
check:
	go vet ./...
	pnpm --dir web check
test:
	go test -race ./...
test-web: build
	./scripts/test-browser
build:
	pnpm --dir web build
	mkdir -p dist
	go build -tags release -trimpath -o dist/reader ./cmd/reader
build-linux:
	pnpm --dir web build
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 go build -tags release -trimpath -ldflags='-s -w' -o dist/reader-linux-amd64 ./cmd/reader
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 go build -trimpath -ldflags='-s -w' -o dist/miniflux-export-linux-amd64 ./cmd/miniflux-export
verify: check-generated check test build-linux
clean:
	rm -rf dist web/build web/.svelte-kit

check-privacy:
	./scripts/check-privacy --history
