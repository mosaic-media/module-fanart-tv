package fanarttv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	v1 "github.com/mosaic-media/sdk/contracts/platform/v1"
)

const (
	// CapabilityID is the id the Platform registers this module under and the id
	// stamped onto every candidate it supplies, so a set assembled from several
	// providers stays attributable (ADR 0074).
	//
	// Unlike every module before it, this id never appears in a ContentRef: this
	// module sources nothing, so nothing ever routes an import back to it.
	CapabilityID = "fanart-tv"
	// modulePath is this module's import path, which is how it reads its own
	// version out of the build graph rather than carrying a constant nothing
	// forces to stay true.
	modulePath = "github.com/mosaic-media/module-fanart-tv"
)

// moduleVersion is resolved once from the build graph. A var rather than a const
// because it is a fact about the binary, discovered at startup, not a literal.
var moduleVersion = v1.ModuleVersion(modulePath)

// defaultAPIKey is a fanart.tv project key linked into the binary at build time,
// so a deployment has working artwork before anyone configures anything. It is
// empty in an ordinary `go build`; Mosaic's release build sets it:
//
//	go build -ldflags "-X github.com/mosaic-media/module-fanart-tv.defaultAPIKey=$FANART_KEY" ./cmd/mosaic-platform
//
// **It is not a secret once the binary ships, and nothing here pretends
// otherwise.** A string linked into a distributed binary is recoverable with
// `strings`, so this is a *shared* credential whose exposure is accepted rather
// than a hidden one. What makes that acceptable is what it can do: it is
// read-only, it reaches only fanart.tv's public artwork index, and it is
// revocable centrally if abused. What it costs is a shared rate limit across
// every deployment that has not set its own — which is why a user can override
// it, and why the settings screen says plainly which one is in use.
//
// It is never written into the settings document, never rendered, and never
// logged. resolveKeys is the only function that reads it; keeping it that way is
// what makes those three claims verifiable by reading rather than by trust.
var defaultAPIKey string

// Capability satisfies the SDK's capability contract and the roles it declares.
// The assertions fail to compile if the module drifts from what the Platform
// invokes or from a role it claims to fill (ADR 0027).
var (
	_ v1.Capability         = (*Capability)(nil)
	_ v1.ArtworkProvider    = (*Capability)(nil)
	_ v1.SettingsUIProvider = (*Capability)(nil)
)

// Capability is the fanart.tv artwork module.
//
// It fills exactly one content role — RoleArtwork — plus its own settings
// screen, and the roles it does *not* fill are the whole shape of it. fanart.tv
// has no titles, no overviews, no years, no cast, no search and no catalogs;
// there is no query that turns "Blade Runner" into a result here, because you
// must already know which film you mean. It cannot describe content and must
// never claim it can (ADR 0075).
type Capability struct {
	http *http.Client
}

// New builds the capability over an HTTP client (nil for a default). The
// Platform passes its own, which carries the netguard dial guard and the
// outbound telemetry seam (ADR 0055).
//
// The client is built per invocation rather than here, because the credential
// comes from the settings document the Platform hands in on each call and a user
// may change it between two of them.
func New(httpClient *http.Client) *Capability {
	return &Capability{http: httpClient}
}

// Manifest is the module's self-declaration, including the provider roles it
// fills (ADR 0027).
//
// **The roles it does not declare matter more than the ones it does.** It does
// not declare RoleMetadata, and the temptation to is the specific mistake ADR
// 0075 exists to prevent: ADR 0035 makes a registered RoleMetadata part of the
// composition-root check that Mosaic can identify content, and a module that
// cannot name a film has no business satisfying it. Declaring the role to reach
// ContentMetadata's image fields would produce a deployment that boots and
// cannot identify anything — a failure no test would catch.
func (c *Capability) Manifest() v1.Manifest {
	return v1.Manifest{
		ID:       CapabilityID,
		Version:  moduleVersion,
		Name: "fanart.tv artwork",
		Description: "A community library of high-resolution artwork — clear logos, character art, " +
			"banners and backdrops. It adds nothing to your library on its own: it dresses what is " +
			"already in it.",
		Provides: []v1.Role{v1.RoleArtwork, v1.RoleSettingsUI},
	}
}

// errCannotImport is what Import always returns.
var errCannotImport = errors.New("fanart.tv supplies artwork for content another source identified; it cannot import content of its own")

// Import is required by v1.Capability and is unreachable for this module.
//
// Import materialises the item named by a ContentRef, and a ref names the
// capability that produced it. This module fills no read role, so it produces no
// refs, so nothing can name it — the method exists because the interface
// requires it, not because there is a path to it.
//
// It returns an error rather than an empty success. An empty success would say
// "I materialised nothing, and that is fine", which would let a bug that routed
// an import here pass silently; an error says what is actually true.
//
// **This is a finding, not a workaround.** v1.Capability bundles identity with
// the one write verb, which fits a module that sources content and does not fit
// one that enriches it. An enrichment-only module has to stub the verb. Worth
// taking to the SDK if a second such module appears.
func (c *Capability) Import(ctx context.Context, svc v1.ContentService, req v1.ImportRequest) (v1.ImportResult, error) {
	return v1.ImportResult{}, errCannotImport
}

// Artwork resolves artwork candidates for content this module did not source
// (RoleArtwork, ADR 0075).
//
// It returns an empty response and no error when it cannot address the content —
// a film with neither a TMDB nor an IMDb id, a series with no TVDB id, or a
// title fanart.tv simply has nothing for. All three are ordinary and none is a
// failure: erroring would make an artwork lookup able to disrupt an import, and
// the art it might have added is by definition an improvement on top of art the
// node already has.
func (c *Capability) Artwork(ctx context.Context, req v1.ArtworkRequest) (v1.ArtworkResponse, error) {
	settings, err := decodeSettings(req.Settings)
	if err != nil {
		return v1.ArtworkResponse{}, err
	}

	apiKey, clientKey := resolveKeys(settings)
	if apiKey == "" {
		return v1.ArtworkResponse{}, errNoAPIKey
	}

	client := NewClient(c.http, apiKey, clientKey)
	candidates, err := client.Artwork(ctx, req.MediaType, req.Identities, req.Season)
	if err != nil {
		return v1.ArtworkResponse{}, fmt.Errorf("resolve fanart.tv artwork: %w", err)
	}
	return v1.ArtworkResponse{Candidates: candidates}, nil
}

// Settings is this module's user-managed configuration (ADR 0021).
//
// The Platform stores it uninterpreted and hands it back on every invocation;
// what the fields mean is this module's business.
type Settings struct {
	// APIKey is the user's own fanart.tv project key.
	//
	// **It only ever holds the user's key.** It is never populated from
	// defaultAPIKey as a convenience: configureModule replaces the whole
	// settings document, so the bundled key reaching this field would write a
	// shared build-time credential into a user's stored settings the next time
	// they touched any control.
	APIKey string `json:"apiKey,omitempty"`
	// ClientKey is a user's personal fanart.tv key, which grants early access to
	// artwork that has been uploaded but not yet promoted. It is optional and
	// independent of APIKey — a user may supply one, both or neither.
	ClientKey string `json:"clientKey,omitempty"`
}

// decodeSettings reads the module's settings document.
//
// An absent or empty document is valid and means "nothing configured", which
// with a bundled key is a fully working module.
func decodeSettings(raw []byte) (Settings, error) {
	var settings Settings
	if len(raw) == 0 {
		return settings, nil
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		return Settings{}, fmt.Errorf("decode fanart.tv settings: %w", err)
	}
	settings.APIKey = strings.TrimSpace(settings.APIKey)
	settings.ClientKey = strings.TrimSpace(settings.ClientKey)
	return settings, nil
}

// resolveKeys picks the credentials for one invocation: the user's key when
// they have set one, otherwise the key linked into the binary.
//
// **This is the only function that reads defaultAPIKey.** Keeping it that way is
// what makes "never written into settings, never rendered, never logged" a
// property that can be verified by reading rather than a claim to trust.
func resolveKeys(settings Settings) (apiKey, clientKey string) {
	apiKey = settings.APIKey
	if apiKey == "" {
		apiKey = defaultAPIKey
	}
	return apiKey, settings.ClientKey
}

// usingBundledKey reports whether this deployment is running on the shared
// build-time credential, so the settings screen can say which one is in use
// without ever rendering either.
func usingBundledKey(settings Settings) bool {
	return settings.APIKey == "" && defaultAPIKey != ""
}
