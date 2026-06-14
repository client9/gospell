SHELL := sh

.PHONY: help
.DEFAULT_GOAL := help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}' 

.PHONY: fixture
fixture: hunspell-en_US hunspell

hunspell:
	git clone --depth 1 https://github.com/hunspell/hunspell.git

hunspell-en_US:
	curl -L -o "$@.zip" https://github.com/en-wl/wordlist/releases/download/rel-2026.02.25/hunspell-en_US-2026.02.25.zip
	mkdir -p $@
	unzip -d $@ "$@.zip"

build: ## build module
	go build ./cmd/gospell

bench: ## run benchmarks
	go test -bench=. -benchmem 

test: fixture ## run all unit tests
	go test ./...

version: ## print OS, Go, and golangci versions
	@echo $$0
	@uname -a
	@go version
	@golangci-lint --version

cover: ## generate code coverage report
	rm -f cover.out
	go test -run='^Test' -coverprofile=cover.out -coverpkg=.
	go tool cover -func=cover.out

## NOTE: this downloads it's schema over the network
lintverify:
	golangci-lint config verify

fmt: ## reformat source code
	go mod tidy
	go fmt ./...

lint: ## lint and verify repo is already formatted
	go mod tidy
	git diff --exit-code -- go.mod go.sum
	test -z "$$(gofmt -l .)"
	golangci-lint run ./...

clean: ## remove any generated files
	go clean -i -cache -testcache
	rm -f *.out 
	rm -f *.prof
	rm -f ./gospell
	rm -rf hunspell
	rm -rf hunspell-en_US*
