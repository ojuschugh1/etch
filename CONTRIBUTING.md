# Contributing to Etch

Thanks for your interest in improving Etch.

## Reporting bugs & requesting features

If you find a bug, have a feature request, or run into any issues - just [open an issue](https://github.com/ojuschugh1/etch/issues).

Include:
- What happened
- What you expected
- Steps to reproduce (if applicable)
- Your OS and Go version

That's it.

## Development setup

```bash
git clone https://github.com/ojuschugh1/etch.git
cd etch
make build
make test
```

Requires Go 1.21+.

## Running tests

```bash
make test              # all tests
go test ./... -v       # verbose
go test -race ./...    # with race detector
```

## Code style

- Standard Go formatting (`gofmt`)
- Keep comments short and useful
- Tests go next to the code they test (`*_test.go`)
- Property-based tests use [rapid](https://github.com/flyingmutant/rapid)

## Focus areas

If you're looking for something to work on:

- Proxy improvements (performance, edge cases)
- Diff engine (better output, new formats)
- CLI UX (better messages, progress indicators)
- New language examples in `examples/`
