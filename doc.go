// Package fanarttv is Mosaic's fanart.tv artwork module.
//
// It fills one content role — [v1.RoleArtwork] (sdk#6) — plus its own settings
// screen, and the roles it does not fill are the shape of it.
//
// # It illustrates content; it does not identify it
//
// fanart.tv has no titles, no overviews, no years, no cast, no search and no
// catalogs. There is no query that turns "Blade Runner" into a result: you must
// already know which film you mean, and hand it an identifier. So this module
// can answer exactly one question — what does this look like — and none of the
// others.
//
// That is why it declares no [v1.RoleMetadata], and the temptation to is the
// specific mistake sdk#6 exists to prevent. platform#23 makes a registered
// metadata role part of the composition-root check that a deployment can
// identify content; a module that cannot name a film satisfying that check would
// produce a Mosaic that boots and finds nothing. boundary_test.go asserts the
// manifest never grows the role.
//
// # It is reached by enrichment, never by a ref
//
// Every other source module is invoked because a search or catalog result named
// it in a [v1.ContentRef]. This one is never named, because it produces no
// results. The Platform reaches it through the artwork enrichment pass it runs
// after materialising a work (sdk#6), addressing it by the work's shared
// external identity — the same mechanism platform#46 built for stream providers.
//
// [Capability.Import] therefore exists only because [v1.Capability] requires it,
// and always refuses.
//
// # What it returns is a candidate set, not an answer
//
// The module ranks nothing and chooses nothing. It returns every image
// fanart.tv has as [v1.ArtworkCandidate]s carrying language and the source's own
// like count, and the Platform's selection rule (platform#47) resolves which one
// fills a slot — because that choice is ultimately a user's, and a module cannot
// hold it.
//
// Two mappings in here are easy to get silently wrong:
//
//   - lang: "00" means textless, not a language. It maps to an empty Language,
//     which is what lets the Platform prefer a backdrop with no burned-in title
//     to sit under a hero's clearlogo. Carried through as a language, every
//     textless image would look foreign and the preference would never fire.
//   - An HD variant outranks its standard-definition twin. Likes accumulate per
//     image, so an older SD logo frequently has more of them than the HD one
//     that replaced it. This is the one ranking fanart.tv's own data cannot
//     express.
//
// # Honest limits
//
//   - A series needs a TVDB id. fanart.tv keys television by TVDB id and
//     nothing else. module-tmdb binds one; module-cinemeta binds only imdb, so a
//     series imported through Cinemeta gets no artwork from here. Recorded in
//     sdk#6 rather than papered over — it is not fixable in this module.
//   - Episode stills are not fetched. A metadata provider already returns every
//     episode's still in one call; asking here would be a request per episode
//     for data the Platform has in bulk. Series and season art only.
//   - The artwork type table is a guess that must be verified. The mapping from
//     fanart.tv's response keys to slots is data in one table precisely so that
//     a type which has been renamed or added is a one-line correction. Check it
//     against fanart.tv's current documentation before trusting the coverage.
package fanarttv
