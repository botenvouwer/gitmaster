VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  = -s -w -X main.version=$(VERSION)

.PHONY: build install run test vet

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
