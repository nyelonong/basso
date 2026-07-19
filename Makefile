SHELL := /bin/sh
FILE  ?= patterns/basic-groove.fnl

.PHONY: build install run test vet fmt fmt-fix gates clean

build:
	nix-shell --run 'go build -o bin/basso ./cmd/basso'

install:
	nix-shell --run 'go install ./cmd/basso && echo "installed to $$(go env GOPATH)/bin/basso"'

run:
	nix-shell --run 'go run ./cmd/basso play $(FILE)'

test:
	nix-shell --run 'go test ./...'

vet:
	nix-shell --run 'go vet ./...'

fmt:
	nix-shell --run 'gofmt -l .'

fmt-fix:
	nix-shell --run 'gofmt -w .'

gates:
	nix-shell --run 'gofmt -l . && go vet ./... && go test ./...'

clean:
	rm -rf bin
