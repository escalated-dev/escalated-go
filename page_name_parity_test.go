package escalated

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Every page name this package renders resolves to a component in
// @escalated-dev/escalated.
//
// Inertia resolving a name to nothing is not an error. The response is a 200,
// the resolver returns undefined, Vue renders nothing, and the panel comes up
// blank -- which reads as a permissions problem or an empty dataset. Screens
// shipped that way across six of the backends in this portfolio before anyone
// noticed, and the handler tests asserting a 200 said they were fine
// throughout.
//
// Neither repo's tests can see the failure alone: a handler test asserts a
// status, and the frontend never hears the name. This is the comparison,
// against the manifest the frontend package publishes and this repo vendors at
// testdata/escalated-pages.json.
//
// Adding a screen goes: component into the frontend, frontend release, refresh
// the fixture, then render the name here. In that order, or it ships blank.

const pageManifest = "testdata/escalated-pages.json"

var pageNamePattern = regexp.MustCompile(`"(Escalated/[A-Za-z0-9/_]+)"`)

func shippedPages(t *testing.T) []string {
	t.Helper()

	raw, err := os.ReadFile(pageManifest)
	if err != nil {
		t.Fatalf("reading the page manifest: %v", err)
	}

	var manifest struct {
		Pages []string `json:"pages"`
	}

	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("parsing the page manifest: %v", err)
	}

	return manifest.Pages
}

// renderedPages maps every page name rendered anywhere in the package to the
// files that render it, so a failure can name the file and not only the string.
func renderedPages(t *testing.T) map[string]map[string]bool {
	t.Helper()

	found := map[string]map[string]bool{}

	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			if name := entry.Name(); name != "." && (strings.HasPrefix(name, ".") || name == "docker" || name == "testdata") {
				return fs.SkipDir
			}

			return nil
		}

		// This file is a list of page names; counting it would make the test
		// pass by comparing itself against the manifest.
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "page_name_parity_test.go") {
			return nil
		}

		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		for _, match := range pageNamePattern.FindAllStringSubmatch(string(source), -1) {
			if found[match[1]] == nil {
				found[match[1]] = map[string]bool{}
			}

			found[match[1]][filepath.ToSlash(path)] = true
		}

		return nil
	})
	if err != nil {
		t.Fatalf("scanning for page names: %v", err)
	}

	return found
}

func explainMissingPages(missing []string, rendered map[string]map[string]bool) string {
	lines := []string{"these page names have no component in @escalated-dev/escalated, so they render a blank panel:"}

	for _, name := range missing {
		files := make([]string, 0, len(rendered[name]))
		for file := range rendered[name] {
			files = append(files, file)
		}
		sort.Strings(files)

		lines = append(lines, fmt.Sprintf("  %s  (%s)", name, strings.Join(files, ", ")))
	}

	lines = append(lines,
		"",
		"Either the name is wrong, or the component has not been released yet.",
		"If it has been: refresh "+pageManifest+" from the package.",
	)

	return strings.Join(lines, "\n")
}

func TestRendersOnlyPageNamesTheFrontendShips(t *testing.T) {
	shipped := shippedPages(t)
	rendered := renderedPages(t)

	if len(rendered) == 0 {
		t.Fatal("found no page names at all, which means this test is not looking where it should")
	}

	shippedSet := map[string]bool{}
	for _, name := range shipped {
		shippedSet[name] = true
	}

	var missing []string
	for name := range rendered {
		if !shippedSet[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)

	if len(missing) > 0 {
		t.Error(explainMissingPages(missing, rendered))
	}
}

func TestThePageManifestIsPresentAndLooksLikeOne(t *testing.T) {
	// A fixture gone missing or empty would make the test above pass by
	// comparing against nothing.
	shipped := shippedPages(t)

	if len(shipped) <= 50 {
		t.Errorf("the manifest has only %d pages, which does not look like the real one", len(shipped))
	}

	for _, name := range shipped {
		if !strings.HasPrefix(name, "Escalated/") {
			t.Errorf("the manifest contains %q, which is not an Escalated page name", name)
		}
	}
}
