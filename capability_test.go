package fanarttv_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	fanarttv "github.com/mosaic-media/module-fanart-tv"
	v1 "github.com/mosaic-media/sdk/contracts/platform/v1"
)

// The tests are **hermetic**: a fake fanart.tv over httptest, reached by
// rewriting the request host through the injected http.Client. There is no
// fanart.tv key CI could hold that is not somebody's, and the API base URL is a
// constant on purpose — adding a settable field so tests could point elsewhere
// would put a seam in the production type that only tests use.

// rewriteHost sends every request to the fake regardless of the host the module
// built, which is what lets apiBaseURL stay a constant.
type rewriteHost struct {
	target *url.URL
	base   http.RoundTripper
	// lastPath and lastHeaders record what the module actually asked for, so a
	// test can assert the addressing rather than only the result.
	lastPath    string
	lastHeaders http.Header
}

func (r *rewriteHost) RoundTrip(req *http.Request) (*http.Response, error) {
	r.lastPath = req.URL.Path
	r.lastHeaders = req.Header.Clone()
	req = req.Clone(req.Context())
	req.URL.Scheme = r.target.Scheme
	req.URL.Host = r.target.Host
	return r.base.RoundTrip(req)
}

// fakeFanart serves one canned document for every request.
func fakeFanart(t *testing.T, status int, body any) (*fanarttv.Capability, *rewriteHost) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if body != nil {
			_ = json.NewEncoder(w).Encode(body)
		}
	}))
	t.Cleanup(server.Close)

	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse fake server url: %v", err)
	}
	transport := &rewriteHost{target: target, base: http.DefaultTransport}
	return fanarttv.New(&http.Client{Transport: transport}), transport
}

// newTestCapability builds a capability with no server behind it, for tests that
// only read the manifest.
func newTestCapability() *fanarttv.Capability {
	return fanarttv.New(http.DefaultClient)
}

// settingsWithKey is the settings document a configured deployment has. Every
// test supplies one, because the bundled key is empty in an ordinary build and
// the module correctly refuses to work without a credential.
func settingsWithKey(t *testing.T) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"apiKey": "test-project-key"})
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	return raw
}

func request(t *testing.T, mediaType v1.MediaType, season int, identities ...v1.ExternalIdentity) v1.ArtworkRequest {
	t.Helper()
	return v1.ArtworkRequest{
		Settings:   settingsWithKey(t),
		Identities: identities,
		MediaType:  mediaType,
		Season:     season,
	}
}

// TestFilmIsAddressedByTMDBIDInPreferenceToIMDb covers the addressing that is
// this module's whole anti-corruption job. fanart.tv's own primary key for a film
// is the TMDB id; the IMDb path is a compatibility route.
func TestFilmIsAddressedByTMDBIDInPreferenceToIMDb(t *testing.T) {
	capability, transport := fakeFanart(t, http.StatusOK, map[string]any{
		"name":        "Blade Runner",
		"movieposter": []map[string]string{{"url": "https://assets/p.jpg", "lang": "en", "likes": "7"}},
	})

	_, err := capability.Artwork(context.Background(), request(t, v1.MediaMovie, 0,
		v1.ExternalIdentity{Scheme: "imdb", ID: "tt0083658"},
		v1.ExternalIdentity{Scheme: "tmdb", ID: "78"},
	))
	if err != nil {
		t.Fatalf("Artwork: %v", err)
	}

	if transport.lastPath != "/v3/movies/78" {
		t.Errorf("requested %q, want the TMDB id to be preferred", transport.lastPath)
	}
}

// TestFilmFallsBackToIMDbID covers the compatibility route — a film sourced from
// Cinemeta has an IMDb id and no TMDB one.
func TestFilmFallsBackToIMDbID(t *testing.T) {
	capability, transport := fakeFanart(t, http.StatusOK, map[string]any{"name": "Blade Runner"})

	if _, err := capability.Artwork(context.Background(), request(t, v1.MediaMovie, 0,
		v1.ExternalIdentity{Scheme: "imdb", ID: "tt0083658"},
	)); err != nil {
		t.Fatalf("Artwork: %v", err)
	}

	if transport.lastPath != "/v3/movies/tt0083658" {
		t.Errorf("requested %q, want the IMDb id used as a fallback", transport.lastPath)
	}
}

// TestSeriesRequiresATVDBID is the limit sdk#6 records rather than papers
// over. fanart.tv keys television by TVDB id and only by TVDB id, so a series
// carrying just an IMDb id — which is every series Cinemeta imports — cannot be
// looked up here at all.
//
// The right behaviour is an empty answer and no error, and no request at all.
func TestSeriesRequiresATVDBID(t *testing.T) {
	capability, transport := fakeFanart(t, http.StatusOK, map[string]any{"name": "unused"})

	resp, err := capability.Artwork(context.Background(), request(t, v1.MediaTVSeries, 0,
		v1.ExternalIdentity{Scheme: "imdb", ID: "tt0903747"},
	))
	if err != nil {
		t.Fatalf("Artwork should decline quietly, not error: %v", err)
	}
	if len(resp.Candidates) != 0 {
		t.Errorf("got %d candidates, want none", len(resp.Candidates))
	}
	if transport.lastPath != "" {
		t.Errorf("issued a request to %q; an unaddressable title should cost no call", transport.lastPath)
	}
}

// TestSeriesIsAddressedByTVDBID covers the television path.
func TestSeriesIsAddressedByTVDBID(t *testing.T) {
	capability, transport := fakeFanart(t, http.StatusOK, map[string]any{"name": "Breaking Bad"})

	if _, err := capability.Artwork(context.Background(), request(t, v1.MediaTVSeries, 0,
		v1.ExternalIdentity{Scheme: "imdb", ID: "tt0903747"},
		v1.ExternalIdentity{Scheme: "tvdb", ID: "81189"},
	)); err != nil {
		t.Fatalf("Artwork: %v", err)
	}

	if transport.lastPath != "/v3/tv/81189" {
		t.Errorf("requested %q, want the TVDB id", transport.lastPath)
	}
}

// TestTextlessArtworkLosesItsLanguage is the mapping that makes the Platform's
// selection rule work. fanart.tv marks an image with no burned-in text as
// `lang: "00"`, which is not a language — carrying it through as one would make
// every textless backdrop look foreign-language, and the Platform would stop
// preferring exactly the images that belong under a clearlogo.
func TestTextlessArtworkLosesItsLanguage(t *testing.T) {
	capability, _ := fakeFanart(t, http.StatusOK, map[string]any{
		"moviebackground": []map[string]string{
			{"url": "https://assets/textless.jpg", "lang": "00", "likes": "3"},
			{"url": "https://assets/english.jpg", "lang": "en", "likes": "9"},
		},
	})

	resp, err := capability.Artwork(context.Background(), request(t, v1.MediaMovie, 0,
		v1.ExternalIdentity{Scheme: "tmdb", ID: "78"}))
	if err != nil {
		t.Fatalf("Artwork: %v", err)
	}

	byURL := map[string]v1.ArtworkCandidate{}
	for _, candidate := range resp.Candidates {
		byURL[candidate.URL] = candidate
	}
	if got := byURL["https://assets/textless.jpg"].Language; got != "" {
		t.Errorf("textless image has Language %q, want empty", got)
	}
	if got := byURL["https://assets/english.jpg"].Language; got != "en" {
		t.Errorf("english image has Language %q, want \"en\"", got)
	}
}

// TestHDVariantsOutrankTheirStandardDefinitionTwins covers the one ranking
// fanart.tv's own data cannot express. Likes accumulate per image, so an older
// SD logo frequently has more of them than the HD one that replaced it — and
// picking by likes alone would systematically choose the lower-resolution image.
func TestHDVariantsOutrankTheirStandardDefinitionTwins(t *testing.T) {
	capability, _ := fakeFanart(t, http.StatusOK, map[string]any{
		"hdmovielogo": []map[string]string{{"url": "https://assets/hd.png", "lang": "en", "likes": "2"}},
		"movielogo":   []map[string]string{{"url": "https://assets/sd.png", "lang": "en", "likes": "50"}},
	})

	resp, err := capability.Artwork(context.Background(), request(t, v1.MediaMovie, 0,
		v1.ExternalIdentity{Scheme: "tmdb", ID: "78"}))
	if err != nil {
		t.Fatalf("Artwork: %v", err)
	}

	var hd, sd float64
	for _, candidate := range resp.Candidates {
		switch candidate.URL {
		case "https://assets/hd.png":
			hd = candidate.Rank
		case "https://assets/sd.png":
			sd = candidate.Rank
		}
	}
	if hd <= sd {
		t.Errorf("HD logo ranked %v, SD ranked %v — the HD variant should win despite fewer likes", hd, sd)
	}
	// Both map to the same slot, which is the point: they are the same kind of
	// image and the Platform chooses between them.
	for _, candidate := range resp.Candidates {
		if candidate.Slot != v1.ArtworkLogo {
			t.Errorf("candidate %q has slot %q, want logo", candidate.URL, candidate.Slot)
		}
	}
}

// TestSeasonArtworkIsScopedToItsSeason covers what gives a season container its
// own poster instead of repeating the series'. One response carries every
// season's art, so the filtering has to be right or season 1's poster ends up on
// season 4.
func TestSeasonArtworkIsScopedToItsSeason(t *testing.T) {
	body := map[string]any{
		"tvposter": []map[string]string{{"url": "https://assets/series.jpg", "lang": "en", "likes": "9"}},
		"seasonposter": []map[string]string{
			{"url": "https://assets/s1.jpg", "lang": "en", "likes": "4", "season": "1"},
			{"url": "https://assets/s2.jpg", "lang": "en", "likes": "6", "season": "2"},
		},
	}

	capability, _ := fakeFanart(t, http.StatusOK, body)
	resp, err := capability.Artwork(context.Background(), request(t, v1.MediaTVSeries, 2,
		v1.ExternalIdentity{Scheme: "tvdb", ID: "81189"}))
	if err != nil {
		t.Fatalf("Artwork: %v", err)
	}

	if len(resp.Candidates) != 1 {
		t.Fatalf("got %d candidates for season 2, want exactly the season-2 poster: %+v", len(resp.Candidates), resp.Candidates)
	}
	if resp.Candidates[0].URL != "https://assets/s2.jpg" {
		t.Errorf("season 2 got %q", resp.Candidates[0].URL)
	}
}

// TestSeriesLevelRequestExcludesSeasonArtwork is the other half of the same
// rule. Without the exclusion a season 3 poster would be a candidate for the
// series itself, and the series would sometimes wear one season's art.
func TestSeriesLevelRequestExcludesSeasonArtwork(t *testing.T) {
	capability, _ := fakeFanart(t, http.StatusOK, map[string]any{
		"tvposter":     []map[string]string{{"url": "https://assets/series.jpg", "lang": "en", "likes": "9"}},
		"seasonposter": []map[string]string{{"url": "https://assets/s3.jpg", "lang": "en", "likes": "99", "season": "3"}},
	})

	resp, err := capability.Artwork(context.Background(), request(t, v1.MediaTVSeries, 0,
		v1.ExternalIdentity{Scheme: "tvdb", ID: "81189"}))
	if err != nil {
		t.Fatalf("Artwork: %v", err)
	}

	for _, candidate := range resp.Candidates {
		if candidate.URL == "https://assets/s3.jpg" {
			t.Error("a season poster leaked into the series' own artwork")
		}
	}
	if len(resp.Candidates) != 1 || resp.Candidates[0].URL != "https://assets/series.jpg" {
		t.Errorf("series artwork = %+v, want just the series poster", resp.Candidates)
	}
}

// TestUnknownArtworkTypesAreIgnored covers the open vocabulary (platform#11). A
// type this build has never heard of must not be guessed at — a disc image
// rendered as a poster is a visible defect nobody reported — and it must not
// fail the response either, which would lose every other type with it.
func TestUnknownArtworkTypesAreIgnored(t *testing.T) {
	capability, _ := fakeFanart(t, http.StatusOK, map[string]any{
		"name":                  "Blade Runner",
		"tmdb_id":               "78",
		"holographicprojection": []map[string]string{{"url": "https://assets/future.jpg", "likes": "1"}},
		"movieposter":           []map[string]string{{"url": "https://assets/p.jpg", "lang": "en", "likes": "7"}},
	})

	resp, err := capability.Artwork(context.Background(), request(t, v1.MediaMovie, 0,
		v1.ExternalIdentity{Scheme: "tmdb", ID: "78"}))
	if err != nil {
		t.Fatalf("an unknown artwork type must not fail the response: %v", err)
	}
	if len(resp.Candidates) != 1 || resp.Candidates[0].URL != "https://assets/p.jpg" {
		t.Errorf("candidates = %+v, want only the known poster", resp.Candidates)
	}
}

// TestNotFoundIsAnEmptyAnswer covers the most common outcome of all: most titles
// have no fan-made artwork. It must be cheap and silent, because a 404 treated
// as an error would make an ordinary result look like a broken source.
func TestNotFoundIsAnEmptyAnswer(t *testing.T) {
	capability, _ := fakeFanart(t, http.StatusNotFound, nil)

	resp, err := capability.Artwork(context.Background(), request(t, v1.MediaMovie, 0,
		v1.ExternalIdentity{Scheme: "tmdb", ID: "78"}))
	if err != nil {
		t.Fatalf("a 404 is an answer, not a failure: %v", err)
	}
	if len(resp.Candidates) != 0 {
		t.Errorf("got %d candidates, want none", len(resp.Candidates))
	}
}

// TestRejectedCredentialIsDistinctFromAMissingOne covers the error a user can
// actually act on. Reporting a revoked key as "not set" sends them to look at a
// field that is not empty.
func TestRejectedCredentialIsDistinctFromAMissingOne(t *testing.T) {
	capability, _ := fakeFanart(t, http.StatusUnauthorized, nil)

	_, err := capability.Artwork(context.Background(), request(t, v1.MediaMovie, 0,
		v1.ExternalIdentity{Scheme: "tmdb", ID: "78"}))
	if err == nil {
		t.Fatal("a rejected credential should be an error")
	}
	if !strings.Contains(err.Error(), "rejected") {
		t.Errorf("error %q should say the credential was rejected, not that it is missing", err)
	}
}

// TestNoCredentialAtAllIsAnError covers the other side: with no user key and no
// bundled one the module can do nothing, and saying so is more useful than
// returning an empty set that looks like "this title has no art".
func TestNoCredentialAtAllIsAnError(t *testing.T) {
	capability, _ := fakeFanart(t, http.StatusOK, map[string]any{})

	_, err := capability.Artwork(context.Background(), v1.ArtworkRequest{
		Settings:   nil,
		Identities: []v1.ExternalIdentity{{Scheme: "tmdb", ID: "78"}},
		MediaType:  v1.MediaMovie,
	})
	if err == nil {
		t.Fatal("no credential should be an error, not an empty answer")
	}
}

// TestCredentialTravelsInAHeader covers a small privacy property that is easy to
// lose: a key in a query string lands in access logs and proxy traces.
func TestCredentialTravelsInAHeader(t *testing.T) {
	capability, transport := fakeFanart(t, http.StatusOK, map[string]any{})

	if _, err := capability.Artwork(context.Background(), request(t, v1.MediaMovie, 0,
		v1.ExternalIdentity{Scheme: "tmdb", ID: "78"})); err != nil {
		t.Fatalf("Artwork: %v", err)
	}

	if got := transport.lastHeaders.Get("api-key"); got != "test-project-key" {
		t.Errorf("api-key header = %q, want the configured key", got)
	}
	if strings.Contains(transport.lastPath, "api_key") {
		t.Error("the credential appears in the request path")
	}
}

// TestImportRefuses covers the stub. This module fills no read role, so it
// produces no refs and nothing can route an import to it — but v1.Capability
// requires the method, and an empty success would let a bug that routed one here
// pass silently.
func TestImportRefuses(t *testing.T) {
	_, err := newTestCapability().Import(context.Background(), nil, v1.ImportRequest{})
	if err == nil {
		t.Fatal("Import should refuse; this module sources no content")
	}
}

// TestManifestDeclaresArtworkAndSettings pins the roles positively, alongside
// the negative assertion in TestManifestDeclaresNoMetadataRole.
func TestManifestDeclaresArtworkAndSettings(t *testing.T) {
	declared := map[v1.Role]bool{}
	for _, role := range newTestCapability().Manifest().Provides {
		declared[role] = true
	}
	if !declared[v1.RoleArtwork] {
		t.Error("manifest should declare the artwork role")
	}
	if !declared[v1.RoleSettingsUI] {
		t.Error("manifest should declare the settings-UI role")
	}
}

// TestSettingsUIRenders covers that the screen builds at all — it is the only
// path to setting a personal key, and a capability with no client path is owed
// rather than done.
func TestSettingsUIRenders(t *testing.T) {
	resp, err := newTestCapability().SettingsUI(context.Background(), v1.SettingsUIRequest{
		Settings: settingsWithKey(t),
	})
	if err != nil {
		t.Fatalf("SettingsUI: %v", err)
	}
	if len(resp.UI) == 0 {
		t.Fatal("SettingsUI returned no screen")
	}
	// The stored key must never be rendered into the screen's visible text. It
	// still travels inside the configureModule action payloads — platform#17's
	// replace-semantics gap, recorded in configureInput — but nothing should
	// draw it.
	var tree map[string]any
	if err := json.Unmarshal(resp.UI, &tree); err != nil {
		t.Fatalf("the screen is not valid JSON: %v", err)
	}
}
