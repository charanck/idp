.PHONY: test build

# -p 1 is required: every package's tests share one ephemeral Postgres
# (CP_TEST_DATABASE_URL) and truncate its tables between tests, which races
# across packages if the go tool runs them concurrently (its default).
test:
	go test -p 1 ./...

build:
	go build ./...
