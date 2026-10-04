.PHONY: build test vet fmt check cross clean

build:
	go build -o bin/llmctl ./cmd/llmctl

# A hung test fails in two minutes rather than the default ten.
test:
	go test -timeout 2m ./...

vet:
	go vet ./...

# Unformatted code fails the build rather than becoming a review comment.
fmt:
	@test -z "$$(gofmt -l . )" || (echo "unformatted files:"; gofmt -l .; exit 1)

# The isolation guard is part of the standard check, not an optional extra:
# it is the cheapest evidence that the multi-platform path stays open.
check: fmt vet test
	./scripts/check_os_isolation.sh

# Proves the zero-CGO decision holds. A macOS machine must be able to produce
# a Windows binary without a C toolchain.
cross:
	GOOS=windows GOARCH=amd64 go build -o /dev/null ./...
	GOOS=darwin  GOARCH=arm64 go build -o /dev/null ./...
	GOOS=linux   GOARCH=amd64 go build -o /dev/null ./...

clean:
	rm -rf bin/
