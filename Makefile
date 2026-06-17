MODULES := core aws messaging messaging/nats gormx gormx/migrate gormx/postgres gormx/sqlite httpx host cache otelx outbox testx

.PHONY: build vet test fmt tidy lint staticcheck release-patch release-minor $(MODULES)

build:
	@for m in $(MODULES); do echo "==> build $$m"; (cd $$m && go build ./...) || exit 1; done

vet:
	@for m in $(MODULES); do echo "==> vet $$m"; (cd $$m && go vet ./...) || exit 1; done

test:
	@for m in $(MODULES); do echo "==> test $$m"; (cd $$m && go test ./...) || exit 1; done

fmt:
	@gofmt -l -w $(MODULES)

tidy:
	@for m in $(MODULES); do echo "==> tidy $$m"; (cd $$m && go mod tidy) || exit 1; done

lint:
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not installed"; exit 1; }
	@for m in $(MODULES); do echo "==> lint $$m"; (cd $$m && golangci-lint run ./...) || exit 1; done

staticcheck:
	@command -v staticcheck >/dev/null 2>&1 || { echo "staticcheck not installed: go install honnef.co/go/tools/cmd/staticcheck@latest"; exit 1; }
	@for m in $(MODULES); do echo "==> staticcheck $$m"; (cd $$m && staticcheck ./...) || exit 1; done

# Cut a lockstep release: bump internal requires, stamp the CHANGELOG, then
# (after a confirmation prompt) commit, push, and tag every module. See
# scripts/release.sh for the full flow.
release-patch:
	@MODULES="$(MODULES)" scripts/release.sh patch

release-minor:
	@MODULES="$(MODULES)" scripts/release.sh minor
