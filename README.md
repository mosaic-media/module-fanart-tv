# module-fanart-tv

Mosaic's [fanart.tv](https://fanart.tv) artwork module. It supplies posters,
backdrops, clearlogos, clearart, banners, disc art and per-season art for titles
**another source has already identified**.

MIT-licensed, its own Go module, importing only the published
[`sdk`](https://github.com/mosaic-media/sdk) and
[`sdui`](https://github.com/mosaic-media/sdui) contracts.

## What it is for

Artwork used to arrive as a by-product of asking a question about *titles*:
whichever module described the content supplied whatever images it happened to
carry. Cinemeta has a poster, a background and sometimes a logo. TMDB has more
and used to discard most of it. Neither has clearart or banners at all — a gap
[ADR 0034](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0034-rich-metadata-preview.md)
recorded as waiting on exactly this kind of source.

This module closes it, and does one thing beyond filling gaps: it returns
**every** image fanart.tv has, as a candidate set
([ADR 0074](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0074-artwork-is-a-candidate-set.md)),
so the Platform can choose well now and a user can choose differently later.

## What it is not

**It is not a metadata source and cannot become one.** fanart.tv has no titles,
no overviews, no years, no cast, no search and no catalogs. There is no query
that turns "Blade Runner" into a result — you must already know which film you
mean.

So it declares no `RoleMetadata`, and that restraint is enforced rather than
intended: `boundary_test.go` fails if the manifest ever grows the role.
[ADR 0035](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0035-metadata-as-required-capability.md)
makes a registered metadata role part of the composition-root check that a
deployment can identify content, and a module that cannot name a film satisfying
that check would produce a Mosaic that boots and finds nothing — a failure no
build and no test would report.

## How it is reached

Every other source module is invoked because a search or catalog result named it
in a `ContentRef`. This one is never named, because it produces no results.

Instead the Platform runs an **artwork enrichment pass** after materialising a
work
([ADR 0075](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0075-the-artwork-provider-role.md)),
handing every registered artwork provider the work's *shared* external identities
— the same mechanism
[ADR 0073](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0073-stream-resolution-is-decoupled-from-metadata-provenance.md)
built for stream providers. `Import` exists only because `v1.Capability` requires
it, and always refuses.

Candidates from several providers **union** rather than compete, so installing
this alongside TMDB gives you both sets with provenance on each.

## Two mappings that carry the quality

Most of what makes artwork look better than the metadata source's own is two
translations that are easy to get silently wrong:

- **`lang: "00"` means textless, not a language.** It becomes an empty
  `Language`, which is what lets the Platform prefer a backdrop with no burned-in
  title to sit under a hero's clearlogo. Carried through as a language code,
  every textless image would look foreign and the preference would never fire.
  This is the single most visible improvement the module makes.
- **An HD variant outranks its standard-definition twin.** Likes accumulate per
  image, so an older SD logo frequently has more of them than the HD one that
  replaced it. Ranking on likes alone would systematically pick the lower
  resolution. It is the one ordering fanart.tv's own data cannot express.

## Credentials

fanart.tv requires a project key. Mosaic's release build links one in with
`-ldflags -X`, so a deployment has working artwork before anyone configures
anything, and a user can override it in **Settings › fanart.tv**. An optional
*personal* key grants early access to artwork uploaded but not yet promoted.

The bundled key is **not a secret once the binary ships** and nothing here
pretends otherwise — a linked string is recoverable with `strings`. It is
read-only, reaches only a public artwork index, and is revocable centrally. What
it costs is a shared rate limit, which is why a user can replace it.
`resolveKeys` is the only function that reads it; it is never written into the
settings document, never rendered, and never logged.

## Honest limits

- **A series needs a TVDB id.** fanart.tv keys television by TVDB id and nothing
  else. `module-tmdb` binds one; `module-cinemeta` binds only `imdb`, so **a
  series imported through Cinemeta gets no artwork from here.** Not fixable in
  this module, and recorded in ADR 0075 rather than papered over.
- **Episode stills are not fetched.** A metadata provider returns every episode's
  still in one call; asking here would be a request per episode for data the
  Platform already has. Series and season art only.
- **The artwork type table should be verified against fanart.tv's current
  documentation.** The response-key → slot mapping is deliberately one table of
  data so a renamed or added type is a one-line correction. An unrecognised key
  is ignored rather than guessed at, so unverified coverage under-delivers rather
  than mis-delivers — but it is under-delivery until checked.
- **The settings screen carries the key in its action payloads.** ADR 0021's
  `configureModule` replaces the whole settings document with no merge, so every
  control must echo the credential back. This is the same finding `module-tmdb`
  recorded, reached independently — two modules hitting it is what makes it an
  SDK item rather than a quirk.

## Building and testing

Everything runs in the container:

```bash
docker compose -f docker-compose.test.yml run --rm test
```

The tests are **hermetic** — a fake fanart.tv over `httptest`, reached by
rewriting the request host through the injected `http.Client` — so they need no
network and no API key. There is no fanart.tv key CI could hold that is not
somebody's, and the API base URL is a constant on purpose: a settable field so
tests could point elsewhere would put a seam in the production type that only
tests use.

**This module requires SDK `v0.21.0`**, which adds `RoleArtwork`,
`ArtworkProvider` and the candidate types. Until that tag is pushed it builds
only inside a Go workspace over the sibling `sdk` and `sdui` checkouts, and
`go.sum` cannot be completed.
