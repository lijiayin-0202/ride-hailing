# ride-hailing 微服务常用命令（Windows 下可用 Git Bash / WSL 执行；cmd 用户可直接看命令内容）
MODULE   := github.com/lijayin-0202/radish-ride-service
VERSION  ?= $(shell git describe --tags --always 2>/dev/null || echo v0.0.1)
LDFLAGS  := -X main.Version=$(VERSION)

.PHONY: all
all: init build

.PHONY: init
init:
	go mod download
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	go install github.com/go-kratos/kratos/cmd/protoc-gen-go-http/v2@latest
	go install github.com/go-kratos/kratos/cmd/kratos/v2@latest

.PHONY: api
api: ## 依据 api/**/*.proto 与 internal/conf/conf.proto 重新生成 Go 代码（依赖 buf）
	buf dep update
	buf generate

.PHONY: build
build: ## 编译到 bin/
	go build -ldflags "$(LDFLAGS)" -o ./bin/ ./...

.PHONY: run
run: ## 直接运行（默认读 ./configs）
	go run ./cmd/ride-hailing -conf ./configs

.PHONY: test
test:
	go test ./... -cover

.PHONY: lint
lint:
	go vet ./...
	gofmt -l .

.PHONY: generate
generate: ## wire 依赖注入代码生成
	cd cmd/ride-hailing && wire

.PHONY: docker
docker:
	docker build -t radish-go/ride-hailing:$(VERSION) .

.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-12s\033[0m %s\n", $$1, $$2}'
