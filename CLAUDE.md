# Claude Instructions — module-fanart-tv

Mosaic's fanart.tv artwork module. It declares `RoleArtwork` and
`RoleSettingsUI`, and nothing else.

It is an **extension module**
([architecture#3](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0003-two-module-tiers.md)):
it shares no `UnitOfWork`, sits on no hot path, and Mosaic without it is still
Mosaic — artwork simply stays as good as the metadata source made it. It is a
separate process behind a gRPC harness, cross-compiled here, catalogued in the
signed registry and **installed by a user at runtime** rather than compiled into
the Platform
([platform#39](https://github.com/mosaic-media/platform/blob/main/docs/adr/0039-extension-module-boundary.md),
[platform#40](https://github.com/mosaic-media/platform/blob/main/docs/adr/0040-module-distribution-and-trust.md),
[platform#51](https://github.com/mosaic-media/platform/blob/main/docs/adr/0051-extension-installation-is-user-initiated-and-persistent.md)).

**That is the fact most of this module's mistakes come back to**, including the
one under "The bundled key": anything reasoning from "this compiles into the
Platform binary" is reasoning about a build this module left.

## The one thing not to get wrong

**Never declare `RoleMetadata`.** It is the shortest path to putting artwork
somewhere, and it is the specific mistake
[sdk#6](https://github.com/mosaic-media/sdk/blob/main/docs/adr/0006-the-artwork-provider-role.md)
exists to prevent.

[platform#23](https://github.com/mosaic-media/platform/blob/main/docs/adr/0023-metadata-as-required-capability.md)
makes a registered `RoleMetadata` *and* `RoleSearch` a composition-root
requirement, because a Mosaic that cannot identify or find content reads as
broken. **This module cannot name a film.** Declaring the role to reach
`ContentMetadata`'s image fields would satisfy half that check with a module
structurally incapable of meeting it, and the failure would be neither a compile
error nor a red test: it would be a deployment that boots and finds nothing.

`boundary_test.go`'s `TestManifestDeclaresNoMetadataRole` is what makes it a red
test. Keep it, and keep `search`, `catalog` and `stream` in its forbidden list
for the same reason.

## The boundary is the point

- **Import only [`sdk`](https://github.com/mosaic-media/sdk),
  [`contracts`](https://github.com/mosaic-media/contracts) and the standard
  library.** `boundary_test.go` parses every import in the package and fails on
  anything else. The `contracts` exemption is there because this module authors
  its own settings screen
  ([sdk#4](https://github.com/mosaic-media/sdk/blob/main/docs/adr/0004-module-contributed-settings-ui.md),
  [contracts#3](https://github.com/mosaic-media/contracts/blob/main/docs/adr/0003-sdui-contract-repository.md)).
- **The test scans the module package, not `cmd/`.** The harness entrypoint
  additionally imports `sdk/host` to serve itself out of process, which is what
  an extension module is and is not a violation — but it also means the parse
  does not cover it. A dependency added under `cmd/` is one nothing here catches.
- **Being optional makes the boundary an ecosystem claim, not a build-safety
  one.** This is the shape a third party's module takes, written against nothing
  but the published surface. That is a reason to hold it harder, not a licence to
  relax it.

## Everything fanart.tv-shaped stops in `fanart.go`

This module is an anti-corruption layer
([module-stremio-addons#2](https://github.com/mosaic-media/module-stremio-addons/blob/main/docs/adr/0002-modules-as-anti-corruption-layers.md)),
and there is more to corrupt than the API's simplicity suggests:

- **Two endpoints keyed by different identifier spaces** — films by TMDB *or*
  IMDb id, television by TVDB id and nothing else. `endpointFor` owns this and
  the Platform must never learn it. It is also why `ArtworkRequest` hands over
  the whole identity set at once rather than one id at a time: which id is usable
  depends on what is being asked about.
- **The response's keys *are* the artwork types**, so it decodes into a map
  rather than a struct. A struct would silently drop every type this build
  predates.
- **Numbers arrive as strings** — `likes` and `season` both quoted. They are
  typed as strings and converted on the way out, because decoding them as numbers
  fails the entire response on one odd entry, and losing a title's whole artwork
  over one bad `likes` value is not a trade worth making.

`artworkTypeSlot` is deliberately **one table of data, not a switch**, so a type
fanart.tv renames or adds is a one-line change. An absent key is ignored rather
than guessed at: inventing a mapping is worse than carrying nothing — a disc
image rendered as a poster is a visible defect nobody reports.

**The table has not been verified against fanart.tv's live documentation.** Do
that before claiming coverage. Because unknown keys are ignored, the failure mode
is under-delivery rather than wrong delivery, which is the right way round but is
still a gap.

## The two mappings that carry the quality

Both are tested, and both fail silently if broken — a worse image is still an
image, so nothing goes red and nobody is told.

- **`languageOf` maps `"00"` to an empty string.** `lang: "00"` is *textless*,
  not a language. That empty string is what the Platform's selection rule reads
  as textless, and textless is the correct backdrop to sit under a hero's
  clearlogo
  ([platform#47](https://github.com/mosaic-media/platform/blob/main/docs/adr/0047-artwork-is-a-candidate-set.md)).
  Carried through as a language, every textless image would look
  foreign-language and the preference would never fire. This is the single most
  visible thing the module does.
- **`rankOf` lifts HD variants above their SD twins.** Likes accumulate per
  image, so an older SD logo frequently outscores the HD one that replaced it.
  It is the one ordering fanart.tv's own data cannot express, and `hdRankBonus`
  is a tie-break rather than an override on purpose.

## This module ranks nothing and chooses nothing

It returns candidates. **The Platform's selection rule decides which one fills a
slot**, because that choice is ultimately a user's and a module cannot hold it
([platform#47](https://github.com/mosaic-media/platform/blob/main/docs/adr/0047-artwork-is-a-candidate-set.md)).
Do not add a "best image" heuristic here — if the selection is wrong, it is wrong
in the Platform.

The HD bonus above is the one exception, and it is an exception because it
corrects the source's *own* ranking rather than expressing a preference about the
result.

## The bundled key

`defaultAPIKey` is linked in at build time with `-ldflags -X`. The six rules
governing every Mosaic project credential are in
[architecture#4](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0004-project-credentials-in-official-builds.md);
these are the ones easiest to break from inside this repository:

- **`resolveKeys` is the only function that reads the symbol.** `bundledKeyPresent`
  and `usingBundledKey` both ask `resolveKeys` rather than the variable, and the
  settings screen asks them. The linker guard is the deliberate exception and
  ships in no binary. This has been broken once already, in the way that is
  hardest to see: the presence checks read the variable directly while the doc
  comment above `resolveKeys` still claimed a single reader. **Changing the
  screen is when to re-check it.**
- **`Settings.APIKey` only ever holds the user's own key.** Never populate it
  from the bundled one as a convenience: `configureModule` replaces the whole
  document, so it would write a shared build-time credential into a user's stored
  settings the next time they touched any control.
- **The settings screen describes the bundled key, never shows it.** There is
  nothing for a user to copy, verify or fix.
- **This repository's own `release.yml` links it, and it is the only build that
  can.** A core module's key is applied by the workflow that links the binary
  carrying it; this module is not in that binary, so its `-X` lives in the
  `binaries` job's cross-compile and `FANART_PROJECT_KEY` belongs in **this**
  repository's secrets. That is the inverse of a core module, whose key must not
  be held by the module repository at all.

**The linker guard is mandatory, and this module is why.** `-X` against a symbol
that no longer resolves is *silently ignored*. This module had the key, the
three-state screen, the single-reader function and the whole policy written out
in a doc comment — and the comment named a build the tier split had taken it out
of. No workflow injected the key, nothing checked, every released binary shipped
an empty one, and enrichment answered "fanart.tv API key not set" for the whole
life of the module. `linkercheck_test.go` links a canary through the same symbol
path the release build uses, and **both** `docker-compose.test.yml` and
`.github/workflows/verify.yml` now run that pass. Renaming the variable or moving
its package means updating the path in all three.

> **A citation in this repository's own source is wrong and has not been
> corrected here.** `capability.go` and `verify.yml` attribute these rules to
> [supervisor#1](https://github.com/mosaic-media/supervisor/blob/main/docs/adr/0001-supervisor-as-host-manager.md),
> which is the Supervisor's host-manager record and carries no numbered rules at
> all. The record meant is
> [architecture#4](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0004-project-credentials-in-official-builds.md).
> It resolves, so no lint catches it.

## Modules are the forcing function for the SDK

When something cannot be expressed, that is a **finding**, not an obstacle to
work around. **Do not simulate a missing surface locally.**

**The SDK is where the *shape* of an interaction goes; the Platform holds the
implementations**
([sdk#10](https://github.com/mosaic-media/sdk/blob/main/docs/adr/0010-the-sdk-carries-no-implementation.md)
— read its Status before repeating what it decided as though it were built).
Applied to the credential problem, that rules out the tempting answer: a module
reaches the Platform's secret facility by *declaring* a settings field secret and
letting the Platform seal it, never through `Seal`/`Open` primitives in the SDK,
which would publish an implementation and hand a module an encryption oracle.

**The findings this module has produced live in `README.md`'s "Honest limits".**
That is the published list and the one a reader without this file sees; a second
copy here is how the first goes stale. Add to it in the same change that finds
the gap.

## Everything runs in the container, nothing runs on the host

**Do not run `go build`, `go test`, `go vet` or `gofmt` directly on this
machine.**

```bash
docker compose -f docker-compose.test.yml run --rm test
```

That runs gofmt, `go build ./...`, `go vet ./...`, `go test ./...` and the tagged
`linkercheck` pass, against the Go version pinned in the compose file, which must
stay equal to `go.mod`'s. Append `bash` for a shell in the same environment.

**`.github/workflows/verify.yml` mirrors that compose file step for step**, and
the two must stay in step: the compose file is what you run, the workflow is what
refuses the push.

What the container is protecting is **the boundary**. A host with a populated
module cache, a leftover `go.work` or a stray `replace` can satisfy an import a
third party's machine could not, and `boundary_test.go` still passes because the
import resolved.

The tests are **hermetic** — a fake fanart.tv over `httptest`, reached by
rewriting the request host through the injected `http.Client`. Keep them that
way. There is no fanart.tv key CI could hold that is not somebody's, and
`apiBaseURL` is a constant on purpose: a settable field so tests could point
elsewhere would put a seam in the production type that only tests use.

## Versioning and release

A change is a **minor bump**, tagged and pushed. **A `replace` must never land in
a commit.**

**Nothing bumps a `require` afterwards.** The Platform does not depend on this
module
([platform#51](https://github.com/mosaic-media/platform/blob/main/docs/adr/0051-extension-installation-is-user-initiated-and-persistent.md)),
so there is no version line to move. A release reaches people through the
**catalogue**: `release.yml`'s `binaries` job cross-compiles and signs, and its
`dispatch` job tells the registry there is a new version to list. That dispatch
waits on `binaries` on purpose — a catalogue entry pointing at a release whose
binaries do not exist yet is worse than a late one.

Warm the Go proxy after tagging anyway: this module is still resolved as a Go
module by anything building it from source, and the proxy and checksum database
are eventually consistent with a just-pushed tag.

## Workflow

- Observability goes through the SDK's ambient `v1.Telemetry`
  ([sdk#5](https://github.com/mosaic-media/sdk/blob/main/docs/adr/0005-modules-observe-through-the-sdk.md)),
  reached as `TelemetryFrom(ctx)`. Do not print, and do not configure an
  exporter, a sink or retention — the Platform owns the observability plane. The
  API key is a credential: classify it, never write it verbatim.
- **MIT-licensed**
  ([architecture#1](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0001-licensing.md)).
  Files here carry no SPDX header — match the files already present.
- **This repository owns no decision records**, and that is correct rather than
  an oversight: every decision governing it is enforced somewhere else. Do not
  create a `docs/adr/` here to hold one; take it to the repository whose gate,
  composition root or release workflow would enforce it.

<!-- shared-rules:begin -->
## Rules every Mosaic repository shares

*Generated. The source is `architecture/shared/repository-rules.md`; edit it there
and run `scripts/shared_rules.py --write` across the fleet. A copy edited in place
fails its repository's gate, which is the point: these rules were eleven
hand-kept copies in four variants, and the abridged ones had quietly dropped the
reasoning while keeping the rules — and in one case dropped a rule outright.*

### What this file may say

**A `CLAUDE.md` states rules, and facts about its own repository. It does not
state facts about another one — it links instead.**

An audit of all twelve of these files against their source found 74 stale claims.
None of roughly 180 rules was wrong; 62 of the 74 were facts about somebody
else's repository. Ownership predicts rot: a fact about this repository stays true
because whoever changes the code changes the sentence in the same session, and a
fact about another one dies the moment they edit it with nothing here going red.

The same applies to facts this repository already publishes in a generated
artefact — counts, versions, what is built. Point at the artefact.

### Decision records live with the code they govern

Each repository owns the records whose *mechanism* it holds — the spec file, the
lint gate, the conformance corpus, the composition root, the release workflow.
A decision can bind five repositories and still have exactly one steward.

- **`docs/adr/`**, numbered from 1 in every repository, with `docs/adr/README.md`
  a **generated** index. Read the index first; it is the bounded thing.
- **A record's heading carries no number.** The number lives in the filename and
  the index only, so a record's anchor survives being renumbered.
- **Cite a record as `repo#N`, and make it a link** — a relative path within a
  repository, an absolute URL across them, and the bare label only where no URL
  is possible, such as a code comment or a Dockerfile. The old `ADR NNNN`
  spelling is refused by a lint: once every repository numbers from 1, that form
  resolves quietly to a *different* record instead of dangling, and no tool in
  the fleet could detect it.
- **Cross-cutting records stay in [`architecture`](https://github.com/mosaic-media/architecture)** —
  the ones with no enforcing mechanism anywhere: licensing, repository naming and
  topology, the module tier model.

### Decision records are append-only

An ADR is an account of what was decided and why, at a time. It is evidence, not
documentation, and its value is that it was not edited afterwards.

- **Never rewrite a record's body** — not to correct it, not to annotate it, not
  to add "as built, this differs". That turns a record into a running commentary
  and destroys the thing it is for.
- **State changes go in the `**Status:**` line and nowhere else** — built, built
  in part (naming the part), or superseded, wholly or partly.
- **A changed decision earns a new record that supersedes it**, with its own
  Context / Decision / Alternatives / Consequences, and both records then point
  at each other through their Status lines. The old body stays exactly as it was.
- **An unbuilt decision is not a superseded one.** "Not done yet" belongs in the
  Status line and the roadmap; only a reversal earns a new record.

### The roadmap is maintained, not consulted

**`docs/roadmap.md` in [`architecture`](https://github.com/mosaic-media/architecture)
is the single record of where the build is, across every repository.** It stays
there because a milestone spans repositories by construction. Read it before
starting, and **update it in the same session as the change that dates it** — not
in a follow-up, which does not happen.

- A slice that lands is marked landed, **with what it left out named in the same
  sentence**. "Built" with no qualifier claims the whole slice shipped.
- Implementation that departed from its record is recorded where it departed.
  The surprises are the most valuable thing in it.
- **Do not restate the roadmap here.** A second copy of "what is built" in a
  `CLAUDE.md` is how the first copy goes stale unnoticed.
- A capability with no client path is not done — it is
  [owed](https://github.com/mosaic-media/architecture/blob/main/docs/unreachable-capability.md).

### Demonstrated, not asserted

**Say what you actually ran.** A skipped test is not a passed test, and "it should
work" is not evidence.

Each repository's container is the authority on its own gate, and the command is
in that repository's section below. It exists because the checks that matter fail
*soft*: a missing PostgreSQL skips storage tests and still prints `ok`, a missing
generator toolchain produces a drift guard that passes by not running. Where the
container cannot be run, running what you can on the host is better than running
nothing — **provided you report which checks ran and which did not.** Claiming a
gate passed when it was not executed is the one thing this rule exists to stop.

### Commit and push

- **Commit and push each repository separately.** They are siblings on disk and
  independent in git.
- **Commit author identity** must be `AdamNi-7080 <anicholls41@gmail.com>`. If git
  has no identity configured, set it repo-locally rather than globally.
- **Push once the change has been demonstrated working in this session.** Commit
  locally and say so otherwise. **Force-push always requires asking.**
<!-- shared-rules:end -->
