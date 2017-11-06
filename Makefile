GO_VERSION=1.8
GO_FILES=$(shell find . -type f -name "*.go")
BIN_DIR ?= bin
BRANCH := $(shell git branch | sed -n -e 's/^\* \(.*\)/\1/p' | sed -e 's/\//_/g')
TAG := ${BRANCH}-$(shell git rev-parse --short HEAD)
IMAGE_URL := gridx/lightsail-auto-snapshot:${TAG}

all: bin/snapshotter

test:
	go test -v $(glide nv)

bin:
	mkdir -p bin
clean:
	rm -f bin/*

bin/snapshotter: ${GO_FILES} bin
	go build -o $@

bin/snapshotter.linux: ${GO_FILES}
	CGO_ENABLED=0 go build -o $@

docker: bin/snapshotter.linux Dockerfile
	docker build -t ${IMAGE_URL} -f Dockerfile .

push: docker
	docker push ${IMAGE_URL}

ci:
	docker run --rm -v "$$PWD:/go/src/github.com/grid-x/lightsail-auto-snapshotter" -w /go/src/github.com/grid-x/lightsail-auto-snapshotter golang:${GO_VERSION} bash -c 'make bin/snapshotter.linux'

ci_test:
	docker run --rm -v "$$PWD:/go/src/github.com/grid-x/lightsail-auto-snapshotter" -w /go/src/github.com/grid-x/lightsail-auto-snapshotter golang:${GO_VERSION} bash -c ' make test'
