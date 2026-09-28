VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/built-for-devs/quiver/cmd.Version=$(VERSION)

.PHONY: build install test lint lint-actions snapshot clean

build:
	go build -ldflags "$(LDFLAGS)" -o quiver .

install:
	go install -ldflags "$(LDFLAGS)" .

test:
	go test ./...

lint:
	@test -z "$$(gofmt -l .)" || (gofmt -l . && echo "run gofmt -w ." && exit 1)
	go vet ./...

lint-actions:
	go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/*.yml examples/*.yml

# Build release archives locally into dist/ without publishing.
snapshot:
	goreleaser release --snapshot --clean --skip=before

clean:
	rm -rf quiver dist
