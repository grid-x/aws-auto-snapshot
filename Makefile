GO_VERSION=1.9
GO_FILES=$(shell find . -type f -name "*.go")
BIN_DIR ?= bin
BRANCH := $(shell git branch | sed -n -e 's/^\* \(.*\)/\1/p' | sed -e 's/\//_/g')
TAG := ${BRANCH}-$(shell git rev-parse --short HEAD)
IMAGE_URL := gridx/aws-auto-snapshot:${TAG}

all: bin/snapshotter

test:
	go vet -v $(shell glide nv)
	go test -v $(shell glide nv)

lint:
	golint -set_exit_status $(shell glide nv)

bin:
	mkdir -p bin

clean:
	rm -f bin/*

bin/snapshotter: ${GO_FILES} bin
	go build -o $@ ./cmd/snapshotter

bin/snapshotter.linux: ${GO_FILES}
	GOOS=linux CGO_ENABLED=0 go build -o $@ ./cmd/snapshotter

docker: bin/snapshotter.linux Dockerfile
	docker build -t ${IMAGE_URL} -f Dockerfile .

push: docker
	docker push ${IMAGE_URL}

ci_deps:
	curl https://glide.sh/get | sh
	go get -u -v github.com/golang/lint/golint

ci:
	docker run --rm -v "$$PWD:/go/src/github.com/grid-x/aws-auto-snapshot" -w /go/src/github.com/grid-x/aws-auto-snapshot golang:${GO_VERSION} bash -c 'make bin/snapshotter.linux'

ci_test:
	docker run --rm -e CI=$$CI -v "$$PWD:/go/src/github.com/grid-x/aws-auto-snapshot" -w /go/src/github.com/grid-x/aws-auto-snapshot golang:${GO_VERSION} bash -c 'make ci_deps && make test'

ci_lint:
	docker run --rm -v "$$PWD:/go/src/github.com/grid-x/aws-auto-snapshot" -w /go/src/github.com/grid-x/aws-auto-snapshot golang:${GO_VERSION} bash -c 'make ci_deps && make lint'
