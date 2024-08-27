.PHONY: build build-js build-go test

build: build-js build-go

build-js:
	docker build -t js-crdt:latest -f js/Dockerfile ./js

build-go:
	docker build -t go-crdt:latest -f go/Dockerfile ./go

test:
	cd test && go test -v

testsingle:
	cd test && go test -v -failfast -run ${TEST}
