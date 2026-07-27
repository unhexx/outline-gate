.PHONY: build test vet lint docker-build run clean

BINARY  ?= outline-gate
CMD     ?= ./cmd/outline-gate
IMAGE   ?= outline-gate:local
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || echo 0.4.0)
LDFLAGS ?= -s -w -X github.com/unhexx/outline-gate/internal/version.Version=$(VERSION)

build:
	go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(BINARY) $(CMD)

test:
	go test ./...

vet:
	go vet ./...

docker-build:
	docker build -f deploy/docker/Dockerfile --build-arg VERSION=$(VERSION) -t $(IMAGE) .

run: build
	./bin/$(BINARY)

clean:
	rm -rf bin/
