.PHONY: all fmt lint test race build frontend-build clean

GO          ?= go
NPM         ?= npm
FRONTENDDIR ?= web
BACKENDCMD  ?= ./cmd/amdgputop-web
BACKENDBIN  ?= amdgputop-web

all: fmt lint test frontend-build build

fmt:
	@gofmt -w ./cmd ./internal

lint:
	@$(GO) vet ./...

test:
	@$(GO) test ./...

race:
	@$(GO) test -race ./...

build:
	@$(GO) build -o $(BACKENDBIN) $(BACKENDCMD)

frontend-build:
	@$(NPM) --prefix $(FRONTENDDIR) run build

clean:
	@rm -f $(BACKENDBIN)
	@$(NPM) --prefix $(FRONTENDDIR) run clean || true
