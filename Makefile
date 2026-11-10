.PHONY: up down reset test test-local logs

# Start the whole stack (Postgres, migrations + seed, API).
up:
	docker compose up --build

down:
	docker compose down

# Drop the database volume so the next `make up` re-seeds from scratch.
reset:
	docker compose down -v

# Run all tests (unit + integration) in containers against a throwaway Postgres.
# No Go toolchain, Postgres or API key needed on the host.
test:
	docker compose --profile test run --rm --build test; \
	status=$$?; docker compose --profile test rm -sf postgres-test >/dev/null; exit $$status

# Same tests with the local Go toolchain. Integration tests run only when
# TEST_DATABASE_URL points at a Postgres instance; otherwise they are skipped.
test-local:
	cd backend && go test -race -count=1 ./...

logs:
	docker compose logs -f api
