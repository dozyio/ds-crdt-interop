.PHONY: build build-js build-go test

build: build-js build-go

build-js:
	docker build -t js-crdt:latest -f js/Dockerfile ./js

build-go:
	docker build -t go-crdt:latest -f go/Dockerfile ./go

test:
	@cd test && \
	if [ -z "${TEST}" ]; then \
		go test -v -failfast -test.count 1 -timeout 15m; \
	else \
		go test -v -failfast -test.count 1 -timeout 15m -run ${TEST}; \
	fi
