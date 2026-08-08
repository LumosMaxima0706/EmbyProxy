DEV_IMAGE := embyproxy-dev:go1.26.4

.PHONY: test test-image

test-image:
	docker build -f Dockerfile.dev -t $(DEV_IMAGE) .

test: test-image
	docker run --rm -v "$(CURDIR):/workspace" -w /workspace $(DEV_IMAGE) go test ./...
