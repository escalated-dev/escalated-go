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

// renderCallPattern catches the first string argument of every Render call,
// prefixed or not. The frontend resolver strips an optional "Escalated/", so an
// unprefixed name is checked as if it carried the prefix; matching only the
// prefixed spelling let five unprefixed names that resolve to nothing through.
var renderCallPattern = regexp.MustCompile(`\.Render\(\s*\w+\s*,\s*\w+\s*,\s*"([A-Za-z0-9/_]+)"`)

func normalizePageName(name string) string {
	if strings.HasPrefix(name, "Escalated/") {
		return name
	}
	return "Escalated/" + name
}

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

		matches := pageNamePattern.FindAllStringSubmatch(string(source), -1)
		matches = append(matches, renderCallPattern.FindAllStringSubmatch(string(source), -1)...)
		for _, match := range matches {
			name := normalizePageName(match[1])
			if found[name] == nil {
				found[name] = map[string]bool{}
			}

			found[name][filepath.ToSlash(path)] = true
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

func TestTheGuardSeesUnprefixedRenderCalls(t *testing.T) {
	src := `_ = h.renderer.Render(w, r, "Agent/Tickets/Index", map[string]any{})`
	m := renderCallPattern.FindAllStringSubmatch(src, -1)
	if len(m) != 1 || normalizePageName(m[0][1]) != "Escalated/Agent/Tickets/Index" {
		t.Fatalf("render call pattern missed an unprefixed page name: %v", m)
	}

	// The ticket screens are rendered from these files; if the scan cannot see
	// them it is not looking where it should.
	rendered := renderedPages(t)
	for _, name := range []string{"Escalated/Agent/TicketIndex", "Escalated/Agent/TicketShow", "Escalated/Agent/Dashboard", "Escalated/Customer/Index", "Escalated/Customer/Create", "Escalated/Customer/Show", "Escalated/Admin/SlaPolicies/Index"} {
		if len(rendered[name]) == 0 {
			t.Errorf("no render call found for %s", name)
		}
	}
}

// notYetRendered lists the shipped pages this package does not render yet. It
// is the to-do list: render a page and its name must leave the list, and a
// page the frontend adds must be listed or rendered.
var notYetRendered = []string{
	"Escalated/Admin/ApiTokens/Index",
	"Escalated/Admin/Automations/Form",
	"Escalated/Admin/Automations/Index",
	"Escalated/Admin/BusinessHours/Form",
	"Escalated/Admin/BusinessHours/Index",
	"Escalated/Admin/CannedResponses/Index",
	"Escalated/Admin/Capacity/Index",
	"Escalated/Admin/CustomFields/Form",
	"Escalated/Admin/CustomFields/Index",
	"Escalated/Admin/CustomObjects/Form",
	"Escalated/Admin/CustomObjects/Index",
	"Escalated/Admin/CustomObjects/Records",
	"Escalated/Admin/Departments/Form",
	"Escalated/Admin/EscalationRules/Form",
	"Escalated/Admin/EscalationRules/Index",
	"Escalated/Admin/Import/Connect",
	"Escalated/Admin/Import/Index",
	"Escalated/Admin/Import/Mapping",
	"Escalated/Admin/Import/Progress",
	"Escalated/Admin/Import/Review",
	"Escalated/Admin/KnowledgeBase/Articles/Form",
	"Escalated/Admin/KnowledgeBase/Articles/Index",
	"Escalated/Admin/KnowledgeBase/Categories/Index",
	"Escalated/Admin/Macros/Index",
	"Escalated/Admin/Plugins/Index",
	"Escalated/Admin/Reports",
	"Escalated/Admin/Reports/AgentDetail",
	"Escalated/Admin/Reports/AgentMetrics",
	"Escalated/Admin/Reports/AgentRanking",
	"Escalated/Admin/Reports/Cohorts",
	"Escalated/Admin/Reports/Comparison",
	"Escalated/Admin/Reports/CsatReport",
	"Escalated/Admin/Reports/Dashboard",
	"Escalated/Admin/Reports/ResolutionTimes",
	"Escalated/Admin/Reports/ResponseTimes",
	"Escalated/Admin/Reports/SlaReport",
	"Escalated/Admin/Reports/SlaTrends",
	"Escalated/Admin/Roles/Form",
	"Escalated/Admin/Roles/Index",
	"Escalated/Admin/Settings",
	"Escalated/Admin/Settings/CsatSettings",
	"Escalated/Admin/Settings/DataRetention",
	"Escalated/Admin/Settings/DatabaseConnection",
	"Escalated/Admin/Settings/EmailSettings",
	"Escalated/Admin/Settings/PublicTickets",
	"Escalated/Admin/Settings/Sandbox",
	"Escalated/Admin/Settings/SsoSettings",
	"Escalated/Admin/Settings/TwoFactor",
	"Escalated/Admin/SlaPolicies/Form",
	"Escalated/Admin/Statuses/Form",
	"Escalated/Admin/Statuses/Index",
	"Escalated/Admin/Webhooks/DeliveryLog",
	"Escalated/Admin/Webhooks/Form",
	"Escalated/Admin/Webhooks/Index",
	"Escalated/Admin/Workflows/Builder",
	"Escalated/Admin/Workflows/Form",
	"Escalated/Admin/Workflows/Index",
	"Escalated/Admin/Workflows/Logs",
	"Escalated/Auth/TwoFactorChallenge",
	"Escalated/Customer/KnowledgeBase/Article",
	"Escalated/Customer/KnowledgeBase/Index",
	"Escalated/Error",
	"Escalated/Guest/Create",
	"Escalated/Guest/Show",
	"Escalated/Plugin/Page",
}

func TestNotYetRenderedListIsExact(t *testing.T) {
	rendered := renderedPages(t)
	listed := map[string]bool{}
	for _, name := range notYetRendered {
		if listed[name] {
			t.Errorf("%s is listed twice", name)
		}
		listed[name] = true
		if len(rendered[name]) > 0 {
			t.Errorf("%s is rendered now; remove it from notYetRendered", name)
		}
	}
	for _, name := range shippedPages(t) {
		if len(rendered[name]) == 0 && !listed[name] {
			t.Errorf("%s ships but is neither rendered nor listed in notYetRendered", name)
		}
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
