.PHONY: test test-race

test:
	go test ./...

test-race:
	docker build -f Dockerfile.test -t sentinel-agent-test:local .
	docker run --rm -v "$(CURDIR):/src" -w /src sentinel-agent-test:local
