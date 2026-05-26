MODULE := github.com/paivot-ai/nd
BIN    := nd
PREFIX := $(HOME)/go/bin

PLUGIN_NAME     := nd
PLUGIN_SRC      := nd-skill
PLUGIN_CACHE    := $(HOME)/.claude/plugins/cache/$(PLUGIN_NAME)/$(PLUGIN_NAME)
SETTINGS_FILE   := $(HOME)/.claude/settings.json

.PHONY: help build test vet install install-plugin install-skill uninstall-plugin clean

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  %-15s %s\n", $$1, $$2}'

VERSION := $(shell git describe --tags --always 2>/dev/null || echo dev)
# Strip leading 'v' for plugin directory (v0.6.0 -> 0.6.0)
PLUGIN_VERSION := $(shell echo $(VERSION) | sed 's/^v//')

build: ## Build nd binary
	go build -ldflags "-X github.com/paivot-ai/nd/cmd.version=$(VERSION)" -o $(BIN) .

test: ## Run tests
	go test -v ./...

vet: ## Run go vet
	go vet ./...

install: build install-plugin ## Install nd binary and Claude Code plugin
	mkdir -p $(PREFIX)
	cp $(BIN) $(PREFIX)/$(BIN)

install-plugin: ## Install Claude Code plugin to ~/.claude/plugins
	@# -- Copy plugin files into the cache --
	@mkdir -p "$(PLUGIN_CACHE)/$(PLUGIN_VERSION)"
	@cp -R $(PLUGIN_SRC)/.claude-plugin $(PLUGIN_SRC)/skills \
		"$(PLUGIN_CACHE)/$(PLUGIN_VERSION)/"
	@# -- Update version in installed plugin.json --
	@sed -i '' 's/"version": *"[^"]*"/"version": "$(PLUGIN_VERSION)"/' \
		"$(PLUGIN_CACHE)/$(PLUGIN_VERSION)/.claude-plugin/plugin.json"
	@sed -i '' 's/"version": *"[^"]*"/"version": "$(PLUGIN_VERSION)"/' \
		"$(PLUGIN_CACHE)/$(PLUGIN_VERSION)/.claude-plugin/marketplace.json"
	@# -- Enable plugin in settings.json --
	@if [ ! -f "$(SETTINGS_FILE)" ]; then \
		mkdir -p "$$(dirname "$(SETTINGS_FILE)")"; \
		echo '{"enabledPlugins":{}}' > "$(SETTINGS_FILE)"; \
	fi
	@python3 -c "\
import json, sys; \
f='$(SETTINGS_FILE)'; \
d=json.load(open(f)); \
ep=d.setdefault('enabledPlugins',{}); \
key='$(PLUGIN_NAME)@$(PLUGIN_NAME)'; \
changed=ep.get(key)!=True; \
ep[key]=True; \
json.dump(d,open(f,'w'),indent=4); \
print('  plugin enabled in settings.json') if changed else print('  plugin already enabled')"
	@# -- Remove stale install at ~/.claude/skills/nd-skill if present --
	@if [ -d "$(HOME)/.claude/skills/nd-skill" ]; then \
		rm -rf "$(HOME)/.claude/skills/nd-skill"; \
		echo "  removed stale ~/.claude/skills/nd-skill"; \
	fi
	@echo "  nd plugin $(PLUGIN_VERSION) installed -- restart Claude Code to activate"

install-skill: install-plugin ## Alias for install-plugin (matches vlt convention)

uninstall-plugin: ## Remove Claude Code plugin
	@rm -rf "$(PLUGIN_CACHE)"
	@if [ -f "$(SETTINGS_FILE)" ]; then \
		python3 -c "\
import json; \
f='$(SETTINGS_FILE)'; \
d=json.load(open(f)); \
d.get('enabledPlugins',{}).pop('$(PLUGIN_NAME)@$(PLUGIN_NAME)',None); \
json.dump(d,open(f,'w'),indent=4)"; \
		echo "  plugin removed from settings.json"; \
	fi
	@echo "  nd plugin uninstalled"

clean: ## Remove build artifacts
	rm -f $(BIN)
