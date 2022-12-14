SHELL := /bin/bash

.PHONY: all check format vet generate build tidy

help:
	@echo "Please use \`make <target>\` where <target> is one of"
	@echo "  check               to do static check"
	@echo "  build               to create bin directory and build"
	@echo "  generate            to generate code"

check: vet

format:
	gofmt -w -l .

vet:
	go vet ./...

generate:
	go generate ./...

build:
	go build ./...

tidy:
	go mod tidy
	go mod verify
