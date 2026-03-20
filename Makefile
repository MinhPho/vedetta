.PHONY: build build-capi run test bench lint clean fmt check

BINARY := watchpost
BUILD_DIR := ./build

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
