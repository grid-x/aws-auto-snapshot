GO_FILES := $(shell find . -type f -name "*.go")
GO_BUILD := CGO_ENABLED=0 go build -ldflags="-w -s"
GO_TOOLS := gridx/golang-tools:master-839443d
GO_PROJECT := github.com/grid-x/aws-auto-snapshot
DOCKER_RUN := docker run -e CI=$${CI} -it ${DOCKER_LINK} --rm -v $$PWD:/go/src/${GO_PROJECT} -w /go/src/${GO_PROJECT}
GO_RUN := ${DOCKER_RUN} ${GO_TOOLS} bash -c

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
	${GO_BUILD} -o $@ ./cmd/snapshotter

bin/snapshotter.linux: ${GO_FILES}
	GOOS=linux CGO_ENABLED=0 ${GO_BUILD} -o $@ ./cmd/snapshotter

docker: bin/snapshotter.linux Dockerfile
	docker build -t ${IMAGE_URL} -f Dockerfile .

push: docker
	docker push ${IMAGE_URL}

ci_build:
	${GO_RUN} "make bin/snapshotter.linux"

ci_test:
	${GO_RUN} "make test"

ci_lint:
	${GO_RUN} "make lint"
