# node-host Makefile
# Local development commands only

.PHONY: build
build:
	yarn install --frozen-lockfile
	yarn proto:gen

.PHONY: run
run:
	yarn start

.PHONY: clean
clean:
	rm -rf node_modules src/proto
