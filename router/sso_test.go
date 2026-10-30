package router_test

import (
	"bytes"
	"compress/flate"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beevik/etree"
	"github.com/golang-jwt/jwt/v5"
	dsig "github.com/russellhaering/goxmldsig"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/models"
)

// Single sign-on (ADR 2026-10-29-configurable-access-features, port
// checklist "SSO"): an install-level switch, off by default, with the
// provider (SAML, OAuth 2 / OIDC, JWT) chosen in Admin > Settings > SSO.

const (
	ssoBase   = "https://support.example.com"
	idpEntity = "https://idp.example.com/metadata"
	idpSSO    = "https://idp.example.com/sso"
	acsURL    = ssoBase + "/escalated/sso/saml/acs"
	spEntity  = ssoBase + "/escalated/sso/saml/metadata"
)

// ssoHost is the host's side of SSO: its users by email, who it signed in
// and the users it created.
type ssoHost struct {
	mu       sync.Mutex
	users    map[string]models.UserID
	signedIn []models.UserID
	created  []string
}

func (h *ssoHost) byEmail(_ context.Context, email string) (models.UserID, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.users[strings.ToLower(email)], nil
}

func (h *ssoHost) signIn(_ http.ResponseWriter, _ *http.Request, id models.UserID) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.signedIn = append(h.signedIn, id)
	return nil
}

func (h *ssoHost) create(_ context.Context, email, name string) (models.UserID, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.created = append(h.created, email+"|"+name)
	id := models.UserID("950")
	h.users[strings.ToLower(email)] = id
	return id, nil
}

func (h *ssoHost) last() models.UserID {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.signedIn) == 0 {
		return ""
	}
	return h.signedIn[len(h.signedIn)-1]
}

func ssoOn(on bool, host *ssoHost, extra ...func(*escalated.Config)) func(*escalated.Config) {
	return func(cfg *escalated.Config) {
		cfg.Access.SSO = &on
		cfg.Access.Secret = "test-access-secret"
		cfg.Access.SSOOptions.BaseURL = ssoBase
		cfg.Access.SSOOptions.SignIn = host.signIn
		cfg.Access.SSOOptions.CreateUser = host.create
		cfg.UserIDByEmail = host.byEmail
		for _, e := range extra {
			e(cfg)
		}
	}
}

func newSSOFixture(t *testing.T, on bool, extra ...func(*escalated.Config)) (*uiFixture, *ssoHost) {
	t.Helper()
	host := &ssoHost{users: map[string]models.UserID{"ada@example.com": uiAgent}}
	f := newUIFixture(t, ssoOn(on, host, extra...))
	return f, host
}

func (f *uiFixture) setSettings(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		if err := f.esc.Store.SetSetting(context.Background(), k, v); err != nil {
			t.Fatal(err)
		}
	}
}

func get(h http.Handler, path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func postForm(h http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func wantSignedIn(t *testing.T, rec *httptest.ResponseRecorder, host *ssoHost, user models.UserID, what string) {
	t.Helper()
	if rec.Code != http.StatusSeeOther || host.last() != user {
		t.Fatalf("%s: %d (signed in %q, want %q): %s", what, rec.Code, host.last(), user, rec.Body.String())
	}
}

func wantRefused(t *testing.T, rec *httptest.ResponseRecorder, host *ssoHost, what string) {
	t.Helper()
	before := len(host.signedIn)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "Single sign-on failed") {
		t.Errorf("%s: %d %s, want the generic 403", what, rec.Code, rec.Body.String())
	}
	if len(host.signedIn) != before {
		t.Errorf("%s: someone was signed in", what)
	}
}

// ------------------------------------------------------------- switches

func TestSSOSwitchOffByDefault(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			host := &ssoHost{users: map[string]models.UserID{}}
			f := newUIFixture(t, func(cfg *escalated.Config) {
				cfg.Access.SSOOptions.SignIn = host.signIn
				cfg.UserIDByEmail = host.byEmail
			})
			h := f.mounts()[name]
			if f.esc.Features.SSO {
				t.Fatal("SSO is on by default")
			}
			f.setSettings(t, map[string]string{"sso_provider": "jwt", "sso_url": "https://idp.example.com/login", "sso_jwt_secret": "s"})
			for _, req := range [][2]string{
				{http.MethodGet, "/escalated/sso/login"},
				{http.MethodGet, "/escalated/sso/saml/metadata"},
				{http.MethodPost, "/escalated/sso/saml/acs"},
				{http.MethodGet, "/escalated/sso/oauth/callback"},
				{http.MethodGet, "/escalated/sso/jwt/callback"},
				{http.MethodPost, "/escalated/sso/jwt/callback"},
				{http.MethodGet, "/escalated/admin/settings/sso"},
				{http.MethodPost, "/escalated/admin/settings/sso"},
			} {
				if rec := send(h, req[0], req[1], uiAgent, "admin", `{"sso_provider":"none"}`); rec.Code != http.StatusNotFound {
					t.Errorf("%s %s with the switch off: %d, want 404", req[0], req[1], rec.Code)
				}
			}
			page := visit(t, h, "/escalated/admin/departments", uiAgent, "admin")
			if obj(t, sharedEscalated(t, page), "features")["sso"] != false {
				t.Error("features.sso is not false")
			}
			// A setting named like the switch changes nothing.
			if rec := send(h, http.MethodPost, "/escalated/admin/settings", uiAgent, "admin", `{"sso":true}`); rec.Code >= 500 {
				t.Fatal(rec.Code)
			}
			if rec := get(h, "/escalated/sso/login"); rec.Code != http.StatusNotFound {
				t.Errorf("login after posting a setting: %d", rec.Code)
			}
		})
	}
}

func TestSSONeedsTheHostHooks(t *testing.T) {
	on := true
	db := newUIFixture(t).esc.Config.DB
	host := &ssoHost{users: map[string]models.UserID{}}
	for what, configure := range map[string]func(*escalated.Config){
		"no SignIn": func(cfg *escalated.Config) { cfg.UserIDByEmail = host.byEmail; cfg.Access.SSOOptions.BaseURL = ssoBase },
		"no UserIDByEmail": func(cfg *escalated.Config) {
			cfg.Access.SSOOptions.SignIn = host.signIn
			cfg.Access.SSOOptions.BaseURL = ssoBase
		},
		"no base URL": func(cfg *escalated.Config) {
			cfg.Access.SSOOptions.SignIn = host.signIn
			cfg.UserIDByEmail = host.byEmail
		},
		"create without CreateUser": func(cfg *escalated.Config) {
			cfg.Access.SSOOptions.SignIn = host.signIn
			cfg.UserIDByEmail = host.byEmail
			cfg.Access.SSOOptions.BaseURL = ssoBase
			cfg.Access.SSOOptions.CreateUsers = &on
		},
	} {
		cfg := escalated.DefaultConfig()
		cfg.DB = db
		cfg.Access.SSO = &on
		cfg.Access.Secret = "s"
		configure(&cfg)
		if _, err := escalated.New(cfg); err == nil {
			t.Errorf("%s: New accepted SSO without it", what)
		}
	}
}

func TestSSOProviderNoneAnswers404AndSettingsPage(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f, _ := newSSOFixture(t, true)
			h := f.mounts()[name]
			if rec := get(h, "/escalated/sso/login"); rec.Code != http.StatusNotFound {
				t.Errorf("login with sso_provider none: %d, want 404", rec.Code)
			}
			for _, p := range []string{"/escalated/sso/saml/metadata", "/escalated/sso/oauth/callback", "/escalated/sso/jwt/callback"} {
				if rec := get(h, p); rec.Code != http.StatusNotFound {
					t.Errorf("%s with sso_provider none: %d", p, rec.Code)
				}
			}

			page := visit(t, h, "/escalated/admin/settings/sso", uiAgent, "admin")
			if page.Component != "Escalated/Admin/Settings/SsoSettings" {
				t.Fatalf("component %q", page.Component)
			}
			endpoints := obj(t, page.Props, "endpoints")
			want := map[string]string{"login": ssoBase + "/escalated/sso/login", "sp_entity_id": spEntity, "metadata": spEntity, "acs": acsURL,
				"oauth_redirect": ssoBase + "/escalated/sso/oauth/callback", "jwt_callback": ssoBase + "/escalated/sso/jwt/callback"}
			for k, v := range want {
				if endpoints[k] != v {
					t.Errorf("endpoints.%s = %v, want %s", k, endpoints[k], v)
				}
			}
			if obj(t, sharedEscalated(t, page), "features")["sso"] != true {
				t.Error("features.sso is not true")
			}

			// Save JWT settings; the secret comes back masked and a posted mask
			// keeps it.
			rec := pageJSON(h, http.MethodPost, "/escalated/admin/settings/sso", `{"sso_provider":"jwt","sso_url":"https://idp.example.com/login","sso_jwt_secret":"top-secret","sso_jwt_algorithm":"HS256"}`)
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
			}
			page = visit(t, h, "/escalated/admin/settings/sso", uiAgent, "admin")
			settings := obj(t, page.Props, "settings")
			if settings["sso_jwt_secret"] != "********" || settings["sso_provider"] != "jwt" {
				t.Errorf("settings %v", settings)
			}
			if strings.Contains(mustJSON(t, page.Props), "top-secret") {
				t.Error("the JWT secret reached the page")
			}
			pageJSON(h, http.MethodPost, "/escalated/admin/settings/sso", `{"sso_provider":"jwt","sso_url":"https://idp.example.com/login","sso_jwt_secret":"********"}`)
			if f.setting(t, "sso_jwt_secret") != "top-secret" {
				t.Error("posting the mask replaced the secret")
			}
			// Validation: SAML needs its entity ID and sign-in URL.
			bad := pageOf(t, pageJSON(h, http.MethodPost, "/escalated/admin/settings/sso", `{"sso_provider":"saml","sso_login_url":"not a url"}`))
			hasKeys(t, "errors", obj(t, bad.Props, "errors"), "sso_entity_id", "sso_login_url")
			// Settings need the admin gate.
			if rec := send(h, http.MethodGet, "/escalated/admin/settings/sso", uiAgent, "agent", ""); rec.Code != http.StatusForbidden {
				t.Errorf("agent on the SSO settings: %d", rec.Code)
			}
		})
	}
}

// ------------------------------------------------------------------- SAML

type testIdP struct {
	key     *rsa.PrivateKey
	certDER []byte
}

func (i *testIdP) GetKeyPair() (*rsa.PrivateKey, []byte, error) { return i.key, i.certDER, nil }

func (i *testIdP) certPEM() string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: i.certDER}))
}

func newTestIdP(t *testing.T) *testIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "Test IdP"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return &testIdP{key: key, certDER: der}
}

type samlOpts struct {
	inResponseTo, issuer, audience, destination, recipient, email, role, assertionID string
	notBefore, notOnOrAfter                                                          time.Time
	signAssertion, signResponse                                                      bool
	signer                                                                           *testIdP
	tamper, encrypted, twoAssertions                                                 bool
}

func (i *testIdP) defaults(requestID string) samlOpts {
	return samlOpts{
		inResponseTo: requestID, issuer: idpEntity, audience: spEntity, destination: acsURL, recipient: acsURL,
		email: "ada@example.com", assertionID: "_a" + randomHex(), notBefore: time.Now().Add(-time.Minute),
		notOnOrAfter: time.Now().Add(5 * time.Minute), signAssertion: true, signer: i,
	}
}

func randomHex() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return strings.ToLower(base64.RawURLEncoding.EncodeToString(b))
}

// signingContext signs as IdPs do: RSA-SHA256 with exclusive
// canonicalization.
func signingContext(idp *testIdP) *dsig.SigningContext {
	ctx := dsig.NewDefaultSigningContext(idp)
	ctx.Canonicalizer = dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList("")
	return ctx
}

const (
	nsProtocol  = "urn:oasis:names:tc:SAML:2.0:protocol"
	nsAssertion = "urn:oasis:names:tc:SAML:2.0:assertion"
)

func (i *testIdP) assertion(t *testing.T, o samlOpts) *etree.Element {
	t.Helper()
	ts := func(tm time.Time) string { return tm.UTC().Format(time.RFC3339) }
	a := etree.NewElement("saml:Assertion")
	a.CreateAttr("xmlns:saml", nsAssertion)
	a.CreateAttr("ID", o.assertionID)
	a.CreateAttr("Version", "2.0")
	a.CreateAttr("IssueInstant", ts(time.Now()))
	a.CreateElement("saml:Issuer").SetText(o.issuer)
	subject := a.CreateElement("saml:Subject")
	subject.CreateElement("saml:NameID").SetText(o.email)
	sc := subject.CreateElement("saml:SubjectConfirmation")
	sc.CreateAttr("Method", "urn:oasis:names:tc:SAML:2.0:cm:bearer")
	scd := sc.CreateElement("saml:SubjectConfirmationData")
	if o.inResponseTo != "" {
		scd.CreateAttr("InResponseTo", o.inResponseTo)
	}
	scd.CreateAttr("Recipient", o.recipient)
	scd.CreateAttr("NotOnOrAfter", ts(o.notOnOrAfter))
	cond := a.CreateElement("saml:Conditions")
	cond.CreateAttr("NotBefore", ts(o.notBefore))
	cond.CreateAttr("NotOnOrAfter", ts(o.notOnOrAfter))
	cond.CreateElement("saml:AudienceRestriction").CreateElement("saml:Audience").SetText(o.audience)
	authn := a.CreateElement("saml:AuthnStatement")
	authn.CreateAttr("AuthnInstant", ts(time.Now()))
	authn.CreateElement("saml:AuthnContext").CreateElement("saml:AuthnContextClassRef").SetText("urn:oasis:names:tc:SAML:2.0:ac:classes:Password")
	attrs := a.CreateElement("saml:AttributeStatement")
	for name, value := range map[string]string{"email": o.email, "name": "Ada Agent", "role": o.role} {
		if value == "" {
			continue
		}
		attr := attrs.CreateElement("saml:Attribute")
		attr.CreateAttr("Name", name)
		attr.CreateElement("saml:AttributeValue").SetText(value)
	}
	if o.signAssertion {
		signed, err := signingContext(o.signer).SignEnveloped(a)
		if err != nil {
			t.Fatal(err)
		}
		a = signed
	}
	return a
}

func (i *testIdP) response(t *testing.T, o samlOpts) string {
	t.Helper()
	resp := etree.NewElement("samlp:Response")
	resp.CreateAttr("xmlns:samlp", nsProtocol)
	resp.CreateAttr("xmlns:saml", nsAssertion)
	resp.CreateAttr("ID", "_r"+randomHex())
	resp.CreateAttr("Version", "2.0")
	resp.CreateAttr("IssueInstant", time.Now().UTC().Format(time.RFC3339))
	resp.CreateAttr("Destination", o.destination)
	if o.inResponseTo != "" {
		resp.CreateAttr("InResponseTo", o.inResponseTo)
	}
	resp.CreateElement("saml:Issuer").SetText(o.issuer)
	resp.CreateElement("samlp:Status").CreateElement("samlp:StatusCode").CreateAttr("Value", "urn:oasis:names:tc:SAML:2.0:status:Success")
	if o.encrypted {
		resp.CreateElement("saml:EncryptedAssertion").CreateElement("xenc:EncryptedData")
	} else {
		resp.AddChild(i.assertion(t, o))
		if o.twoAssertions {
			second := o
			second.assertionID = "_a" + randomHex()
			second.email = "mallory@example.com"
			resp.AddChild(i.assertion(t, second))
		}
	}
	if o.signResponse {
		signed, err := signingContext(o.signer).SignEnveloped(resp)
		if err != nil {
			t.Fatal(err)
		}
		resp = signed
	}
	doc := etree.NewDocument()
	doc.SetRoot(resp)
	xml, err := doc.WriteToString()
	if err != nil {
		t.Fatal(err)
	}
	if o.tamper {
		xml = strings.Replace(xml, ">ada@example.com<", ">eve@example.com<", 1)
	}
	return base64.StdEncoding.EncodeToString([]byte(xml))
}

// samlLogin starts a sign-in and returns the AuthnRequest ID the IdP must
// answer.
func samlLogin(t *testing.T, h http.Handler) string {
	t.Helper()
	rec := get(h, "/escalated/sso/login")
	if rec.Code != http.StatusFound {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil || !strings.HasPrefix(loc.String(), idpSSO+"?") {
		t.Fatalf("login went to %q", rec.Header().Get("Location"))
	}
	raw, err := base64.StdEncoding.DecodeString(loc.Query().Get("SAMLRequest"))
	if err != nil {
		t.Fatal(err)
	}
	xmlBytes, err := io.ReadAll(flate.NewReader(bytes.NewReader(raw)))
	if err != nil {
		t.Fatal(err)
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(xmlBytes); err != nil {
		t.Fatal(err)
	}
	if acs := doc.Root().SelectAttrValue("AssertionConsumerServiceURL", ""); acs != acsURL {
		t.Errorf("AuthnRequest ACS %q", acs)
	}
	return doc.Root().SelectAttrValue("ID", "")
}

func TestSSOSAML(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f, host := newSSOFixture(t, true)
			h := f.mounts()[name]
			idp, other := newTestIdP(t), newTestIdP(t)
			f.setSettings(t, map[string]string{"sso_provider": "saml", "sso_entity_id": idpEntity, "sso_login_url": idpSSO})
			acs := func(response string) *httptest.ResponseRecorder {
				return postForm(h, "/escalated/sso/saml/acs", url.Values{"SAMLResponse": {response}})
			}

			// No certificate configured: every response is refused.
			if rec := get(h, "/escalated/sso/login"); rec.Code != http.StatusForbidden {
				t.Errorf("login without a certificate: %d", rec.Code)
			}
			wantRefused(t, acs(idp.response(t, idp.defaults(""))), host, "no certificate")
			f.setSettings(t, map[string]string{"sso_certificate": idp.certPEM()})

			// Metadata.
			md := get(h, "/escalated/sso/saml/metadata")
			if md.Code != http.StatusOK || !strings.Contains(md.Body.String(), acsURL) || !strings.Contains(md.Body.String(), `entityID="`+spEntity+`"`) {
				t.Errorf("metadata: %d %s", md.Code, md.Body.String())
			}

			// A correctly signed response for the pending request signs in.
			id := samlLogin(t, h)
			ok := idp.defaults(id)
			wantSignedIn(t, acs(idp.response(t, ok)), host, uiAgent, "valid response")

			refusals := map[string]func(o *samlOpts){
				"unsigned assertion":     func(o *samlOpts) { o.signAssertion = false },
				"signed Response only":   func(o *samlOpts) { o.signAssertion, o.signResponse = false, true },
				"signed by another key":  func(o *samlOpts) { o.signer = other },
				"tampered after signing": func(o *samlOpts) { o.tamper = true },
				"wrong Issuer":           func(o *samlOpts) { o.issuer = "https://evil.example.com" },
				"wrong Audience":         func(o *samlOpts) { o.audience = "https://other-sp.example.com" },
				"wrong Destination":      func(o *samlOpts) { o.destination = "https://evil.example.com/acs" },
				"wrong Recipient":        func(o *samlOpts) { o.recipient = "https://evil.example.com/acs" },
				"expired": func(o *samlOpts) {
					o.notBefore, o.notOnOrAfter = time.Now().Add(-time.Hour), time.Now().Add(-10*time.Minute)
				},
				"not yet valid":                func(o *samlOpts) { o.notBefore = time.Now().Add(10 * time.Minute) },
				"unknown InResponseTo":         func(o *samlOpts) { o.inResponseTo = "_not-issued-here" },
				"missing InResponseTo":         func(o *samlOpts) { o.inResponseTo = "" },
				"encrypted assertion":          func(o *samlOpts) { o.encrypted = true },
				"two assertions":               func(o *samlOpts) { o.twoAssertions = true },
				"same assertion a second time": func(o *samlOpts) { o.assertionID = ok.assertionID },
			}
			for what, change := range refusals {
				o := idp.defaults(samlLogin(t, h))
				change(&o)
				if what == "unknown InResponseTo" || what == "missing InResponseTo" {
					// The pending request is left unanswered.
					o2 := o
					wantRefused(t, acs(idp.response(t, o2)), host, what)
					continue
				}
				wantRefused(t, acs(idp.response(t, o)), host, what)
			}

			// A response answering a request is consumed with it.
			id = samlLogin(t, h)
			first := idp.response(t, idp.defaults(id))
			wantSignedIn(t, acs(first), host, uiAgent, "fresh request")
			wantRefused(t, acs(first), host, "the same response again")
		})
	}
}

func TestSSOSAMLIdPInitiatedOnlyWhenAllowed(t *testing.T) {
	on := true
	f, host := newSSOFixture(t, true, func(cfg *escalated.Config) { cfg.Access.SSOOptions.AllowIdPInitiated = &on })
	h := f.mounts()["chi"]
	idp := newTestIdP(t)
	f.setSettings(t, map[string]string{"sso_provider": "saml", "sso_entity_id": idpEntity, "sso_login_url": idpSSO, "sso_certificate": idp.certPEM()})
	unsolicited := idp.response(t, idp.defaults(""))
	wantSignedIn(t, postForm(h, "/escalated/sso/saml/acs", url.Values{"SAMLResponse": {unsolicited}}), host, uiAgent, "IdP-initiated, allowed")
	wantRefused(t, postForm(h, "/escalated/sso/saml/acs", url.Values{"SAMLResponse": {unsolicited}}), host, "IdP-initiated replay")
}

// ------------------------------------------------------------------ OAuth

type oauthIdP struct {
	srv       *httptest.Server
	challenge string
	verifier  string
	profile   map[string]any
	fail      bool
}

func newOAuthIdP(t *testing.T) *oauthIdP {
	t.Helper()
	idp := &oauthIdP{profile: map[string]any{"email": "Ada@Example.com", "name": "Ada", "email_verified": true}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		idp.verifier = r.PostForm.Get("code_verifier")
		sum := sha256.Sum256([]byte(idp.verifier))
		if idp.fail || r.PostForm.Get("code") != "the-code" || base64.RawURLEncoding.EncodeToString(sum[:]) != idp.challenge {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"Bearer","expires_in":3600}`))
	})
	mux.HandleFunc("GET /userinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(idp.profile)
	})
	idp.srv = httptest.NewServer(mux)
	t.Cleanup(idp.srv.Close)
	return idp
}

func TestSSOOAuth(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			idp := newOAuthIdP(t)
			f, host := newSSOFixture(t, true, func(cfg *escalated.Config) { cfg.Access.SSOOptions.HTTPClient = idp.srv.Client() })
			h := f.mounts()[name]
			f.setSettings(t, map[string]string{
				"sso_provider": "oauth", "sso_oauth_authorize_url": idp.srv.URL + "/authorize", "sso_oauth_token_url": idp.srv.URL + "/token",
				"sso_oauth_userinfo_url": idp.srv.URL + "/userinfo", "sso_oauth_client_id": "client", "sso_oauth_client_secret": "secret",
			})
			start := func() (string, *http.Cookie) {
				rec := get(h, "/escalated/sso/login")
				if rec.Code != http.StatusFound {
					t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
				}
				loc, _ := url.Parse(rec.Header().Get("Location"))
				q := loc.Query()
				if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("state") == "" {
					t.Fatalf("authorize URL lacks state or PKCE: %s", loc)
				}
				if q.Get("redirect_uri") != ssoBase+"/escalated/sso/oauth/callback" {
					t.Errorf("redirect_uri %q", q.Get("redirect_uri"))
				}
				idp.challenge = q.Get("code_challenge")
				var cookie *http.Cookie
				for _, c := range rec.Result().Cookies() {
					if c.Name == "escalated_sso_oauth" {
						cookie = c
					}
				}
				if cookie == nil || !cookie.HttpOnly {
					t.Fatal("no HttpOnly state cookie")
				}
				return q.Get("state"), cookie
			}

			state, cookie := start()
			rec := get(h, "/escalated/sso/oauth/callback?code=the-code&state="+url.QueryEscape(state), cookie)
			wantSignedIn(t, rec, host, uiAgent, "matching state")
			if idp.verifier == "" {
				t.Error("no PKCE verifier was sent")
			}

			state, cookie = start()
			wantRefused(t, get(h, "/escalated/sso/oauth/callback?code=the-code&state="+url.QueryEscape(state)), host, "missing state cookie")
			wantRefused(t, get(h, "/escalated/sso/oauth/callback?code=the-code", cookie), host, "missing state")
			_, cookie = start()
			wantRefused(t, get(h, "/escalated/sso/oauth/callback?code=the-code&state=other", cookie), host, "mismatched state")
			state, cookie = start()
			wantRefused(t, get(h, "/escalated/sso/oauth/callback?error=access_denied&state="+url.QueryEscape(state), cookie), host, "IdP error")
			state, cookie = start()
			idp.fail = true
			wantRefused(t, get(h, "/escalated/sso/oauth/callback?code=the-code&state="+url.QueryEscape(state), cookie), host, "failed exchange")
			idp.fail = false
			state, cookie = start()
			idp.profile["email_verified"] = false
			wantRefused(t, get(h, "/escalated/sso/oauth/callback?code=the-code&state="+url.QueryEscape(state), cookie), host, "unverified email")
		})
	}
}

// -------------------------------------------------------------------- JWT

const jwtSecret = "jwt-shared-secret-for-tests"

func jwtToken(t *testing.T, method jwt.SigningMethod, key any, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(method, claims)
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func goodClaims() jwt.MapClaims {
	return jwt.MapClaims{"email": "ada@example.com", "name": "Ada", "aud": ssoBase, "exp": time.Now().Add(5 * time.Minute).Unix(), "iat": time.Now().Unix(), "jti": "j" + randomHex()}
}

func TestSSOJWT(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f, host := newSSOFixture(t, true)
			h := f.mounts()[name]
			f.setSettings(t, map[string]string{"sso_provider": "jwt", "sso_url": "https://idp.example.com/login", "sso_jwt_secret": jwtSecret, "sso_jwt_algorithm": "HS256"})
			callback := func(token string) *httptest.ResponseRecorder {
				return get(h, "/escalated/sso/jwt/callback?jwt="+url.QueryEscape(token))
			}

			rec := get(h, "/escalated/sso/login")
			if rec.Code != http.StatusFound || rec.Header().Get("Location") != "https://idp.example.com/login?return_to="+url.QueryEscape(ssoBase+"/escalated/sso/jwt/callback") {
				t.Errorf("login: %d %q", rec.Code, rec.Header().Get("Location"))
			}

			valid := goodClaims()
			token := jwtToken(t, jwt.SigningMethodHS256, []byte(jwtSecret), valid)
			wantSignedIn(t, callback(token), host, uiAgent, "valid token")
			wantRefused(t, callback(token), host, "reused jti")
			// POST works too.
			wantSignedIn(t, postForm(h, "/escalated/sso/jwt/callback", url.Values{"jwt": {jwtToken(t, jwt.SigningMethodHS256, []byte(jwtSecret), goodClaims())}}), host, uiAgent, "POST")

			none := jwt.NewWithClaims(jwt.SigningMethodNone, goodClaims())
			noneToken, _ := none.SignedString(jwt.UnsafeAllowNoneSignatureType)
			without := func(key string) jwt.MapClaims { c := goodClaims(); delete(c, key); return c }
			with := func(key string, v any) jwt.MapClaims { c := goodClaims(); c[key] = v; return c }
			cases := map[string]string{
				"bad signature":   jwtToken(t, jwt.SigningMethodHS256, []byte("another-secret"), goodClaims()),
				"alg none":        noneToken,
				"wrong algorithm": jwtToken(t, jwt.SigningMethodHS512, []byte(jwtSecret), goodClaims()),
				"missing exp":     jwtToken(t, jwt.SigningMethodHS256, []byte(jwtSecret), without("exp")),
				"past exp":        jwtToken(t, jwt.SigningMethodHS256, []byte(jwtSecret), with("exp", time.Now().Add(-10*time.Minute).Unix())),
				"future nbf":      jwtToken(t, jwt.SigningMethodHS256, []byte(jwtSecret), with("nbf", time.Now().Add(10*time.Minute).Unix())),
				"wrong aud":       jwtToken(t, jwt.SigningMethodHS256, []byte(jwtSecret), with("aud", "https://other.example.com")),
				"missing jti":     jwtToken(t, jwt.SigningMethodHS256, []byte(jwtSecret), without("jti")),
				"no email":        jwtToken(t, jwt.SigningMethodHS256, []byte(jwtSecret), without("email")),
			}
			for what, tok := range cases {
				wantRefused(t, callback(tok), host, what)
			}
		})
	}
}

func TestSSOJWTRSAAndAlgorithmSwitching(t *testing.T) {
	f, host := newSSOFixture(t, true)
	h := f.mounts()["chi"]
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	pubDER, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	pubPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}))
	f.setSettings(t, map[string]string{"sso_provider": "jwt", "sso_url": "https://idp.example.com/login", "sso_jwt_secret": pubPEM, "sso_jwt_algorithm": "RS256"})
	callback := func(token string) *httptest.ResponseRecorder {
		return get(h, "/escalated/sso/jwt/callback?jwt="+url.QueryEscape(token))
	}
	wantSignedIn(t, callback(jwtToken(t, jwt.SigningMethodRS256, key, goodClaims())), host, uiAgent, "RS256")
	// HS256 signed with the public key as the secret: the classic switch.
	wantRefused(t, callback(jwtToken(t, jwt.SigningMethodHS256, []byte(pubPEM), goodClaims())), host, "HS256 with the public key")
}

// ------------------------------------------------------- users and roles

func TestSSOUnknownEmailsAndRoleMapping(t *testing.T) {
	on, off := true, false
	signInAs := func(t *testing.T, f *uiFixture, claims jwt.MapClaims) *httptest.ResponseRecorder {
		f.setSettings(t, map[string]string{"sso_provider": "jwt", "sso_url": "https://idp.example.com/login", "sso_jwt_secret": jwtSecret})
		return get(f.mounts()["chi"], "/escalated/sso/jwt/callback?jwt="+url.QueryEscape(jwtToken(t, jwt.SigningMethodHS256, []byte(jwtSecret), claims)))
	}
	newcomer := func() jwt.MapClaims {
		c := goodClaims()
		c["email"] = "new@example.com"
		c["name"] = "Nia New"
		return c
	}

	t.Run("create off refuses", func(t *testing.T) {
		f, host := newSSOFixture(t, true)
		wantRefused(t, signInAs(t, f, newcomer()), host, "unknown email")
		if len(host.created) != 0 {
			t.Error("a user was created")
		}
	})
	t.Run("create on creates", func(t *testing.T) {
		f, host := newSSOFixture(t, true, func(cfg *escalated.Config) { cfg.Access.SSOOptions.CreateUsers = &on })
		wantSignedIn(t, signInAs(t, f, newcomer()), host, "950", "unknown email, create on")
		if len(host.created) != 1 || host.created[0] != "new@example.com|Nia New" {
			t.Errorf("created %v", host.created)
		}
	})
	for _, c := range []struct {
		name        string
		roles, sync bool
		want        bool
	}{
		{"roles on, sync on", true, true, true},
		{"roles on, sync off", true, false, false},
		{"roles off, sync on", false, true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			f, host := newSSOFixture(t, true, func(cfg *escalated.Config) {
				cfg.Access.RolesPermissions = map[bool]*bool{true: &on, false: &off}[c.roles]
				cfg.Access.SSOOptions.SyncRoles = map[bool]*bool{true: &on, false: &off}[c.sync]
			})
			if err := f.esc.Roles().Seed(context.Background()); err != nil {
				t.Fatal(err)
			}
			claims := goodClaims()
			claims["role"] = "light_agent"
			wantSignedIn(t, signInAs(t, f, claims), host, uiAgent, c.name)
			slugs, err := f.esc.Roles().UserRoleSlugs(context.Background(), []string{uiAgent})
			if err != nil {
				t.Fatal(err)
			}
			got := len(slugs[uiAgent]) == 1 && slugs[uiAgent][0] == "light_agent"
			if got != c.want {
				t.Errorf("roles %v, want light_agent assigned: %v", slugs[uiAgent], c.want)
			}
			// An unknown role changes nothing.
			claims = goodClaims()
			claims["role"] = "no-such-role"
			signInAs(t, f, claims)
			after, _ := f.esc.Roles().UserRoleSlugs(context.Background(), []string{uiAgent})
			if len(after[uiAgent]) != len(slugs[uiAgent]) {
				t.Errorf("an unknown role changed the roles: %v", after[uiAgent])
			}
		})
	}
}
