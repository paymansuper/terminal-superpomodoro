VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
UNAME := $(shell uname -s)

build:
	go build -ldflags "-s -w -X main.version=$(VERSION)" -o bin/superpomodoro ./cmd/superpomodoro
ifeq ($(UNAME),Darwin)
	swiftc -O -o bin/superpomodoro-float macos/main.swift
endif

run: build
	./bin/superpomodoro

install: build
	install -m 0755 bin/superpomodoro /usr/local/bin/superpomodoro
	ln -sf /usr/local/bin/superpomodoro /usr/local/bin/spomo
ifeq ($(UNAME),Darwin)
	install -m 0755 bin/superpomodoro-float /usr/local/bin/superpomodoro-float
endif

release-snapshot:
	goreleaser release --snapshot --clean

.PHONY: build run install release-snapshot
