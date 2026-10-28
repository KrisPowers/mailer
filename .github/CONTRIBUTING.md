# Contributing

Thanks for helping. Bug reports, fixes and small features are all welcome; for
anything large, open an issue first so the approach can be agreed before you
build it.

## Getting set up

See [Development](../docs/development.md) for building and testing the server, the
dashboard and the container.

## Pull requests

- Keep each pull request to one change.
- Run `go vet ./...` and `go test ./...`, and add tests for new behavior.
- If you changed `internal/admin/ui`, rebuild and commit `internal/admin/web`.
- Update the docs in `docs/` when behavior or settings change.
- Write commit messages as `<type>(<scope>): <summary>`, for example
  `fix(imap): keep flags on copy`.

## Secrets

Never commit a data folder, a `.env`, or any token, key or real address. Tests use
`example.com` and documentation IP ranges.
