GO ?= go
.PHONY: build plugin test
build:
	mkdir -p bin
	$(GO) build -trimpath -o bin/tincan ./cmd/tincan
plugin:
	mkdir -p plugins/tincan/bin
	$(GO) build -trimpath -o plugins/tincan/bin/tincan ./cmd/tincan
test:
	$(GO) test -race ./...
	$(GO) vet ./...
	python3 -m unittest discover -s sdk/python -p 'test_*.py'
