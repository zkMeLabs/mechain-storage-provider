SHELL := /bin/bash

.PHONY: all build clean format install-tools generate lint mock-gen test tidy vet buf-gen proto-clean
.PHONY: install-go-test-coverage check-coverage

help:
	@echo "Please use \`make <target>\` where <target> is one of"
	@echo "  build                 to create build directory and compile sp"
	@echo "  clean                 to remove build directory"
	@echo "  format                to format sp code"
	@echo "  generate              to generate mock code"
	@echo "  install-tools         to install mockgen, buf and protoc-gen-gocosmos tools"
	@echo "  lint                  to run golangci lint"
	@echo "  mock-gen              to generate mock files"
	@echo "  test                  to run all sp unit tests"
	@echo "  tidy                  to run go mod tidy and verify"
	@echo "  vet                   to do static check"
	@echo "  buf-gen               to use buf to generate pb.go files"
	@echo "  proto-clean           to remove generated pb.go files"
	@echo "  proto-format          to format proto files"
	@echo "  proto-format-check    to check proto files"

build:
	bash +x ./build.sh

check-coverage:
	@go-test-coverage --config=./.testcoverage.yml || true

clean:
	rm -rf ./build

format:
	bash script/format.sh
	gofmt -w -l .

generate:
	go generate ./...

install-go-test-coverage:
	go install github.com/vladopajic/go-test-coverage/v2@latest

install-tools:
	go install go.uber.org/mock/mockgen@v0.1.0
	go install github.com/bufbuild/buf/cmd/buf@v1.28.0
	go install github.com/cosmos/gogoproto/protoc-gen-gocosmos@latest

lint:
	golangci-lint run --fix

mock-gen:
	mockgen -source=core/spdb/spdb.go -destination=core/spdb/spdb_mock.go -package=spdb
	mockgen -source=store/bsdb/database.go -destination=store/bsdb/database_mock.go -package=bsdb
	mockgen -source=core/task/task.go -destination=core/task/task_mock.go -package=task

# only run unit tests, exclude e2e tests
test:
	go test -failfast $$(go list ./... | grep -v e2e |grep -v modular/blocksyncer) -covermode=atomic -coverprofile=./coverage.out -timeout 99999s
	# go test -cover ./...
	# go test -coverprofile=coverage.out ./...
	# go tool cover -html=coverage.out

tidy:
	go mod tidy
	go mod verify

vet:
	go vet ./...

buf-gen:
	rm -rf ./base/types/*/*.pb.go && rm -rf ./modular/metadata/types/*.pb.go && rm -rf ./store/types/*.pb.go
	buf generate

proto-clean:
	rm -rf ./base/types/*/*.pb.go && rm -rf ./modular/metadata/types/*.pb.go && rm -rf ./store/types/*.pb.go

proto-format:
	buf format -w

proto-format-check:
	buf format --diff --exit-code

###############################################################################
###                        Docker                                           ###
###############################################################################
DOCKER := $(shell which docker)
DOCKER_IMAGE := zkmelabs/mechain-storage-provider
COMMIT_HASH := $(shell git rev-parse --short=7 HEAD)
DOCKER_TAG := $(COMMIT_HASH)

build-docker:
	$(DOCKER) build -t ${DOCKER_IMAGE}:${DOCKER_TAG} .
	$(DOCKER) tag ${DOCKER_IMAGE}:${DOCKER_TAG} ${DOCKER_IMAGE}:latest
	$(DOCKER) tag ${DOCKER_IMAGE}:${DOCKER_TAG} ${DOCKER_IMAGE}:${COMMIT_HASH}

.PHONY: build-docker
###############################################################################
###                        Docker Compose                                   ###
###############################################################################
build-dcf:
	go run cmd/ci/main.go

start-dc:
	docker compose up -d
	docker compose ps
	
stop-dc:
	docker compose down --volumes

.PHONY: build-dcf start-dc stop-dc

###############################################################################
###                                Releasing                                ###
###############################################################################

PACKAGE_NAME:=github.com/zkMeLabs/mechain-storage-provider
GOLANG_CROSS_VERSION  = v1.22
GOPATH ?= '$(HOME)/go'
release-dry-run:
	docker run \
		--rm \
		--privileged \
		-e CGO_ENABLED=1 \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v `pwd`:/go/src/$(PACKAGE_NAME) \
		-v ${GOPATH}/pkg:/go/pkg \
		-w /go/src/$(PACKAGE_NAME) \
		ghcr.io/goreleaser/goreleaser-cross:${GOLANG_CROSS_VERSION} \
		--clean --skip validate --skip publish --snapshot

release:
	@if [ ! -f ".release-env" ]; then \
		echo "\033[91m.release-env is required for release\033[0m";\
		exit 1;\
	fi
	docker run \
		--rm \
		--privileged \
		-e CGO_ENABLED=1 \
		--env-file .release-env \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v `pwd`:/go/src/$(PACKAGE_NAME) \
		-w /go/src/$(PACKAGE_NAME) \
		ghcr.io/goreleaser/goreleaser-cross:${GOLANG_CROSS_VERSION} \
		release --clean --skip validate

.PHONY: release-dry-run release