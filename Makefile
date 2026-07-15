.PHONY: build test vet lint docker-build run clean

BINARY ?= outline-gate
CMD    ?= ./cmd/outline-gate
IMAGE  ?= outline-gate:local

build:
	go build -o bin/$(BINARY) $(CMD)

test:
	go test ./...

vet:
	go vet ./...

docker-build:
	docker build -f deploy/docker/Dockerfile -t $(IMAGE) .

run: build
	./bin/$(BINARY)

clean:
	rm -rf bin/
