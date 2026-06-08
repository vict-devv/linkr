# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

**linkr** is a Go project. The repository is in early initialization — no source files exist yet.

## Common Commands

Once source files are added, standard Go commands apply:

```sh
go build ./...          # build all packages
go test ./...           # run all tests
go test ./pkg/foo/...   # run tests in a specific package
go test -run TestName   # run a single test by name
go vet ./...            # lint/static analysis
go run .                # run the main package
```

## Architecture

To be documented as the project takes shape.
