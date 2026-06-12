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

# actionlint (GitHub Actions workflow linter), resolved the same way.
ACTIONLINT ?= $(shell command -v actionlint 2>/dev/null || echo "$(shell $(GO) env GOPATH)/bin/actionlint")

# checkmake (Makefile linter), resolved the same way. Configured by checkmake.ini.
CHECKMAKE ?= $(shell command -v checkmake 2>/dev/null || echo "$(shell $(GO) env GOPATH)/bin/checkmake")

# renovate-config-validator ships in the npm `renovate` package; run via npx so
# no global install is needed. The version is pinned (and bumped by Renovate,
# in sync with the CI pin) so the schema matches; override RENOVATE_VALIDATE to
# change it. An unpinned `renovate` can resolve to a stale npx cache.
RENOVATE_VALIDATE ?= npx --yes --package renovate@43.220.0 -- renovate-config-validator --strict

# Integration tests (Vagrant). VAGRANT can be set to e.g. "sudo vagrant" when the
# provider needs root; LAYOUTS restricts which disk layouts run (empty = all).
ITEST_DIR := test/integration
VAGRANT   ?= vagrant
LAYOUTS   ?=
ITEST_RUN := sudo REDO_BACKUP_BIN=/opt/itest/redo-backup LAYOUTS='$(LAYOUTS)' /opt/itest/run-tests.sh

.DEFAULT_GOAL := build

# --- Phony targets ----------------------------------------------------------
# checkmake only parses the first physical line of a .PHONY declaration, and its
# minphony/phonydeclared rules require all, clean, test, and the body-less
# `integration` aggregator to be declared PHONY — so those four are kept on the
# first line below; the remaining targets may wrap onto the continuation line.
.PHONY: all integration clean test build race vet fmt fmt-check lint actionlint checkmake \
        leakcheck renovate-validate install uninstall integration-up integration-run integration-destroy help

all: build ## Build everything (default)

build: ## Compile the binary into $(BUILDDIR)/$(BINARY)
	$(GO) build -ldflags '$(LDFLAGS)' -o $(BUILDDIR)/$(BINARY) $(PKG)

test: ## Run the unit tests
	$(GO) test ./...

race: ## Run the unit tests with the race detector
	$(GO) test -race ./...

# Go 1.26's experimental goroutine leak profile. The per-package TestMain in
# internal/leakcheck fails the run if any goroutine is left blocked on an
# unreachable concurrency primitive. The GOEXPERIMENT is what arms it; drop it
# once the profile is on by default (planned for Go 1.27).
leakcheck: ## Run the unit tests under the Go 1.26 goroutine leak detector
	GOEXPERIMENT=goroutineleakprofile $(GO) test ./...

vet: ## Run go vet
	$(GO) vet ./...

fmt: ## Format the source with gofumpt (via golangci-lint)
	$(GOLANGCI_LINT) fmt

fmt-check: ## Fail if any file is not gofumpt-formatted
	$(GOLANGCI_LINT) fmt --diff

lint: ## Run golangci-lint
	$(GOLANGCI_LINT) run ./...

actionlint: ## Lint the GitHub Actions workflows
	$(ACTIONLINT)

checkmake: ## Lint the Makefile
	$(CHECKMAKE) Makefile

renovate-validate: ## Validate renovate.json5 against the Renovate schema
	$(RENOVATE_VALIDATE)

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

integration: integration-up integration-run ## Run the full Vagrant integration suite (build, up, run)

integration-up: build ## Build the binary and boot/provision the integration VM
	cd $(ITEST_DIR) && $(VAGRANT) up

integration-run: ## Run the integration suite in the already-running VM (use LAYOUTS=... to filter)
	cd $(ITEST_DIR) && $(VAGRANT) ssh -c "$(ITEST_RUN)"

integration-destroy: ## Destroy the integration VM
	cd $(ITEST_DIR) && $(VAGRANT) destroy -f

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
