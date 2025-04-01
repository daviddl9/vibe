VERSION ?= $(shell git describe --tags --always --dirty)
COMMIT  ?= $(shell git rev-parse --short HEAD)
DATE    ?= $(shell git log -1 --format=%cd --date=format:"%Y-%m-%dT%H:%M:%S")

LDFLAGS=-ldflags "\
	-X github.com/skyforclouds/webconsole/cli/internal/version.Version=${VERSION} \
	-X github.com/skyforclouds/webconsole/cli/internal/version.GitCommit=${COMMIT} \
	-X github.com/skyforclouds/webconsole/cli/internal/version.GitCommitDate=${DATE}"

.PHONY: build
build:
	go build ${LDFLAGS} -o vibe

.PHONY: install
install: build shell-completion
	chmod a+x vibe
	sudo cp vibe /usr/local/bin
	rm -f vibe

.PHONY: check
check:
	go fmt ./...
	golangci-lint run

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: shell-completion
shell-completion:
	@echo "Installing shell completions..."
	@mkdir -p ~/.zsh/completion
	./vibe completion zsh > ~/.zsh/completion/_vibe
	# ./vibe completion bash | sudo tee /etc/bash_completion.d/vibe > /dev/null
	# @echo "Completions installed for zsh and bash"
	# @echo "Note: For zsh, ensure '~/.zsh/completion' is in your fpath"

.PHONY: test
test:
	go test -v ./...
