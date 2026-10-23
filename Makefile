GO ?= go
NPM ?= npm

.PHONY: all fmt test race vet build web-install web-check web-build web-interop run docker-build
all: test build
fmt:
	$(GO) fmt ./...
test:
	$(GO) test ./...
race:
	$(GO) test -race ./...
vet:
	$(GO) vet ./...
build:
	$(GO) build ./cmd/server
web-install:
	cd web && $(NPM) ci
web-check:
	cd web && $(NPM) run lint && $(NPM) run test:i18n && $(NPM) run typecheck && $(NPM) run test
web-build:
	cd web && $(NPM) run build
	rm -rf internal/webui/dist
	cp -a web/dist internal/webui/dist
web-interop:
	cd web && $(NPM) run test:interop
run:
	$(GO) run ./cmd/server
docker-build:
	docker build -t mail-aggregator:local .
