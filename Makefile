GOTOOLCHAIN ?= local
export GOTOOLCHAIN

.PHONY: build test race vet fmt selfcheck docker clean

build:
	go build -o bin/portctl ./cmd/portctl

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -l .

selfcheck: build
	./bin/portctl selfcheck

docker:
	docker build -t nanhaiport:local .

clean:
	rm -rf bin
