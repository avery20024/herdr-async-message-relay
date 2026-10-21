.PHONY: build generate test check clean

VERSION := $(shell tr -d '\r\n' < VERSION)

build:
	go build -trimpath -ldflags "-X main.version=$(VERSION)" -o bin/hrelay ./cmd/hrelay

generate:
	sqlc generate

test:
	go test -race ./...
	python3 -m unittest discover -s scripts -p 'test_*.py'

check:
	@test -z "$$(gofmt -l cmd internal db)" || (echo '請執行 gofmt'; exit 1)
	go vet ./...
	go test -race ./...
	python3 -m unittest discover -s scripts -p 'test_*.py'
	$(MAKE) build

clean:
	rm -rf bin dist
