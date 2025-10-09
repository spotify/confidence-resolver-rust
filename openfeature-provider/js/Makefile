# openfeature-provider/js Makefile
# TypeScript OpenFeature provider

.PHONY: build
build:
	yarn install --frozen-lockfile
	yarn proto:gen
	yarn build

.PHONY: test
test:
	yarn test --run --exclude='**/*.e2e.test.ts'

.PHONY: clean
clean:
	rm -rf node_modules dist src/proto

