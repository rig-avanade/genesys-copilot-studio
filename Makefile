.PHONY: test build image

test:
	go test -race ./...
	go vet ./...

build:
	CGO_ENABLED=0 go build -trimpath -o bin/ ./cmd/...

image:
	docker build -t genesys-copilot-studio:local .
