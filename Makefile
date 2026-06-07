# SPDX-License-Identifier: EUPL-1.2
#
# redo-backups build/install Makefile.
#
# Installation paths follow the usual conventions and are overridable, so the
# same Makefile works for a local `make install` (default PREFIX=/usr/local) and
# for distribution packaging such as a Gentoo ebuild, which calls:
#
#     emake PREFIX=/usr DESTDIR="${D}" install
#
# DESTDIR is the staging root (empty for a direct install); PREFIX is the install
# prefix; bindir/sysconfdir/docdir can be overridden individually if needed.

# --- Configurable paths -----------------------------------------------------
DESTDIR    ?=
PREFIX     ?= /usr/local
bindir     ?= $(PREFIX)/bin
# Configuration always lives under /etc by design (profiles in
# $(sysconfdir)/redo-backups/), regardless of PREFIX. Override for unusual roots.
sysconfdir ?= /etc
docdir     ?= $(PREFIX)/share/doc/redo-backups

# --- Build configuration ----------------------------------------------------
BINARY  := redo-backup
PKG     := ./cmd/redo-backup
BUILDDIR := bin
GO      ?= go
INSTALL ?= install

# Version embedded into the binary (overridable, e.g. by GoReleaser/CI).
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

# golangci-lint is invoked from PATH or, if absent there, from $(go env GOPATH)/bin.
GOLANGCI_LINT ?= $(shell command -v golangci-lint 2>/dev/null || echo "$(shell $(GO) env GOPATH)/bin/golangci-lint")

.DEFAULT_GOAL := build

# --- Phony targets ----------------------------------------------------------
.PHONY: all build test race vet fmt fmt-check lint clean install uninstall help

all: build ## Build everything (default)

build: ## Compile the binary into $(BUILDDIR)/$(BINARY)
	$(GO) build -ldflags '$(LDFLAGS)' -o $(BUILDDIR)/$(BINARY) $(PKG)

test: ## Run the unit tests
	$(GO) test ./...

race: ## Run the unit tests with the race detector
	$(GO) test -race ./...

vet: ## Run go vet
	$(GO) vet ./...

fmt: ## Format the source with gofmt
	gofmt -w .

fmt-check: ## Fail if any file is not gofmt-formatted
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "These files are not gofmt-formatted:"; echo "$$unformatted"; exit 1; \
	fi

lint: ## Run golangci-lint
	$(GOLANGCI_LINT) run ./...

clean: ## Remove build artifacts
	rm -rf $(BUILDDIR)

install: build ## Install the binary and example config under $(DESTDIR)
	$(INSTALL) -d $(DESTDIR)$(bindir)
	$(INSTALL) -m 0755 $(BUILDDIR)/$(BINARY) $(DESTDIR)$(bindir)/$(BINARY)
	$(INSTALL) -d $(DESTDIR)$(sysconfdir)/redo-backups
	$(INSTALL) -m 0644 examples/etc/redo-backups/example.conf \
		$(DESTDIR)$(sysconfdir)/redo-backups/example.conf
	$(INSTALL) -d $(DESTDIR)$(docdir)
	$(INSTALL) -m 0644 README.md docs/redo-format.md $(DESTDIR)$(docdir)/

uninstall: ## Remove the installed binary and example config
	rm -f $(DESTDIR)$(bindir)/$(BINARY)
	rm -f $(DESTDIR)$(sysconfdir)/redo-backups/example.conf
	-rmdir --ignore-fail-on-non-empty $(DESTDIR)$(sysconfdir)/redo-backups 2>/dev/null || true
	rm -f $(DESTDIR)$(docdir)/README.md $(DESTDIR)$(docdir)/redo-format.md
	-rmdir --ignore-fail-on-non-empty $(DESTDIR)$(docdir) 2>/dev/null || true

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
