# Contributing to SuperPomodoro

Thanks for helping out. Bug reports, ideas and pull requests are all welcome.

## Development

```sh
make build        # builds bin/superpomodoro, plus bin/superpomodoro-float on macOS
make run          # builds and starts the timer
go test ./...     # runs the tests
```

Before opening a pull request please make sure `gofmt -l .` prints nothing and `go vet ./...` and `go test ./...` pass. CI runs the same checks and also compiles the Swift mini window on macOS.

The terminal app intentionally has no third party Go modules. Please keep it that way, it makes the binary small and the Homebrew build trivial.

## Adding a theme

For your own use you don't need to touch the code, a theme file in `~/.config/superpomodoro/themes/` is enough (see the README). To ship a new built-in theme, edit `cmd/superpomodoro/themes.go`. A theme is one line with a name and ten colors.

```go
mk("my-theme",
    "#focusA", "#focusB",   // gradient during focus
    "#breakA", "#breakB",   // gradient during a short break
    "#longA", "#longB",     // gradient during a long break
    "#text", "#muted", "#surface"),
```

Tune it on a dark background. Readability on other backgrounds is handled automatically. A theme only gets an animated scene if the scene really fits it, see `themeEffect` in `bg.go`.

## Releasing

Releases are fully automated by `.github/workflows/release.yml`.

1. Create the repository `paymansuper/homebrew-tap` once.
2. Add a fine grained token with write access to that tap as secret `HOMEBREW_TAP_TOKEN` in this repository.
3. Tag and push.

```sh
git tag v0.1.0
git push --tags
```

The workflow builds release binaries with GoReleaser, checks that the Swift window compiles on macOS, and writes the source formula with the new version and checksum into the tap.
