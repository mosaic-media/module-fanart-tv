//go:build linkercheck

package fanarttv

import "testing"

// TestLinkerInjectionPathResolves is the linker-injection guard architecture#4
// rule 3 makes mandatory, behind a build tag so it runs only in the pass that
// supplies the flag.
//
// A -X against a path that does not resolve is silently ignored. Rename the
// variable, move it to another file's package, or mistype the module path in the
// release workflow, and the build still succeeds — it just links nothing, and
// every install that relied on the bundled key gets "fanart.tv API key not set"
// with no error anywhere upstream of a user's screen.
//
// The container gate runs the suite twice: once normally, and once as
//
//	go test -tags linkercheck \
//	  -ldflags "-X github.com/mosaic-media/module-fanart-tv.defaultAPIKey=$canary" \
//	  -run TestLinkerInjectionPathResolves ./...
//
// so the symbol path in the release build is verified by the same string that
// appears in this repository's own gate. The path is spelled in three files
// besides this one and they must stay in step: docker-compose.test.yml,
// .github/workflows/verify.yml, and .github/workflows/release.yml's binaries
// job, which is the build that actually ships it — an extension module links its
// own key (architecture#4 rule 2), because its binaries are cross-compiled here
// and distributed through the signed registry rather than compiled into the
// Platform.
func TestLinkerInjectionPathResolves(t *testing.T) {
	const canary = "linker-injection-canary"
	if defaultAPIKey != canary {
		t.Fatalf("defaultAPIKey = %q, want %q — the -X symbol path no longer resolves, "+
			"so a release build would link no key and fail silently", defaultAPIKey, canary)
	}

	// The resolution must also use it, not merely store it. Both halves are
	// asserted: a user with no key of their own falls through to the bundled
	// one, and a user with a key is never overridden by it.
	if apiKey, _ := resolveKeys(Settings{}); apiKey != canary {
		t.Fatalf("resolveKeys with no user key = %q, want the linked-in %q", apiKey, canary)
	}
	if apiKey, _ := resolveKeys(Settings{APIKey: "mine"}); apiKey != "mine" {
		t.Fatalf("resolveKeys with a user key = %q, want %q — a personal key must win", apiKey, "mine")
	}

	// The settings screen must be able to tell which key is in use, which is what
	// makes architecture#4 rule 6's middle state ("the project key is in use")
	// reachable.
	if !usingBundledKey(Settings{}) {
		t.Fatal("usingBundledKey with a linked-in key and no user key = false, want true")
	}
	if usingBundledKey(Settings{APIKey: "mine"}) {
		t.Fatal("usingBundledKey with a user key = true, want false")
	}
}
