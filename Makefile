APP ?= clickcannon
IMAGE ?= clickcannon-builder
DOCKER ?= docker
GO ?= go
GOOS ?= linux
GOARCH ?= amd64
CGO_ENABLED ?= 0
GOFLAGS ?= -mod=vendor -buildvcs=false
TAGS ?= netgo osusergo
LDFLAGS ?= -s -w

.PHONY: all build linux-static docker-build clean

all: linux-static

build:
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -trimpath $(GOFLAGS) -tags "$(TAGS)" -ldflags "$(LDFLAGS)" -o $(APP) .

linux-static: build

docker-build:
	$(DOCKER) build -t $(IMAGE) .
	$(DOCKER) create --name $(IMAGE)-tmp $(IMAGE) >/dev/null
	$(DOCKER) cp $(IMAGE)-tmp:/root/clickcannon ./$(APP)
	$(DOCKER) rm $(IMAGE)-tmp >/dev/null

clean:
	rm -f $(APP)
