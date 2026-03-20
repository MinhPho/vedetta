.PHONY: build build-capi run test bench lint clean fmt check docker-build docker-push

BINARY := watchpost
BUILD_DIR := ./build
DOCKER_IMAGE := ghcr.io/rvben/watchpost
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

build:
	go build -o $(BUILD_DIR)/$(BINARY) ./cmd/watchpost

build-capi:
	go build -tags cgo_onnxruntime -o $(BUILD_DIR)/$(BINARY) ./cmd/watchpost

run: build
	$(BUILD_DIR)/$(BINARY) -config config.example.yml

test:
	go test ./...

bench:
	go test ./internal/detect/ -bench=. -benchmem -count=1

lint:
	golangci-lint run ./...

clean:
	rm -rf $(BUILD_DIR)
	rm -f watchpost.db

fmt:
	gofmt -w .

check: lint test

docker-build:
	docker build -t $(DOCKER_IMAGE):$(VERSION) -t $(DOCKER_IMAGE):latest .

docker-push:
	docker push $(DOCKER_IMAGE):$(VERSION)
	docker push $(DOCKER_IMAGE):latest
