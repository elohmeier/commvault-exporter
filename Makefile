GOFILES := $(shell find . -name '*.go' -not -path './vendor/*')

.PHONY: build ci docker fmt fmt-check test test-cover tidy-check vet

build:
	go build .

ci: fmt-check tidy-check vet test-cover build docker

docker:
	docker build -t commvault-exporter:local .

fmt:
	gofmt -w $(GOFILES)

fmt-check:
	@test -z "$$(gofmt -l $(GOFILES))"

test:
	go test ./...

test-cover:
	go test ./... -coverprofile=coverage.out -covermode=count
	go tool cover -func=coverage.out

tidy-check:
	go mod tidy
	git diff --exit-code -- go.mod go.sum

vet:
	go vet ./...
