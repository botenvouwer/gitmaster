VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  = -s -w -X main.version=$(VERSION)
PLATFORMS = darwin/arm64 darwin/amd64 linux/amd64 linux/arm64

.PHONY: build install run test vet check build-all

build:
	go build -ldflags "$(LDFLAGS)" -o gitmaster .

install:
	go install -ldflags "$(LDFLAGS)" .

run: build
	./gitmaster

test:
	go test ./...

vet:
	go vet ./...

# check compiles for every supported platform and runs the tests.
check: vet test
	@for p in $(PLATFORMS); do \
		GOOS=$${p%/*} GOARCH=$${p#*/} go build -o /dev/null . || exit 1; \
		echo "ok $$p"; \
	done

# build-all writes dist/gitmaster-<os>-<arch> for every supported platform.
build-all:
	@mkdir -p dist
	@for p in $(PLATFORMS); do \
		GOOS=$${p%/*} GOARCH=$${p#*/} go build -ldflags "$(LDFLAGS)" -o dist/gitmaster-$${p%/*}-$${p#*/} . || exit 1; \
		echo "built dist/gitmaster-$${p%/*}-$${p#*/}"; \
	done
