# Build/test via a throwaway Go toolchain container (host may lack Go).
IMAGE ?= golang:1.22-alpine

.PHONY: build test vet run clean

build:
	docker run --rm -v $(PWD):/src -w /src $(IMAGE) \
		sh -c "CGO_ENABLED=0 go build -trimpath -o /src/conduitvpn ./cmd/conduitvpn"

test:
	docker run --rm -v $(PWD):/src -w /src $(IMAGE) go test ./...

vet:
	docker run --rm -v $(PWD):/src -w /src $(IMAGE) go vet ./...

run: build
	./conduitvpn

clean:
	rm -f conduitvpn
