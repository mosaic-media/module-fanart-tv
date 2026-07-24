# Claude Instructions — module-fanart-tv

Mosaic's fanart.tv artwork module. It is an **extension module**
([ADR 0062](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0062-two-module-tiers.md)):
it shares no `UnitOfWork` and sits on no hot path, and Mosaic without it is
still Mosaic — artwork simply stays as good as the metadata source made it.

**That makes it the first genuinely optional module in the build.** Every module
before it is core under the coupling or guarantee clause, including
`module-remote-playback`. The extension tier's *mechanism*
([ADR 0064](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0064-extension-module-boundary.md))
is not built, so this composes statically like the others for now — the tier is a
delivery classification waiting for a process boundary, not a different way of
writing the code.

## The one thing not to get wrong

**Never declare `RoleMetadata`.** It is the shortest path to putting artwork
somewhere, and it is the specific mistake
[ADR 0075](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0075-the-artwork-provider-role.md)
exists to prevent.

[ADR 0035](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0035-metadata-as-required-capability.md)
makes a registered `RoleMetadata` *and* `RoleSearch` a composition-root
requirement — the serving composition refuses to start without them, because a
Mosaic that cannot identify or find content reads as broken. This module cannot
name a film. Declaring the role to reach `ContentMetadata`'s image fields would
satisfy half that check with a module structurally incapable of meeting it, and
the failure would not be a compile error or a red test: it would be a deployment
that boots and finds nothing.

`boundary_test.go`'s `TestManifestDeclaresNoMetadataRole` is what makes it a red
test. Keep it, and keep `search`, `catalog` and `stream` in its forbidden list
for the same reason.

## Everything fanart.tv-shaped stops in `fanart.go`

This module is an anti-corruption layer
([ADR 0051](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0051-modules-as-anti-corruption-layers.md)),
and there is more to corrupt than the API's simplicity suggests:

- **Two endpoints keyed by different identifier spaces** — films by TMDB *or*
  IMDb id, television by TVDB id and nothing else. `endpointFor` owns this and
  the Platform must never learn it. It is also why `ArtworkRequest` hands over
  the whole identity set at once rather than one at a time: which id is usable
  depends on what is being asked about.
- **The response's keys *are* the artwork types**, so it decodes into a map
  rather than a struct. A struct would silently drop every type this build
  predates.
- **Numbers arrive as strings** — `likes` and `season` both quoted. They are
  typed as strings and converted on the way out, because decoding them as numbers
  fails the entire response on one odd entry, and losing a title's whole artwork
  over one bad `likes` value is not a trade worth making.
- **`lang: "00"` is textless, not a language.** See below.

`artworkTypeSlot` is deliberately **one table of data, not a switch**, so a type
fanart.tv renames or adds is a one-line change. An absent key is ignored rather
than guessed at: the slot vocabulary is open
([ADR 0015](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0015-open-and-closed-vocabularies.md)),
but inventing a mapping is worse than carrying nothing — a disc image rendered as
a poster is a visible defect nobody reports.

**The table has not been verified against fanart.tv's live documentation.** Do
that before claiming coverage. Because unknown keys are ignored, the failure mode
is under-delivery rather than wrong delivery, which is the right way round but is
still a gap.

## The two mappings that carry the quality

Both are tested, and both fail silently if broken — a worse image is still an
image, so nothing goes red and nobody is told.

- **`languageOf` maps `"00"` to an empty string.** That empty string is what the
  Platform's selection rule reads as *textless*, and textless is the correct
  backdrop to sit under a hero's clearlogo
  ([ADR 0074](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0074-artwork-is-a-candidate-set.md)).
  Carried through as a language, every textless image would look foreign-language
  and the preference would never fire. This is the single most visible thing the
  module does.
- **`rankOf` lifts HD variants above their SD twins.** Likes accumulate per
  image, so an older SD logo frequently outscores the HD one that replaced it.
  It is the one ordering fanart.tv's own data cannot express, and `hdRankBonus`
  is a tie-break rather than an override on purpose.

## This module ranks nothing and chooses nothing

It returns candidates. **The Platform's selection rule decides which one fills a
slot**, because that choice is ultimately a user's and a module cannot hold it.
Do not add a "best image" heuristic here — if the selection is wrong, the rule in
the Platform's `enrich_artwork.go` is where it is wrong.

The one exception is the HD bonus above, and it is an exception because it
corrects the source's *own* ranking rather than expressing a preference about the
result.

## The bundled key

Same pattern as `module-tmdb`, same four rules:

- **`resolveKeys` is the only function that reads `defaultAPIKey`.** Keep it that
  way — it is what makes "never written into settings, never rendered, never
  logged" verifiable by reading rather than a claim to trust.
- **`Settings.APIKey` only ever holds the user's own key.** Never populate it
  from the bundled one as a convenience: `configureModule` replaces the whole
  document, so it would write a shared build-time credential into a user's stored
  settings the next time they touched any control.
- **The settings screen describes it, never shows it.** There is nothing for a
  user to copy, verify or fix.
- **A release that links it needs a `linkercheck` gate.** `-X` on an unresolvable
  path is silently ignored, so a rename ships a keyless binary with no error
  anywhere. `module-tmdb` has the canary; **this module does not have one yet**
  and should get one when the key is first linked in a release build.

## Modules are the forcing function for the SDK

When something cannot be expressed, that is a **finding**, not an obstacle to
work around. This module has already produced three, all written up in the
README's honest limits or in the code:

- **`v1.Capability` bundles identity with `Import`.** An enrichment-only module
  has to stub the one write verb it can never perform. Fine for one module; worth
  taking to the SDK if a second appears.
- **`configureModule` has no merge semantic** ([ADR 0021](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0021-module-settings.md)),
  so every control on the settings screen must echo the credential back through
  the client. `module-tmdb` found this first; **two modules hitting it
  independently is what makes it an SDK item rather than a quirk.**
- **Cinemeta-sourced series are unenrichable here**, because fanart.tv needs a
  TVDB id and Cinemeta binds only `imdb`. Not fixable in this module.

Do not simulate a missing surface locally.

## Everything runs in the container, nothing runs on the host

```bash
docker compose -f docker-compose.test.yml run --rm test
```

That runs gofmt, `go build ./...`, `go vet ./...` and `go test ./...` against the
Go version pinned in the compose file, which must stay equal to `go.mod`'s.

The tests are **hermetic** — a fake fanart.tv over `httptest` reached by
rewriting the request host through the injected `http.Client`. Keep them that
way. There is no fanart.tv key CI could hold that is not somebody's, and
`apiBaseURL` is a constant on purpose: a settable field so tests could point
elsewhere would put a seam in the production type that only tests use.

**Until SDK `v0.21.0` is tagged this module builds only in a Go workspace** over
the sibling `sdk` and `sdui` checkouts, and `go.sum` cannot be completed.

## Versioning and release

The Platform requires this at a **tagged version with no `replace`** — a
`replace` must never land in a commit. A change is a minor bump, tagged and
pushed, then the Platform's `go.mod` require is bumped to match.

Pushing the tag is the whole publish; there is no artifact. Warm the Go proxy
after tagging and before bumping the Platform, since the proxy and checksum
database are eventually consistent with a just-pushed tag.

## Workflow

- Commit and push this repository **separately** from `platform`.
- **Commit author identity** must be `AdamNi-7080 <anicholls41@gmail.com>`.
- The test container green before pushing.
- Observability goes through the SDK's ambient `v1.Telemetry`
  ([ADR 0059](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0059-modules-observe-through-the-sdk.md)),
  reached as `TelemetryFrom(ctx)`. Do not print, and do not configure an
  exporter or a sink. The API key is a credential: classify it, never write it
  verbatim.
- **MIT-licensed**, unlike the Platform's AGPL. Files here carry no SPDX header —
  match the files already present.

## The roadmap and the decision records

These rules are identical in every Mosaic repository.

### The roadmap is maintained, not consulted

**`docs/roadmap.md` in [`architecture`](https://github.com/mosaic-media/architecture)
is the single record of where the build is.** Read it before starting work, and
**update it in the same session as the change that dates it.**

- A slice that lands is marked landed, with what was left out.
- Implementation that departs from the plan is recorded where it departed.
- Do not restate the roadmap here.
- A capability with no client path is not done — it is
  [owed](https://github.com/mosaic-media/architecture/blob/main/docs/unreachable-capability.md).

### Decision records are append-only

- **Never rewrite a record's body to match what was built.**
- **State changes in the `**Status:**` line, and nowhere else.**
- **A changed decision needs a new record that supersedes it.** Both then stand.
- **An unbuilt decision is not a superseded one.**
- Records live only in `architecture/docs/adr/`, added to `nav:` in `mkdocs.yml`,
  and `mkdocs build --strict` must pass.

**If the code and a record disagree, say so rather than quietly picking one.**
