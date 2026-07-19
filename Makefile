SHELL := /bin/sh
FILE  ?= patterns/basic-groove.fnl

.PHONY: build install run test vet fmt fmt-fix gates clean

build:
	go build -o bin/basso ./cmd/basso

install:
	go install ./cmd/basso
	@echo "installed to $$(go env GOPATH)/bin/basso"

run:
	go run ./cmd/basso play $(FILE)

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l .

fmt-fix:
	gofmt -w .

gates:
	gofmt -l . && go vet ./... && go test ./...

clean:
	rm -rf bin
