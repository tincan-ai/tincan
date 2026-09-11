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

# Optional native adapter contract tests require Node.
.PHONY: test-adapters
test-adapters:
	python3 -m unittest discover -s sdk/python -p 'test_*.py'
	node --test plugins/tincan/native/openclaw/index.test.mjs
