DEV_IMAGE := embyproxy-dev:go1.26.4

.PHONY: test test-image test-race test-race-core

test-image:
	docker build -f Dockerfile.dev -t $(DEV_IMAGE) .

test: test-image
	docker run --rm -v "$(CURDIR):/workspace" -w /workspace $(DEV_IMAGE) go test ./...

test-race: test-image
	docker run --rm -e CGO_ENABLED=1 -v "$(CURDIR):/workspace" -w /workspace $(DEV_IMAGE) \
		go test -race ./internal/storage ./internal/proxy ./internal/failover ./internal/admin ./internal/mediaproxy

test-race-core: test-race
