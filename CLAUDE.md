# Claude Instructions — module-fanart-tv

A client of one upstream service, fanart.tv, supplying artwork for content
another source identified. It runs out of process — `cmd/module-fanart-tv` is one
call to `host.Serve` — and its release cross-compiles binaries and tells
[`registry`](https://github.com/mosaic-media/registry) to catalogue them.

It is an **extension module**: nothing requires it, and a Platform gains it only
when a user installs it from that signed index. Anything reasoning from "this
compiles into the Platform binary" is reasoning about a build this module left,
which is what the bundled-key rules below turn on.

Fleet-wide conventions — commits, decision records, citation form, the roadmap —
are in [`architecture`](https://github.com/mosaic-media/architecture/blob/main/CLAUDE.md).
This file is what is specific to module-fanart-tv.

## The role it must never declare

`Manifest` declares `RoleArtwork` and `RoleSettingsUI`. `boundary_test.go`'s
`TestManifestDeclaresNoMetadataRole` fails if it ever declares one of four
others, each with its own reason in the test:

| Role | Why the test forbids it |
|---|---|
| `metadata` | this module cannot describe content, and declaring the role would falsely satisfy the composition check in [platform#23](https://github.com/mosaic-media/platform/blob/main/docs/adr/0023-metadata-as-required-capability.md) |
| `search` | it cannot be searched — you must already know which title you mean |
| `catalog` | it has no collections to browse |
| `stream` | it supplies artwork, never playable locations |

The metadata one is the dangerous one: its failure is neither a compile error nor
a red test but a deployment that boots and finds nothing. `Import` **refuses**
rather than returning an empty success — this module produces no `ContentRef`, so
nothing can legitimately route an import here and an empty success would let that
bug pass silently.

## The boundary

- **Import only [`sdk`](https://github.com/mosaic-media/sdk),
  [`contracts`](https://github.com/mosaic-media/contracts) and the standard
  library.** `boundary_test.go` parses the imports of every non-test `.go` file
  and errors on anything else; `contracts` is there because this module authors
  its own settings screen.
- **The test scans this package directory only** (`os.ReadDir(".")`, directories
  skipped). `cmd/module-fanart-tv` imports `sdk/host` to serve itself, which is
  what an extension module is and not a violation — but it also means **nothing
  here checks what `cmd/` imports.**
- **Nothing may be written to stdout.** go-plugin writes its handshake there.
  Observability goes through the SDK's ambient telemetry; do not print.

## The bundled project key

- `defaultAPIKey` (`capability.go`) is empty in an ordinary build and linked with
  `-ldflags -X github.com/mosaic-media/module-fanart-tv.defaultAPIKey=…` against
  `./cmd/module-fanart-tv`. **This repository's own `release.yml` is the only
  build that can apply it**, so **`FANART_PROJECT_KEY` belongs in this
  repository's secrets.** Unset, the job warns and links an empty key — the
  screen says so, and a user's own key still works.
- **`resolveKeys` is the only function that reads the variable.**
  `bundledKeyPresent` and `usingBundledKey` both ask `resolveKeys` rather than the
  variable, and the settings screen asks them — **changing that screen is when to
  re-check it.**
- **`Settings.APIKey` only ever holds the user's own key** (`ClientKey` is
  independent and optional). `configureModule` replaces the whole settings
  document, so a bundled key that reached that field would be written into a
  user's stored settings by the next control they touched.
- The screen **describes** the bundled key and never renders it; a stored key
  appears only through `maskKey`, as its last four characters.
- **The linker guard is not optional.** `-X` against a symbol path that no longer
  resolves is *silently ignored*: a rename, a package move or a mistyped module
  path links nothing, the build stays green, and every released binary ships an
  empty key — surfacing as "fanart.tv API key not set" at a user's install rather
  than in any gate. `linkercheck_test.go` (build tag `linkercheck`) links a canary
  through the same path and asserts both directions of `resolveKeys` and
  `usingBundledKey`. The path is spelled in `docker-compose.test.yml`,
  `.github/workflows/verify.yml` and `release.yml`'s `binaries` job — change them
  together.

## `fanart.go` is the anti-corruption layer

Every fanart.tv-ism stops there and the Platform learns none of them:

- **Two endpoints keyed by different identifier spaces.** Television is addressed
  by TVDB id and nothing else; a film by TMDB id, or IMDb as the compatibility
  path. `endpointFor` owns that, which is why `ArtworkRequest` hands over the
  whole identity set at once.
- **The response decodes into `map[string]json.RawMessage`, not a struct**,
  because its keys *are* the artwork types: a struct would silently drop every
  type this build predates. An unknown key is ignored rather than guessed at, and
  a value that is not an array skips that type rather than failing the response.
- **`artworkTypeSlot` is one table of data, not a switch**, so a renamed or added
  type is a one-line change. Verify it against fanart.tv's current documentation
  before claiming coverage: ignoring unknown keys means quiet under-delivery.
- **Numbers arrive quoted.** `likes` and `season` are strings on `image` and
  converted on the way out; decoding them as numbers would fail a title's whole
  artwork on one odd entry. An unparseable `likes` becomes rank 0.
- **`lang: "00"` means textless, not a language.** `languageOf` maps it to an
  empty string; carried through as a language, every textless image would look
  foreign.
- **`rankOf` adds `hdRankBonus` so an HD variant outranks its SD twin** — likes
  accumulate per image, so an older SD logo often outscores the HD one that
  replaced it. That tie-break corrects the source's own ranking and is the only
  ranking here: **the rest is a candidate set, and the Platform chooses.**
- **An empty answer with no error is normal**: no usable identity, a 404, or
  nothing for the title. Erroring would let an artwork lookup disrupt an import.

## The gate

**Do not run `go build`, `go test`, `go vet` or `gofmt` on this machine.**

```bash
docker compose -f docker-compose.test.yml run --rm test
```

That runs `scripts/adr_lint.py`, gofmt, `go build`, `go vet`, `go test` and the
tagged `linkercheck` canary pass, against the Go version pinned in the compose
file — keep it equal to `go.mod`'s; append `bash` for a shell in the same
environment. `.github/workflows/verify.yml` runs the same checks on a `setup-go`
runner and is what refuses a push, so **keep the two in step.**

The tests are **hermetic** — a fake fanart.tv over `httptest`, reached by
rewriting the request host through the injected `http.Client`. Keep them that
way: no fanart.tv key CI could hold is not somebody's, and `apiBaseURL` stays a
constant so no seam exists for tests alone.

## Records, release, licence

- **This repository owns no decision records and has no `docs/adr/`.** Take a new
  one to the repository whose gate, composition root or release workflow holds
  its mechanism.
- `scripts/adr_lint.py` is **vendored** from
  [`architecture`](https://github.com/mosaic-media/architecture) and run by the
  gate. Do not edit it here — change it there and re-vendor.
- **Something the contract cannot express is a finding, not an obstacle to work
  around**, and never a surface simulated locally. The open ones live in
  `README.md`'s "Honest limits" — the published list a reader without this file
  sees. Add to it in the same change that finds the gap.
- Pushing a tag runs `release.yml`, which reuses `verify.yml`, proves a consumer
  can resolve the version through the public proxy, cross-compiles the binaries
  and their `manifest.json`, and only then dispatches `module-released`. **That
  dispatch waits on `binaries`, not just `release`** — the catalogue fetches
  `manifest.json` from the release assets. A missing `REGISTRY_DISPATCH_TOKEN`
  fails the run rather than warning.
- **A `replace` must never land in a commit.** The version reported is the one
  actually linked, via `v1.ModuleVersion(modulePath)` — not a constant.
- **MIT.** Files here carry no SPDX header — match the files already present.
