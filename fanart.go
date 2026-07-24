package fanarttv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	v1 "github.com/mosaic-media/sdk/contracts/platform/v1"
)

// The anti-corruption layer (ADR 0051). **Every fanart.tv-ism stops in this
// file**, and there are more of them than the API's simplicity suggests:
//
//   - Two endpoints keyed by *different* identifier spaces — films by TMDB or
//     IMDb id, television by TVDB id and nothing else.
//   - A response that is a flat object whose *keys are the artwork types*, so
//     the type vocabulary is the schema rather than a field within it.
//   - Numbers as strings: `likes` and `season` both arrive quoted.
//   - `lang: "00"` meaning *textless*, which is not a language and is frequently
//     the best answer.
//
// Nothing above this file sees any of it. The Platform must never learn that a
// series is addressed by TVDB id, and the SDK's ArtworkCandidate is what leaves
// here.

// apiBaseURL is fanart.tv's web service. It is a constant on purpose: adding a
// settable field so tests could point elsewhere would put a seam in the
// production type that only tests use. The hermetic tests rewrite the request
// host through the injected http.Client instead.
const apiBaseURL = "https://webservice.fanart.tv/v3"

// requestTimeout bounds one call. Artwork enrichment runs inside an import, and
// an import that hangs because an artwork source is slow has turned an
// optional improvement into a broken feature.
const requestTimeout = 15 * time.Second

// textlessLang is what fanart.tv puts in `lang` for an image with no burned-in
// text. It is not a language code and must not be carried as one — it maps to an
// empty ArtworkCandidate.Language, which is what marks a backdrop as safe to sit
// under a clearlogo (ADR 0074).
const textlessLang = "00"

// errNoAPIKey is returned when the module has no credential at all — no user key
// and no bundled one. A sentinel rather than a formatted error because every
// surface that reports it should say the same thing.
var errNoAPIKey = errors.New("fanart.tv API key not set — add one in Settings › fanart.tv (the module cannot read anything without it)")

// errCredentialRejected is returned when fanart.tv refuses the credential that
// *was* sent. Deliberately distinct from errNoAPIKey: reporting a revoked key as
// "not set" sends a user to look at a field that is not empty, and with a
// bundled key in play it is actively misleading — the user has set nothing and
// has nothing to fix.
var errCredentialRejected = errors.New("fanart.tv rejected the credential — if you set your own key, check it in Settings › fanart.tv; otherwise the key bundled with Mosaic is no longer valid")

// Client is a fanart.tv web service client.
type Client struct {
	http *http.Client
	// apiKey is the project key every request carries.
	apiKey string
	// clientKey is a user's personal key, sent alongside the project key when
	// they have one. It is what grants early access to artwork uploaded but not
	// yet promoted, so a user who supplies it gets fresher art than the default.
	clientKey string
}

// NewClient builds a client over an HTTP client (nil for a default). The
// Platform passes its own, which carries the netguard dial guard and the
// outbound telemetry seam (ADR 0055).
func NewClient(httpClient *http.Client, apiKey, clientKey string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: requestTimeout}
	}
	return &Client{http: httpClient, apiKey: apiKey, clientKey: clientKey}
}

// artworkTypeSlot maps one fanart.tv response key to the SDK slot it fills.
//
// **This table is the module's whole vocabulary translation**, kept as data
// rather than a switch so that a type fanart.tv adds, renames or retires is a
// one-line change in one place. A key absent from the table is ignored rather
// than guessed at: the slot vocabulary is open (ADR 0015), but inventing a
// mapping is worse than carrying nothing, because a disc image rendered as a
// poster is a visible defect nobody reported.
//
// The `hd*` variants are listed alongside their standard-definition twins and
// map to the same slot. They are genuinely the same kind of image at a different
// resolution, and rankPreference below is what prefers the better one.
var artworkTypeSlot = map[string]v1.ArtworkSlot{
	// Films.
	"movieposter":     v1.ArtworkPoster,
	"moviebackground": v1.ArtworkBackdrop,
	"moviethumb":      v1.ArtworkLandscape,
	"hdmovielogo":     v1.ArtworkLogo,
	"movielogo":       v1.ArtworkLogo,
	"hdmovieclearart": v1.ArtworkClearArt,
	"movieart":        v1.ArtworkClearArt,
	"moviebanner":     v1.ArtworkBanner,
	"moviedisc":       v1.ArtworkDisc,
	// Television.
	"tvposter":       v1.ArtworkPoster,
	"showbackground": v1.ArtworkBackdrop,
	"tvthumb":        v1.ArtworkLandscape,
	"hdtvlogo":       v1.ArtworkLogo,
	"clearlogo":      v1.ArtworkLogo,
	"hdclearart":     v1.ArtworkClearArt,
	"clearart":       v1.ArtworkClearArt,
	"tvbanner":       v1.ArtworkBanner,
	"characterart":   v1.ArtworkCharacterArt,
	// Television, per season. These carry a `season` field and are filtered to
	// the requested season rather than landing on the series.
	"seasonposter": v1.ArtworkPoster,
	"seasonthumb":  v1.ArtworkLandscape,
	"seasonbanner": v1.ArtworkBanner,
}

// seasonScopedTypes are the response keys whose entries describe one season
// rather than the whole series. They are the reason a season container gets its
// own artwork instead of repeating the series poster four times.
var seasonScopedTypes = map[string]bool{
	"seasonposter": true,
	"seasonthumb":  true,
	"seasonbanner": true,
}

// hdTypes are the response keys naming a high-definition variant. An image from
// one of these outranks the standard-definition twin that maps to the same slot,
// which is the only ranking fanart.tv's own data does not express: `likes` are
// counted per image, and an older SD logo frequently has more of them than the
// HD one that replaced it.
var hdTypes = map[string]bool{
	"hdmovielogo":     true,
	"hdmovieclearart": true,
	"hdtvlogo":        true,
	"hdclearart":      true,
}

// hdRankBonus lifts an HD variant above its SD twin without erasing the like
// counts underneath. It is large enough to win a normal comparison and small
// enough that an SD image with an extraordinary margin still surfaces — the
// intent is a tie-break, not an override.
const hdRankBonus = 1000

// image is one artwork entry as fanart.tv returns it.
//
// Every numeric field arrives as a string, which is why they are typed as
// strings here and converted on the way out. Decoding them as numbers fails the
// whole response on one badly-typed entry, and losing a title's entire artwork
// because one image had an odd `likes` value is not a trade worth making.
type image struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Lang   string `json:"lang"`
	Likes  string `json:"likes"`
	Season string `json:"season"`
}

// Artwork fetches the images fanart.tv has for one title, scoped to the series
// itself (season 0) or to one of its seasons.
//
// The response is decoded into a map keyed by artwork type rather than a struct,
// because the type vocabulary *is* the schema: fanart.tv adds types over time,
// and a struct would silently drop the ones this build predates. The identity
// fields (`name`, `tmdb_id`, …) decode as something that is not an array and are
// skipped by the decoder.
func (c *Client) Artwork(ctx context.Context, mediaType v1.MediaType, identities []v1.ExternalIdentity, season int) ([]v1.ArtworkCandidate, error) {
	if c.apiKey == "" {
		return nil, errNoAPIKey
	}

	path, ok := endpointFor(mediaType, identities)
	if !ok {
		// No identity this source speaks. Not an error — being asked about
		// content it cannot address is the normal case for a provider that did
		// not source the content (ADR 0075).
		return nil, nil
	}

	raw, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}

	// One response carries the whole series — every season's posters included —
	// so which slice of it is wanted is decided here rather than by a second
	// request.
	if season > 0 {
		return seasonCandidates(raw, season), nil
	}
	return seriesCandidates(raw), nil
}

// endpointFor picks the endpoint and identifier for a title.
//
// **This is the module's business and the Platform must never learn it.**
// fanart.tv keys television by TVDB id and *only* by TVDB id, while a film can
// be addressed by either TMDB or IMDb id. That asymmetry is why the whole
// identity set is handed over at once rather than one at a time: which id is
// usable depends on what is being asked about.
func endpointFor(mediaType v1.MediaType, identities []v1.ExternalIdentity) (string, bool) {
	byScheme := make(map[string]string, len(identities))
	for _, identity := range identities {
		if identity.Scheme != "" && identity.ID != "" {
			byScheme[identity.Scheme] = identity.ID
		}
	}

	if isSeries(mediaType) {
		if id := byScheme["tvdb"]; id != "" {
			return "/tv/" + url.PathEscape(id), true
		}
		// A series with no TVDB id is unreachable here. Cinemeta binds only
		// `imdb`, so a series imported through it gets no artwork from this
		// source — a real limit, recorded in ADR 0075 rather than papered over
		// by guessing at a lookup this module cannot perform.
		return "", false
	}

	// TMDB first: it is fanart.tv's own primary key for a film, and an IMDb
	// lookup is the compatibility path rather than the intended one.
	if id := byScheme["tmdb"]; id != "" {
		return "/movies/" + url.PathEscape(id), true
	}
	if id := byScheme["imdb"]; id != "" {
		return "/movies/" + url.PathEscape(id), true
	}
	return "", false
}

// isSeries reports whether a media type is episodic, and therefore addressed by
// fanart.tv's television endpoint.
//
// It matches on a substring because the media vocabulary is open text (ADR
// 0015): "series", "anime_series" and a type this build has never seen all
// describe something with seasons, and an exact-match list would send a new
// episodic type to the film endpoint where every lookup would miss.
func isSeries(mediaType v1.MediaType) bool {
	return strings.Contains(string(mediaType), "series") ||
		strings.Contains(string(mediaType), "show")
}

// get performs one request and decodes it into the raw type-keyed document.
func (c *Client) get(ctx context.Context, path string) (map[string]json.RawMessage, error) {
	endpoint := apiBaseURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build fanart.tv request: %w", err)
	}

	// The credential travels in a header rather than the query string, so it
	// does not land in an access log or a proxy's URL trace.
	req.Header.Set("api-key", c.apiKey)
	if c.clientKey != "" {
		req.Header.Set("client-key", c.clientKey)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call fanart.tv: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		// fanart.tv has nothing for this title. An extremely common answer and
		// not a failure — most titles have no fan-made artwork at all.
		return nil, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, errCredentialRejected
	default:
		return nil, fmt.Errorf("fanart.tv returned %s", resp.Status)
	}

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode fanart.tv response: %w", err)
	}
	return raw, nil
}

// candidatesFrom turns one response into SDK candidates, best-first.
//
// Season-scoped entries are carried with their season number attached so the
// caller can route them; see splitBySeason.
func candidatesFrom(raw map[string]json.RawMessage) []v1.ArtworkCandidate {
	var out []v1.ArtworkCandidate

	// Sorted so a run is reproducible rather than depending on map order — two
	// images with identical likes must not swap places between imports.
	types := make([]string, 0, len(raw))
	for name := range raw {
		types = append(types, name)
	}
	sort.Strings(types)

	for _, name := range types {
		slot, known := artworkTypeSlot[name]
		if !known {
			// Either an identity field (`name`, `tmdb_id`) or an artwork type
			// this build does not know. Both are correctly ignored.
			continue
		}

		var images []image
		if err := json.Unmarshal(raw[name], &images); err != nil {
			// Not an array — an identity field whose key happened to collide, or
			// a shape change. Skipping one type is right; failing the whole
			// response would lose every other type's artwork with it.
			continue
		}

		for _, img := range images {
			if img.URL == "" {
				continue
			}
			out = append(out, v1.ArtworkCandidate{
				Slot:     slot,
				URL:      img.URL,
				Language: languageOf(img.Lang),
				Rank:     rankOf(img.Likes, hdTypes[name]),
				// Season is not on the SDK candidate — it is carried out of band
				// by seasonOf, because a candidate belongs to whichever node the
				// caller attaches it to.
			})
		}
	}
	return out
}

// languageOf normalises fanart.tv's language field.
//
// "00" is fanart.tv's marker for an image with no text on it, not a language.
// Mapping it to an empty string is what lets the Platform's selection rule
// prefer a textless backdrop (ADR 0074) — carrying "00" through as if it were a
// language would make every textless image look like a foreign-language one.
func languageOf(lang string) string {
	lang = strings.TrimSpace(lang)
	if lang == textlessLang {
		return ""
	}
	return lang
}

// rankOf converts fanart.tv's like count into the SDK's rank, lifting an HD
// variant above its standard-definition twin.
//
// A malformed count becomes zero rather than an error: an image whose likes
// cannot be parsed is still a usable image, and it simply sorts last.
func rankOf(likes string, hd bool) float64 {
	rank, err := strconv.ParseFloat(strings.TrimSpace(likes), 64)
	if err != nil {
		rank = 0
	}
	if hd {
		rank += hdRankBonus
	}
	return rank
}

// seasonCandidates splits a response into the series-level candidates and the
// candidates for one requested season.
//
// It exists because fanart.tv returns a series' entire artwork — every season's
// posters included — in one response, while the Platform asks per node. Fetching
// once and splitting is one request per series rather than one per season.
func seasonCandidates(raw map[string]json.RawMessage, season int) []v1.ArtworkCandidate {
	var out []v1.ArtworkCandidate

	types := make([]string, 0, len(raw))
	for name := range raw {
		if seasonScopedTypes[name] {
			types = append(types, name)
		}
	}
	sort.Strings(types)

	for _, name := range types {
		slot := artworkTypeSlot[name]
		var images []image
		if err := json.Unmarshal(raw[name], &images); err != nil {
			continue
		}
		for _, img := range images {
			if img.URL == "" || seasonOf(img.Season) != season {
				continue
			}
			out = append(out, v1.ArtworkCandidate{
				Slot:     slot,
				URL:      img.URL,
				Language: languageOf(img.Lang),
				Rank:     rankOf(img.Likes, hdTypes[name]),
			})
		}
	}
	return out
}

// seriesCandidates returns the candidates that describe the whole series,
// excluding every season-scoped entry.
//
// The exclusion is the point: without it a season 3 poster would be a candidate
// for the series itself, and the series would sometimes show one season's art as
// its own.
func seriesCandidates(raw map[string]json.RawMessage) []v1.ArtworkCandidate {
	filtered := make(map[string]json.RawMessage, len(raw))
	for name, value := range raw {
		if !seasonScopedTypes[name] {
			filtered[name] = value
		}
	}
	return candidatesFrom(filtered)
}

// seasonOf parses fanart.tv's quoted season number.
//
// An entry with no season, or one reading "all", is not season-specific and is
// reported as -1 so it matches no requested season. Treating it as season 0
// would attach it to a source's specials, which is a different thing.
func seasonOf(season string) int {
	season = strings.TrimSpace(season)
	if season == "" {
		return -1
	}
	n, err := strconv.Atoi(season)
	if err != nil {
		return -1
	}
	return n
}
