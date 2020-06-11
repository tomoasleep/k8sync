GO111MODULES=on
NAME?=k8sync

REVISION := $(shell git rev-parse --short HEAD)
SRCS := $(shell find . -type f -name '*.go')

.DEFAULT_GOAL := bin/$(NAME)

bin/$(NAME): $(SRCS)
	go build -o bin/$(NAME)

.PHONY: clean
clean:
	rm -rf bin/*
	rm -rf dist/*
