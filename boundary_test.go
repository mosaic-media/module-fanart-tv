package fanarttv_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestModuleImportsOnlyPublishedContracts is the module boundary made
// executable: this module must use only the published *contract* modules — the
// SDK, and the shared SDUI contract it authors its settings screen with
// (ADR 0038) — plus the standard library.
//
// This is an **extension** module (ADR 0062): nothing about Mosaic breaks
// without it, artwork simply stays as good as the metadata source made it. That
// makes the boundary an ecosystem claim rather than a build-safety one — this is
// the shape a third party's module takes, written against nothing but the
// published surface, and it is the first module in the build that is genuinely
// optional rather than core under the guarantee clause.
func TestModuleImportsOnlyPublishedContracts(t *testing.T) {
	const (
		sdkPrefix      = "github.com/mosaic-media/sdk/"
		sduiPrefix     = "github.com/mosaic-media/contracts/"
		platformPrefix = "github.com/mosaic-media/platform/"
	)

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	fset := token.NewFileSet()
	checked := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		checked++

		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range file.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatalf("unquote import in %s: %v", name, err)
			}
			switch {
			// Standard-library imports have no dot in their first segment.
			case !strings.Contains(strings.SplitN(path, "/", 2)[0], "."):
			case strings.HasPrefix(path, sdkPrefix):
				// The published SDK — the primary contract a module builds against.
			case strings.HasPrefix(path, sduiPrefix):
				// The shared SDUI contract — a module builds its own settings UI
				// with the producer binding (ADR 0038, ADR 0025).
			case strings.HasPrefix(path, platformPrefix):
				t.Errorf("%s imports private Platform package %q; a module may import only the SDK", name, path)
			default:
				t.Errorf("%s imports third-party package %q; this module may use only the published contracts and the standard library", name, path)
			}
		}
	}

	if checked == 0 {
		t.Fatal("no non-test source files were checked; the boundary test is not looking at anything")
	}
}

// TestManifestDeclaresNoMetadataRole is the one boundary specific to this module,
// and it guards the mistake ADR 0075 exists to prevent.
//
// ADR 0035 makes a registered RoleMetadata part of the composition-root check
// that a deployment can identify content. This module cannot name a film — it
// has no titles, no search and no catalogs — so declaring the role to reach
// ContentMetadata's image fields would satisfy that check with a module
// structurally incapable of meeting it. The failure would not be a red test or a
// compile error; it would be a deployment that boots and cannot find anything.
//
// So it is asserted here, where it *is* a red test.
func TestManifestDeclaresNoMetadataRole(t *testing.T) {
	forbidden := map[string]string{
		"metadata": "this module cannot describe content; declaring it would satisfy ADR 0035's composition check falsely",
		"search":   "this module cannot be searched — you must already know which title you mean",
		"catalog":  "this module has no collections to browse",
		"stream":   "this module supplies artwork, never playable locations",
	}

	for _, role := range newTestCapability().Manifest().Provides {
		if reason, bad := forbidden[string(role)]; bad {
			t.Errorf("manifest declares role %q: %s", role, reason)
		}
	}
}
