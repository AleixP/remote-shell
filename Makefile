# Remote Shell: commands for the agent (the Go API in agent/).
#
#   make            show this help
#   make help       same
#
# Every target documented with "## text" after its name appears in the help.
# Variables can be overridden on the command line: make run TLS_CERT=a.crt TLS_KEY=a.key

.DEFAULT_GOAL := help
SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c

# == Settings ==

BIN          := bin/remote-shell
AGENT_DIR    := agent
GO           := cd $(AGENT_DIR) && go
VERSION      ?= dev
PKG_VERSION  := github.com/AleixP/remote-shell/agent/internal/server.Version
LDFLAGS      := -s -w -X $(PKG_VERSION)=$(VERSION)

# Needed by run, doctor and service-install.
TLS_CERT     ?=
TLS_KEY      ?=
# Extra agent flags, for example: make run FLAGS="-port 9443 -log-level debug"
FLAGS        ?=

# Needed by revoke.
ID           ?=
# Duration of the soak test in seconds.
SOAK_SECONDS ?= 300

# ---- Help -------------------------------------------------------------------

.PHONY: help
help: ## Show this help
	@awk 'BEGIN { FS = ":.*## "; printf "\nUsage: make \033[36m<target>\033[0m [VARIABLE=value]\n" } \
		/^# -+ .* -+$$/ { sub(/^# -+ /, ""); sub(/ -+$$/, ""); printf "\n\033[1m%s\033[0m\n", $$0 } \
		/^[a-zA-Z0-9_-]+:.*## / { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
	@printf '\nVariables:\n'
	@printf '  \033[33m%-18s\033[0m %s\n' \
		'TLS_CERT, TLS_KEY' 'certificate and key (run, doctor, service-install)' \
		'FLAGS'             'extra agent flags, e.g. FLAGS="-port 9443 -log-level debug"' \
		'ID'                'device id (revoke)' \
		'VERSION'           'version baked into the binary (build, release)' \
		'SOAK_SECONDS'      'soak test duration, default $(SOAK_SECONDS)'
	@printf '\nExamples:\n'
	@printf '  make run TLS_CERT=host.crt TLS_KEY=host.key\n'
	@printf '  make revoke ID=3f2a\n'
	@printf '  make release VERSION=1.2.3\n\n'

# ---- Build and run ----------------------------------------------------------

.PHONY: build
build: ## Build the agent into bin/remote-shell
	@mkdir -p bin
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o ../$(BIN) ./cmd/remote-shell
	@echo "built $(BIN) ($(VERSION))"

.PHONY: run
run: build ## Build and run the agent in the foreground (needs TLS_CERT and TLS_KEY)
	@test -n "$(TLS_CERT)" -a -n "$(TLS_KEY)" || { echo "error: set TLS_CERT and TLS_KEY, e.g. make run TLS_CERT=host.crt TLS_KEY=host.key" >&2; exit 2; }
	./$(BIN) -tls-cert "$(abspath $(TLS_CERT))" -tls-key "$(abspath $(TLS_KEY))" $(FLAGS)

.PHONY: version
version: build ## Print the version of the built agent
	@./$(BIN) version

.PHONY: flags
flags: build ## List every agent flag with its default
	@./$(BIN) -h 2>&1 | sed '1d' || true

# ---- Tests and quality ------------------------------------------------------

.PHONY: test
test: ## Run the agent tests
	$(GO) test -count=1 ./...

.PHONY: test-race
test-race: ## Run the agent tests with the race detector (what CI runs)
	$(GO) test -race -count=1 -timeout 5m ./...

.PHONY: cover
cover: ## Run the tests and show coverage per package
	$(GO) test -count=1 -cover ./...

.PHONY: cover-html
cover-html: ## Write coverage.html and open it in a browser
	$(GO) test -count=1 -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "wrote $(AGENT_DIR)/coverage.html"
	@(command -v open >/dev/null && open $(AGENT_DIR)/coverage.html) || (command -v xdg-open >/dev/null && xdg-open $(AGENT_DIR)/coverage.html) || true

.PHONY: soak
soak: ## Long-running stability test (SOAK_SECONDS, default 300)
	RS_SOAK_SECONDS=$(SOAK_SECONDS) $(GO) test -count=1 -run TestSoak -v -timeout $$(( $(SOAK_SECONDS) + 120 ))s ./internal/server

.PHONY: vet
vet: ## Run go vet for this system and for Windows
	$(GO) vet ./...
	GOOS=windows $(GO) vet ./...

.PHONY: fmt
fmt: ## Format the Go code
	$(GO) fmt ./...

.PHONY: fmt-check
fmt-check: ## Fail if any Go file is not formatted
	@out=$$(cd $(AGENT_DIR) && gofmt -l .); if [ -n "$$out" ]; then echo "not formatted:"; echo "$$out"; exit 1; fi

.PHONY: tidy
tidy: ## Tidy go.mod and go.sum
	$(GO) mod tidy

.PHONY: check
check: fmt-check vet test-race ## Everything CI checks: formatting, vet and race tests

# ---- Operating the agent ----------------------------------------------------

.PHONY: pair
pair: build ## Show a pairing QR code for a new phone (the agent must be running)
	./$(BIN) pair

.PHONY: devices
devices: build ## List the paired devices
	./$(BIN) devices list

.PHONY: revoke
revoke: build ## Revoke a paired device (ID=<device id>)
	@test -n "$(ID)" || { echo "error: set ID, e.g. make revoke ID=3f2a (see make devices)" >&2; exit 2; }
	./$(BIN) devices revoke "$(ID)"

.PHONY: doctor
doctor: build ## Check Tailscale, certificate, MagicDNS, permissions and exposure
	@test -n "$(TLS_CERT)" -a -n "$(TLS_KEY)" || { echo "error: set TLS_CERT and TLS_KEY" >&2; exit 2; }
	./$(BIN) doctor -tls-cert "$(abspath $(TLS_CERT))" -tls-key "$(abspath $(TLS_KEY))" $(FLAGS)

# ---- Background service (per user, no admin rights) -------------------------

.PHONY: service-install
service-install: build ## Install and start the agent as a background service
	@test -n "$(TLS_CERT)" -a -n "$(TLS_KEY)" || { echo "error: set TLS_CERT and TLS_KEY" >&2; exit 2; }
	./$(BIN) service install -tls-cert "$(abspath $(TLS_CERT))" -tls-key "$(abspath $(TLS_KEY))" $(FLAGS)

.PHONY: service-status
service-status: build ## Show whether the service is installed and running
	./$(BIN) service status

.PHONY: service-start
service-start: build ## Start the installed service
	./$(BIN) service start

.PHONY: service-stop
service-stop: build ## Stop the service (it stays installed)
	./$(BIN) service stop

.PHONY: service-uninstall
service-uninstall: build ## Stop and remove the service
	./$(BIN) service uninstall

# ---- Release and cleanup ----------------------------------------------------

.PHONY: release
release: ## Build archives for every platform into dist/ (VERSION=1.2.3 required)
	@test "$(VERSION)" != "dev" || { echo "error: set VERSION, e.g. make release VERSION=1.2.3" >&2; exit 2; }
	scripts/build-release.sh "$(VERSION)"

.PHONY: clean
clean: ## Remove build output (bin/, dist/ and coverage files)
	rm -rf bin dist $(AGENT_DIR)/coverage.out $(AGENT_DIR)/coverage.html
