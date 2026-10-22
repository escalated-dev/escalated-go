package services

import (
	"encoding/json"
	"strings"
	"testing"
)

// An import job never puts its sealed credentials in its JSON encoding; the
// run reads them server-side (escalated-laravel #258).
func TestImportJobJSONHidesTheCredentials(t *testing.T) {
	sealed := "sealed-credentials-7f3a9c2e5b1d"
	b, err := json.Marshal(ImportJob{ID: "job-1", Platform: "fakedesk", Status: "importing", Credentials: &sealed})
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	if strings.Contains(out, sealed) || strings.Contains(strings.ToLower(out), "credentials") {
		t.Errorf("import job JSON carries the credentials: %s", out)
	}
	if !strings.Contains(out, "job-1") {
		t.Errorf("import job JSON = %s", out)
	}
}
