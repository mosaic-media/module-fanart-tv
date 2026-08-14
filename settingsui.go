package fanarttv

import (
	"context"

	"github.com/mosaic-media/contracts/sdui"
	"github.com/mosaic-media/contracts/ui"
	v1 "github.com/mosaic-media/sdk/contracts/platform/v1"
)

// SettingsUI renders the module's own settings screen as SDUI (RoleSettingsUI,
// sdk#4): set or replace the project key, add a personal key for early
// access, and read the attribution fanart.tv's terms expect.
//
// Every mutating control is an Invoke of the Platform's configureModule command
// carrying the complete new settings document, so the Platform stays the one
// that persists them and the module never holds state between invocations. The
// screen is returned as serialised UINode JSON, which is what keeps the SDK
// SDUI-agnostic.
func (c *Capability) SettingsUI(ctx context.Context, req v1.SettingsUIRequest) (v1.SettingsUIResponse, error) {
	settings, err := decodeSettings(req.Settings)
	if err != nil {
		return v1.SettingsUIResponse{}, err
	}

	screen := ui.Screen(ui.Title("fanart.tv"), ui.Group(
		whatItDoesSection(),
		apiKeySection(settings),
		clientKeySection(settings),
		attributionSection(),
	))

	data, err := screen.BuildJSON()
	if err != nil {
		return v1.SettingsUIResponse{}, err
	}
	return v1.SettingsUIResponse{UI: data}, nil
}

// configureInput builds the configureModule invoke input — the complete document
// the Platform persists. Controls mutate a copy and pass it here, so a change to
// one field never silently drops the other.
//
// **The whole document, including both keys, is what a control has to carry, and
// that is platform#17's gap rather than a choice made here.** configureModule
// *replaces* the stored document; there is no partial update. So a module with a
// secret setting must echo that secret back through the client on every control
// that changes anything else, or setting the personal key would erase the
// project key. The credential therefore appears inside this screen's action
// payloads — reaching only an admin holding `module.configure`, but bypassing
// the Platform's redaction classes (platform#34), which cannot see inside a
// module's opaque settings document.
//
// This is the same finding `module-tmdb` recorded, reached independently by the
// second module that has a credential. Two modules hitting one gap is what turns
// it from a quirk into an SDK item: configureModule needs either a merge
// semantic or a write-only field.
func configureInput(s Settings) map[string]any {
	return map[string]any{
		"moduleId": CapabilityID,
		"settings": map[string]any{
			"apiKey":    s.APIKey,
			"clientKey": s.ClientKey,
		},
	}
}

// whatItDoesSection explains the module before asking for a credential.
//
// It is here because this module is easy to misunderstand in a specific way: it
// looks like a metadata source and is not one. A user who installs it expecting
// search or titles has installed the wrong thing, and the screen is where that
// is cheapest to say.
func whatItDoesSection() *ui.Element {
	return ui.Section("What this does",
		ui.Banner("fanart.tv supplies artwork — posters, logos, clearart, banners and per-season art — for titles another source has already identified. It does not search, and it does not describe films or series, so it works alongside a metadata source rather than instead of one.", ui.ToneInfo))
}

// apiKeySection is the credential form: the current state of the project key, a
// field to set or replace it, and — only when there is one — a control to clear
// it.
func apiKeySection(s Settings) *ui.Element {
	// A form: the field writes `apiKey` into the form's scope and submit merges
	// the scope into the settings document the invoke carries. The rest of the
	// document travels in the action, so only what was typed comes from the
	// scope (contracts#20). This replaces the "$value" substitution.
	keep := s
	keep.APIKey = ""
	field := ui.Form(
		ui.Vars(sdui.Vars(sdui.Var("apiKey", sdui.VarString, ""))),
		ui.SubmitLabel("Save"),
		ui.SubmitAction(ui.Submit(ui.Invoke("configureModule", configureInput(keep)), "settings")),
		ui.TextInput(
			ui.Prop("name", "apiKey"),
			ui.Prop("placeholder", "Paste your fanart.tv project API key…"),
			ui.Prop("validators", map[string]any{"required": true})))

	// Three states, and the middle one is why this is not a one-liner: a user
	// with no key of their own may still have working artwork, and a screen
	// showing an empty field would read as broken.
	if s.APIKey == "" {
		if !usingBundledKey(s) {
			return ui.Section("Project key",
				ui.Banner("fanart.tv has no anonymous access, so no artwork is fetched until a key is set. Create a free account at fanart.tv, then copy the project API key from your account's API section.", ui.ToneWarning),
				field)
		}
		// The bundled key is described, never shown. It is not this user's
		// credential and there is nothing for them to copy, verify or fix — so
		// rendering any part of it would be noise at best.
		return ui.Section("Project key",
			ui.Banner("Using the project key bundled with Mosaic, so artwork works without any setup. Add your own below if you would rather not share its rate limit — yours will take over immediately.", ui.ToneSuccess),
			statusRow(ui.Badge("Bundled key in use", ui.ToneSuccess)),
			field)
	}

	cleared := s
	cleared.APIKey = ""
	revert := "Clearing it stops artwork being fetched until you add another."
	if bundledKeyPresent() {
		revert = "Clearing it falls back to the key bundled with Mosaic."
	}
	return ui.Section("Project key",
		ui.Banner("Your own key is in use. Saving a new one replaces it. "+revert, ui.ToneSuccess),
		statusRow(
			ui.Badge(maskKey(s.APIKey), ui.ToneNeutral),
			ui.Button("Clear", "danger", ui.OnTap(ui.Invoke("configureModule", configureInput(cleared))))),
		field)
}

// clientKeySection carries the optional personal key.
//
// It is a separate section rather than a second row above because it does a
// different job: the project key decides whether the module works at all, this
// one decides how *fresh* the artwork is. Folding them together would suggest
// both are required, and a user who supplies neither is in a perfectly good
// state.
func clientKeySection(s Settings) *ui.Element {
	keep := s
	keep.ClientKey = ""
	field := ui.Form(
		ui.Vars(sdui.Vars(sdui.Var("clientKey", sdui.VarString, ""))),
		ui.SubmitLabel("Save"),
		ui.SubmitAction(ui.Submit(ui.Invoke("configureModule", configureInput(keep)), "settings")),
		ui.TextInput(
			ui.Prop("name", "clientKey"),
			ui.Prop("placeholder", "Paste your personal fanart.tv key… (optional)")))

	if s.ClientKey == "" {
		return ui.Section("Personal key (optional)",
			ui.Banner("A personal key gives early access to artwork that has been uploaded but not yet promoted, so new and recently-changed titles get art sooner. Everything works without one.", ui.ToneInfo),
			field)
	}

	cleared := s
	cleared.ClientKey = ""
	return ui.Section("Personal key (optional)",
		ui.Banner("Your personal key is in use, so newly-uploaded artwork is available before it is promoted.", ui.ToneSuccess),
		statusRow(
			ui.Badge(maskKey(s.ClientKey), ui.ToneNeutral),
			ui.Button("Clear", "danger", ui.OnTap(ui.Invoke("configureModule", configureInput(cleared))))),
		field)
}

// attributionSection credits the source, as fanart.tv's terms expect of anything
// displaying its images, and gives a route to the site.
func attributionSection() *ui.Element {
	return ui.Section("About",
		ui.Banner("Artwork is supplied by fanart.tv and its contributors. This product is not endorsed or certified by fanart.tv.", ui.ToneInfo),
		ui.Button("Open fanart.tv", "ghost", ui.OnTap(ui.OpenURL("https://fanart.tv"))))
}

// maskKey renders enough of a stored key for a user to recognise which one it is
// and not enough to use it.
//
// The last four characters are what distinguishes two keys a user might be
// choosing between; the rest is what makes the value a credential.
func maskKey(key string) string {
	const shown = 4
	if len(key) <= shown {
		return "••••"
	}
	return "••••" + key[len(key)-shown:]
}

// statusRow lays controls out in a wrapping row, which is what a badge plus a
// button wants on a narrow screen.
func statusRow(els ...ui.El) *ui.Element {
	return ui.Component("Box",
		ui.Prop("style", map[string]any{"direction": "row", "align": "center", "gap": 2, "wrap": true}),
		ui.Group(els...))
}
