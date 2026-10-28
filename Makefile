# Quarters — common tasks. Needs Go, Node.js and the Wails CLI
# (go install github.com/wailsapp/wails/v2/cmd/wails@latest).

WAILS ?= $(shell command -v wails || echo $(HOME)/go/bin/wails)
APP   := build/bin/Quarters.app

.PHONY: all deps vendor build install run dev test lint fmt mascot symbols server clean

all: build

deps: ## install the frontend packages
	cd frontend && npm ci

vendor: deps ## copy xterm.js and the font into frontend/dist/vendor
	cd frontend && npm run build

build: vendor ## build Quarters.app (Apple silicon)
	$(WAILS) build -clean -platform darwin/arm64

server: vendor ## the remote-access server alone, no window, for Linux (amd64 and arm64)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags server -trimpath -ldflags "-s -w" -o build/bin/quarters-linux-amd64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -tags server -trimpath -ldflags "-s -w" -o build/bin/quarters-linux-arm64 .

install: build ## copy the app into /Applications
	rsync -a --delete $(APP)/ /Applications/Quarters.app/

run: build ## build and open the app
	open $(APP)

dev: vendor ## run with live reload
	$(WAILS) dev

test: vendor ## run the tests (with the race detector)
	go test -race ./...

lint: ## gofmt, go vet and staticcheck
	@test -z "$$(gofmt -l .)" || (gofmt -l . && echo "run: make fmt" && exit 1)
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...
	node --check frontend/dist/app.js && node --check frontend/dist/remote.js && node --check frontend/dist/sw.js

fmt: ## format the Go code
	gofmt -w .

symbols: ## rebuild the terminal's symbols font (needs fonttools and brotli)
	python3 tools/symbols/build.py

mascot: ## redraw the mascot SVGs from tools/mascot/build.mjs
	node tools/mascot/build.mjs

clean:
	rm -rf build/bin frontend/dist/vendor
