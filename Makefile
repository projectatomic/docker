.PHONY: all binary build cross default docs docs-build docs-shell shell test test-docker-py test-integration-cli test-unit validate

# get OS/Arch of docker engine
DOCKER_OSARCH := $(shell bash -c 'source hack/make/.detect-daemon-osarch && echo $${DOCKER_ENGINE_OSARCH:+$$DOCKER_CLIENT_OSARCH}')
# default for linux/amd64 and others
DOCKERFILE := $(shell go run ./distros/gen_dockerfile.go)
# switch to different Dockerfile for linux/arm
ifeq ($(DOCKER_OSARCH), linux/arm)
	DOCKERFILE := Dockerfile.armhf
else
ifeq ($(DOCKER_OSARCH), linux/arm64)
	# TODO .arm64
	DOCKERFILE := Dockerfile.armhf
else
ifeq ($(DOCKER_OSARCH), linux/ppc64le)
	DOCKERFILE := Dockerfile.ppc64le
else
ifeq ($(DOCKER_OSARCH), linux/s390x)
	DOCKERFILE := Dockerfile.s390x
else
ifeq ($(DOCKER_OSARCH), windows/amd64)
	DOCKERFILE := Dockerfile.windows
endif
endif
endif
endif
endif
export DOCKERFILE

# env vars passed through directly to Docker's build scripts
# to allow things like `make DOCKER_CLIENTONLY=1 binary` easily
# `docs/sources/contributing/devenvironment.md ` and `project/PACKAGERS.md` have some limited documentation of some of these
DOCKER_ENVS := \
	-e BUILDFLAGS \
	-e DOCKER_BUILD_PKGS \
	-e DOCKER_CLIENTONLY \
	-e DOCKER_DEBUG \
	-e DOCKER_EXPERIMENTAL \
	-e DOCKERFILE \
	-e DOCKER_GRAPHDRIVER \
	-e DOCKER_REMAP_ROOT \
	-e DOCKER_STORAGE_OPTS \
	-e DOCKER_USERLANDPROXY \
	-e TESTDIRS \
	-e TESTFLAGS \
	-e TIMEOUT
# note: we _cannot_ add "-e DOCKER_BUILDTAGS" here because even if it's unset in the shell, that would shadow the "ENV DOCKER_BUILDTAGS" set in our Dockerfile, which is very important for our official builds

# to allow `make BIND_DIR=. shell` or `make BIND_DIR= test`
# (default to no bind mount if DOCKER_HOST is set)
# note: BINDDIR is supported for backwards-compatibility here
BIND_DIR := $(if $(BINDDIR),$(BINDDIR),$(if $(DOCKER_HOST),,bundles))
DOCKER_MOUNT := $(if $(BIND_DIR),-v "$(CURDIR)/$(BIND_DIR):/go/src/github.com/docker/docker/$(BIND_DIR)")


GIT_BRANCH := $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null)
DOCKER_IMAGE := docker-dev$(if $(GIT_BRANCH),:$(GIT_BRANCH))
DOCKER_DOCS_IMAGE := docker-docs$(if $(GIT_BRANCH),:$(GIT_BRANCH))

DOCKER_FLAGS := docker run --rm -i --privileged $(DOCKER_ENVS) $(DOCKER_MOUNT)

# if this session isn't interactive, then we don't want to allocate a
# TTY, which would fail, but if it is interactive, we do want to attach
# so that the user can send e.g. ^C through.
INTERACTIVE := $(shell [ -t 0 ] && echo 1 || echo 0)
ifeq ($(INTERACTIVE), 1)
	DOCKER_FLAGS += -t
endif

DOCKER_RUN_DOCKER := $(DOCKER_FLAGS) "$(DOCKER_IMAGE)"

default: binary

all: build
	$(DOCKER_RUN_DOCKER) hack/make.sh

binary: build
	$(DOCKER_RUN_DOCKER) hack/make.sh binary

build: bundles
	docker build ${DOCKER_BUILD_ARGS} -t "$(DOCKER_IMAGE)" -f "$(DOCKERFILE)" .

bundles:
	mkdir bundles

cross: build
	$(DOCKER_RUN_DOCKER) hack/make.sh dynbinary binary cross

deb: build
	$(DOCKER_RUN_DOCKER) hack/make.sh dynbinary build-deb

docs:
	$(MAKE) -C docs docs

gccgo: build
	$(DOCKER_RUN_DOCKER) hack/make.sh gccgo

rpm: build
	$(DOCKER_RUN_DOCKER) hack/make.sh dynbinary build-rpm

shell: build
	$(DOCKER_RUN_DOCKER) bash

test: build
	$(DOCKER_RUN_DOCKER) hack/make.sh dynbinary cross test-unit test-integration-cli test-docker-py

test-docker-py: build
	$(DOCKER_RUN_DOCKER) hack/make.sh dynbinary test-docker-py

test-integration-cli: build
	$(DOCKER_RUN_DOCKER) hack/make.sh dynbinary test-integration-cli

test-unit: build
	$(DOCKER_RUN_DOCKER) hack/make.sh test-unit

validate: build
	$(DOCKER_RUN_DOCKER) hack/make.sh validate-dco validate-gofmt validate-pkg validate-lint validate-test validate-toml validate-vet validate-vendor
