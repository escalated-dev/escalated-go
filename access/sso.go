package access

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/beevik/etree"
	"github.com/golang-jwt/jwt/v5"
	saml2 "github.com/russellhaering/gosaml2"
	"github.com/russellhaering/gosaml2/types"
	dsig "github.com/russellhaering/goxmldsig"
	"golang.org/x/oauth2"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
)

// SSO providers an admin picks in Admin > Settings > SSO (sso_provider).
const (
	ProviderNone  = "none"
	ProviderSAML  = "saml"
	ProviderOAuth = "oauth"
	ProviderJWT   = "jwt"
)

// SAMLRequestTTL is how long an AuthnRequest ID stays answerable.
const SAMLRequestTTL = 10 * time.Minute

// SSOSettingDefaults are the runtime SSO settings (the settings table) and
// their defaults, as escalated-laravel's SsoService.
var SSOSettingDefaults = map[string]string{
	"sso_provider":            ProviderNone,
	"sso_entity_id":           "",
	"sso_url":                 "",
	"sso_login_url":           "",
	"sso_logout_url":          "",
	"sso_metadata_url":        "",
	"sso_certificate":         "",
	"sso_attr_email":          "email",
	"sso_attr_name":           "name",
	"sso_attr_role":           "role",
	"sso_jwt_secret":          "",
	"sso_jwt_algorithm":       "HS256",
	"sso_oauth_authorize_url": "",
	"sso_oauth_token_url":     "",
	"sso_oauth_userinfo_url":  "",
	"sso_oauth_client_id":     "",
	"sso_oauth_client_secret": "",
	"sso_oauth_scopes":        "openid profile email",
}

// SSOSecretSettings are the stored secrets the settings page sees masked.
var SSOSecretSettings = []string{"sso_jwt_secret", "sso_oauth_client_secret"}

// Identity is who an identity provider says signed in.
type Identity struct {
	Email string
	Name  string
	Role  string
}

// Refusal is an SSO sign-in refused. Its message is for the log, never the
// user.
type Refusal struct{ Reason string }

func (r *Refusal) Error() string { return r.Reason }

func refuse(format string, args ...any) error { return &Refusal{Reason: fmt.Sprintf(format, args...)} }

// Settings is the runtime settings store the SSO settings live in.
type Settings interface {
	GetSetting(ctx context.Context, key string) (string, error)
}

// SSO signs people in through the provider chosen in Admin > Settings > SSO,
// leaving the crypto to maintained libraries: russellhaering/gosaml2 and
// goxmldsig (SAML 2.0), golang.org/x/oauth2 (OAuth 2 / OIDC with PKCE) and
// golang-jwt/jwt (JWT).
type SSO struct {
	DB       *sql.DB
	Prefix   string
	Settings Settings
	// Endpoints are the absolute URLs of the sign-in routes.
	Endpoints SSOEndpoints
	// SPEntityID is the SAML SP entity ID (default Endpoints.Metadata).
	SPEntityID string
	// AllowIdPInitiated accepts SAML responses that answer no request.
	AllowIdPInitiated bool
	// JWTAudience is the aud a JWT must carry.
	JWTAudience string
	// ClockSkew is the drift allowed on SAML and JWT validity windows.
	ClockSkew time.Duration
	// HTTPClient makes the OAuth token and userinfo requests. Nil is
	// http.DefaultClient.
	HTTPClient *http.Client
	// Now is the clock (tests). Nil is time.Now.
	Now func() time.Time
}

// SSOEndpoints are the URLs an admin gives the identity provider.
type SSOEndpoints struct {
	Login         string `json:"login"`
	SPEntityID    string `json:"sp_entity_id"`
	Metadata      string `json:"metadata"`
	ACS           string `json:"acs"`
	OAuthRedirect string `json:"oauth_redirect"`
	JWTCallback   string `json:"jwt_callback"`
}

func (s *SSO) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Config reads the SSO settings with their defaults. The legacy sso_url
// fills sso_login_url, and the OAuth authorize URL or SAML metadata URL for
// that provider, when those are empty.
func (s *SSO) Config(ctx context.Context) (map[string]string, error) {
	out := map[string]string{}
	for key, def := range SSOSettingDefaults {
		v, err := s.Settings.GetSetting(ctx, key)
		if err != nil {
			return nil, err
		}
		if v == "" {
			v = def
		}
		out[key] = v
	}
	if legacy := strings.TrimSpace(out["sso_url"]); legacy != "" {
		if out["sso_login_url"] == "" {
			out["sso_login_url"] = legacy
		}
		switch out["sso_provider"] {
		case ProviderOAuth:
			if out["sso_oauth_authorize_url"] == "" {
				out["sso_oauth_authorize_url"] = legacy
			}
		case ProviderSAML:
			if out["sso_metadata_url"] == "" {
				out["sso_metadata_url"] = legacy
			}
		}
	}
	return out, nil
}

// Provider is the chosen provider, or ProviderNone.
func (s *SSO) Provider(ctx context.Context) string {
	v, err := s.Settings.GetSetting(ctx, "sso_provider")
	if err != nil {
		return ProviderNone
	}
	switch v {
	case ProviderSAML, ProviderOAuth, ProviderJWT:
		return v
	}
	return ProviderNone
}

// EndpointList is the endpoints with the SP entity ID filled in.
func (s *SSO) EndpointList() SSOEndpoints {
	e := s.Endpoints
	e.SPEntityID = s.spEntityID()
	return e
}

func (s *SSO) spEntityID() string {
	if s.SPEntityID != "" {
		return s.SPEntityID
	}
	return s.Endpoints.Metadata
}

// ------------------------------------------------------------------ SAML

func (s *SSO) serviceProvider(cfg map[string]string, spOnly bool) (*saml2.SAMLServiceProvider, error) {
	certificate := strings.TrimSpace(cfg["sso_certificate"])
	if !spOnly && certificate == "" {
		return nil, refuse("SAML refused: no IdP certificate is configured")
	}
	store := &dsig.MemoryX509CertificateStore{}
	if certificate != "" {
		cert, err := parseCertificate(certificate)
		if err != nil {
			return nil, refuse("SAML refused: the IdP certificate cannot be read: %v", err)
		}
		store.Roots = []*x509.Certificate{cert}
	}
	return &saml2.SAMLServiceProvider{
		IdentityProviderSSOURL:      cfg["sso_login_url"],
		IdentityProviderIssuer:      cfg["sso_entity_id"],
		ServiceProviderIssuer:       s.spEntityID(),
		AssertionConsumerServiceURL: s.Endpoints.ACS,
		AudienceURI:                 s.spEntityID(),
		IDPCertificateStore:         store,
		// Validity windows are checked with ClockSkew either way (below);
		// the library's own NotOnOrAfter checks get the late side of it.
		Clock: dsig.NewFakeClockAt(s.now().Add(-s.ClockSkew)),
	}, nil
}

// parseCertificate reads a PEM certificate, or the bare base64 body IdPs
// show.
func parseCertificate(text string) (*x509.Certificate, error) {
	if block, _ := pem.Decode([]byte(text)); block != nil {
		return x509.ParseCertificate(block.Bytes)
	}
	body := strings.Join(strings.Fields(text), "")
	raw, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		return nil, err
	}
	return x509.ParseCertificate(raw)
}

// SAMLLoginURL is the IdP sign-in URL for a new AuthnRequest. Its ID is
// stored server-side (the IdP's cross-site POST back carries no SameSite=Lax
// cookie) and must come back as InResponseTo within SAMLRequestTTL.
func (s *SSO) SAMLLoginURL(ctx context.Context) (string, error) {
	cfg, err := s.Config(ctx)
	if err != nil {
		return "", err
	}
	sp, err := s.serviceProvider(cfg, false)
	if err != nil {
		return "", err
	}
	if cfg["sso_login_url"] == "" {
		return "", refuse("SAML refused: no IdP sign-in URL is configured")
	}
	sp.Clock = dsig.NewFakeClockAt(s.now())
	doc, err := sp.BuildAuthRequestDocument()
	if err != nil {
		return "", err
	}
	id := doc.Root().SelectAttrValue("ID", "")
	if err := s.remember(ctx, "saml_request", id, s.now().Add(SAMLRequestTTL)); err != nil {
		return "", err
	}
	return sp.BuildAuthURLFromDocument("", doc)
}

// SAMLMetadata is the SP metadata XML.
func (s *SSO) SAMLMetadata(ctx context.Context) ([]byte, error) {
	cfg, err := s.Config(ctx)
	if err != nil {
		return nil, err
	}
	sp, err := s.serviceProvider(cfg, true)
	if err != nil {
		return nil, err
	}
	// No SP keys: requests are not signed and encrypted assertions are
	// refused, so the descriptor carries no KeyDescriptor (gosaml2's own
	// Metadata() requires one).
	md := &types.EntityDescriptor{
		ValidUntil: s.now().UTC().Add(7 * 24 * time.Hour),
		EntityID:   sp.ServiceProviderIssuer,
		SPSSODescriptor: &types.SPSSODescriptor{
			AuthnRequestsSigned:        false,
			WantAssertionsSigned:       true,
			ProtocolSupportEnumeration: saml2.SAMLProtocolNamespace,
			AssertionConsumerServices: []types.IndexedEndpoint{{
				Binding:  saml2.BindingHttpPost,
				Location: sp.AssertionConsumerServiceURL,
				Index:    1,
			}},
		},
	}
	body, err := xml.MarshalIndent(md, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), body...), nil
}

// ValidateSAML checks a base64 SAMLResponse posted to the ACS: the IdP
// certificate is configured; exactly one assertion, not encrypted, signed by
// that certificate (keys in KeyInfo are never trusted; a signed Response
// alone is not enough); Issuer, Audience, Destination and Recipient; the
// validity windows with ClockSkew; InResponseTo names a pending AuthnRequest
// (consumed) unless IdP-initiated sign-in is allowed; and the assertion ID
// has not been accepted before.
func (s *SSO) ValidateSAML(ctx context.Context, encoded string) (Identity, error) {
	cfg, err := s.Config(ctx)
	if err != nil {
		return Identity{}, err
	}
	sp, err := s.serviceProvider(cfg, false)
	if err != nil {
		return Identity{}, err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil || len(raw) == 0 {
		return Identity{}, refuse("SAML refused: the response is not base64")
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(raw); err != nil || doc.Root() == nil {
		return Identity{}, refuse("SAML refused: malformed XML")
	}
	root := doc.Root()
	if len(findAll(root, "EncryptedAssertion")) > 0 {
		return Identity{}, refuse("SAML refused: encrypted assertions are not supported")
	}
	assertions := findAll(root, "Assertion")
	if len(assertions) != 1 || assertions[0].Parent() != root {
		return Identity{}, refuse("SAML refused: the response must carry exactly one assertion")
	}
	signed := false
	for _, child := range assertions[0].ChildElements() {
		if child.Tag == "Signature" {
			signed = true
		}
	}
	if !signed {
		return Identity{}, refuse("SAML refused: the assertion is not signed")
	}
	if strings.TrimSpace(root.SelectAttrValue("Destination", "")) == "" {
		return Identity{}, refuse("SAML refused: the response names no Destination")
	}

	requestID := strings.TrimSpace(root.SelectAttrValue("InResponseTo", ""))
	if requestID == "" {
		if !s.AllowIdPInitiated {
			return Identity{}, refuse("SAML refused: unsolicited response (no InResponseTo) and IdP-initiated sign-in is off")
		}
	} else {
		ok, err := s.consume(ctx, "saml_request", requestID)
		if err != nil {
			return Identity{}, err
		}
		if !ok {
			return Identity{}, refuse("SAML refused: InResponseTo [%s] is not a pending request", requestID)
		}
	}

	response, err := sp.ValidateEncodedResponse(strings.TrimSpace(encoded))
	if err != nil {
		return Identity{}, refuse("SAML refused: %v", err)
	}
	if len(response.Assertions) != 1 || !response.Assertions[0].SignatureValidated && !response.SignatureValidated {
		return Identity{}, refuse("SAML refused: no verified assertion")
	}
	assertion := response.Assertions[0]
	if assertion.Subject == nil || assertion.Subject.SubjectConfirmation == nil || assertion.Subject.SubjectConfirmation.SubjectConfirmationData == nil {
		return Identity{}, refuse("SAML refused: no bearer subject confirmation")
	}
	if got := assertion.Subject.SubjectConfirmation.SubjectConfirmationData.InResponseTo; got != requestID {
		return Identity{}, refuse("SAML refused: the subject confirmation answers [%s], not [%s]", got, requestID)
	}
	notOnOrAfter, err := s.checkConditions(assertion.Conditions, sp.AudienceURI)
	if err != nil {
		return Identity{}, err
	}

	assertionID := assertions[0].SelectAttrValue("ID", "")
	if assertionID == "" {
		return Identity{}, refuse("SAML refused: the assertion has no ID")
	}
	claimed, err := s.claim(ctx, "saml_assertion", assertionID, notOnOrAfter.Add(s.ClockSkew))
	if err != nil {
		return Identity{}, err
	}
	if !claimed {
		return Identity{}, refuse("SAML refused: assertion [%s] was already used", assertionID)
	}

	attribute := func(name string) string {
		if assertion.AttributeStatement == nil || name == "" {
			return ""
		}
		for _, a := range assertion.AttributeStatement.Attributes {
			if a.Name == name || a.FriendlyName == name {
				for _, v := range a.Values {
					if t := strings.TrimSpace(v.Value); t != "" {
						return t
					}
				}
			}
		}
		return ""
	}
	email := attribute(cfg["sso_attr_email"])
	if email == "" && assertion.Subject.NameID != nil {
		email = strings.TrimSpace(assertion.Subject.NameID.Value)
	}
	return identity(email, attribute(cfg["sso_attr_name"]), attribute(cfg["sso_attr_role"]))
}

// checkConditions requires Conditions with a validity window that holds
// (with ClockSkew) and an AudienceRestriction naming the SP.
func (s *SSO) checkConditions(c *types.Conditions, audience string) (time.Time, error) {
	if c == nil {
		return time.Time{}, refuse("SAML refused: the assertion has no Conditions")
	}
	now := s.now()
	notBefore, err1 := time.Parse(time.RFC3339, c.NotBefore)
	notOnOrAfter, err2 := time.Parse(time.RFC3339, c.NotOnOrAfter)
	if err1 != nil || err2 != nil {
		return time.Time{}, refuse("SAML refused: the assertion's validity window is missing or malformed")
	}
	if now.Add(s.ClockSkew).Before(notBefore) || !now.Add(-s.ClockSkew).Before(notOnOrAfter) {
		return time.Time{}, refuse("SAML refused: the assertion is not valid now (expired or not yet valid)")
	}
	if len(c.AudienceRestrictions) == 0 {
		return time.Time{}, refuse("SAML refused: the assertion names no audience")
	}
	for _, restriction := range c.AudienceRestrictions {
		matched := false
		for _, a := range restriction.Audiences {
			if a.Value == audience {
				matched = true
			}
		}
		if !matched {
			return time.Time{}, refuse("SAML refused: the audience does not include [%s]", audience)
		}
	}
	return notOnOrAfter, nil
}

func findAll(el *etree.Element, tag string) []*etree.Element {
	var out []*etree.Element
	for _, child := range el.ChildElements() {
		if child.Tag == tag {
			out = append(out, child)
		}
		out = append(out, findAll(child, tag)...)
	}
	return out
}

// ----------------------------------------------------------------- OAuth

func (s *SSO) oauthConfig(cfg map[string]string) *oauth2.Config {
	scopes := strings.FieldsFunc(cfg["sso_oauth_scopes"], func(r rune) bool { return r == ' ' || r == ',' || r == '\n' || r == '\t' })
	return &oauth2.Config{
		ClientID:     cfg["sso_oauth_client_id"],
		ClientSecret: cfg["sso_oauth_client_secret"],
		Endpoint: oauth2.Endpoint{
			AuthURL:  cfg["sso_oauth_authorize_url"],
			TokenURL: cfg["sso_oauth_token_url"],
		},
		RedirectURL: s.Endpoints.OAuthRedirect,
		Scopes:      scopes,
	}
}

// OAuthStart returns the authorization URL with a new single-use state and
// PKCE (S256) challenge, and the state and verifier to keep for the
// callback.
func (s *SSO) OAuthStart(ctx context.Context) (authURL, state, verifier string, err error) {
	cfg, err := s.Config(ctx)
	if err != nil {
		return "", "", "", err
	}
	if cfg["sso_oauth_authorize_url"] == "" || cfg["sso_oauth_token_url"] == "" {
		return "", "", "", refuse("OAuth refused: the authorize and token URLs are not configured")
	}
	stateBytes := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, stateBytes); err != nil {
		return "", "", "", err
	}
	state = base64.RawURLEncoding.EncodeToString(stateBytes)
	verifier = oauth2.GenerateVerifier()
	return s.oauthConfig(cfg).AuthCodeURL(state, oauth2.S256ChallengeOption(verifier)), state, verifier, nil
}

// OAuthIdentity exchanges the authorization code (with the PKCE verifier)
// server to server and reads the profile from the userinfo endpoint.
func (s *SSO) OAuthIdentity(ctx context.Context, code, verifier string) (Identity, error) {
	cfg, err := s.Config(ctx)
	if err != nil {
		return Identity{}, err
	}
	client := s.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, client)
	oc := s.oauthConfig(cfg)
	token, err := oc.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Identity{}, refuse("OAuth refused: the token exchange failed: %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg["sso_oauth_userinfo_url"], nil)
	if err != nil {
		return Identity{}, refuse("OAuth refused: the userinfo URL is invalid: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	res, err := oc.Client(ctx, token).Do(req)
	if err != nil {
		return Identity{}, refuse("OAuth refused: the userinfo request failed: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Identity{}, refuse("OAuth refused: userinfo answered %d", res.StatusCode)
	}
	var profile map[string]any
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&profile); err != nil {
		return Identity{}, refuse("OAuth refused: userinfo is not JSON: %v", err)
	}
	if v, ok := profile["email_verified"]; ok {
		switch v {
		case false, "false", float64(0), "0":
			return Identity{}, refuse("OAuth refused: the provider says the email is not verified")
		}
	}
	return identity(claim(profile, cfg["sso_attr_email"], "email"), claim(profile, cfg["sso_attr_name"], "name"), claim(profile, cfg["sso_attr_role"], ""))
}

// claim reads a (dotted) claim, else the fallback claim.
func claim(m map[string]any, name, fallback string) string {
	for _, key := range []string{name, fallback} {
		if key == "" {
			continue
		}
		var v any = m
		for _, part := range strings.Split(key, ".") {
			obj, ok := v.(map[string]any)
			if !ok {
				v = nil
				break
			}
			v = obj[part]
		}
		switch val := v.(type) {
		case string:
			if t := strings.TrimSpace(val); t != "" {
				return t
			}
		case float64:
			return strconv.FormatFloat(val, 'f', -1, 64)
		}
	}
	return ""
}

// ------------------------------------------------------------------- JWT

// JWTLoginURL is the IdP's sign-in URL with return_to set to the callback.
func (s *SSO) JWTLoginURL(ctx context.Context) (string, error) {
	cfg, err := s.Config(ctx)
	if err != nil {
		return "", err
	}
	login := strings.TrimSpace(cfg["sso_login_url"])
	if login == "" {
		login = strings.TrimSpace(cfg["sso_url"])
	}
	if login == "" {
		return "", refuse("JWT sign-in has no login URL configured")
	}
	sep := "?"
	if strings.Contains(login, "?") {
		sep = "&"
	}
	return login + sep + url.Values{"return_to": {s.Endpoints.JWTCallback}}.Encode(), nil
}

// ValidateJWT checks a token handed back by the identity provider: its alg
// must be the configured one (none and algorithm switching are refused), exp
// is required, nbf/iat are honoured with ClockSkew, aud must name
// JWTAudience, and jti is required and single-use.
func (s *SSO) ValidateJWT(ctx context.Context, token string) (Identity, error) {
	cfg, err := s.Config(ctx)
	if err != nil {
		return Identity{}, err
	}
	algorithm := cfg["sso_jwt_algorithm"]
	secret := cfg["sso_jwt_secret"]
	var key any
	switch algorithm {
	case "HS256", "HS384", "HS512":
		key = []byte(secret)
	case "RS256", "RS384", "RS512":
		pub, err := jwt.ParseRSAPublicKeyFromPEM([]byte(secret))
		if err != nil {
			return Identity{}, refuse("JWT refused: the configured public key cannot be read: %v", err)
		}
		key = (*rsa.PublicKey)(pub)
	default:
		return Identity{}, refuse("JWT refused: unsupported algorithm %s", algorithm)
	}
	if secret == "" {
		return Identity{}, refuse("JWT refused: no secret or public key is configured")
	}
	if s.JWTAudience == "" {
		return Identity{}, refuse("JWT refused: no audience is configured")
	}
	claims := jwt.MapClaims{}
	_, err = jwt.ParseWithClaims(strings.TrimSpace(token), claims, func(*jwt.Token) (any, error) { return key, nil },
		jwt.WithValidMethods([]string{algorithm}),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithAudience(s.JWTAudience),
		jwt.WithLeeway(s.ClockSkew),
		jwt.WithTimeFunc(s.now),
	)
	if err != nil {
		return Identity{}, refuse("JWT refused: %v", err)
	}
	exp, err := claims.GetExpirationTime()
	if err != nil || exp == nil {
		return Identity{}, refuse("JWT refused: no exp claim")
	}
	jti, _ := claims["jti"].(string)
	if strings.TrimSpace(jti) == "" {
		return Identity{}, refuse("JWT refused: no jti claim")
	}
	claimed, err := s.claim(ctx, "jwt", jti, exp.Add(s.ClockSkew))
	if err != nil {
		return Identity{}, err
	}
	if !claimed {
		return Identity{}, refuse("JWT refused: jti [%s] was already used", jti)
	}
	m := map[string]any(claims)
	return identity(claim(m, cfg["sso_attr_email"], "email"), claim(m, cfg["sso_attr_name"], "name"), claim(m, cfg["sso_attr_role"], ""))
}

func identity(email, name, role string) (Identity, error) {
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return Identity{}, refuse("SSO refused: no valid email in the identity")
	}
	return Identity{Email: email, Name: name, Role: role}, nil
}

// ----------------------------------------------------------- message IDs

func (s *SSO) table() string { return s.Prefix + "sso_message_ids" }

func (s *SSO) q(query string) string { return sqldialect.Rebind(s.DB, query) }

func (s *SSO) prune(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, s.q("DELETE FROM "+s.table()+" WHERE expires_at <= ?"), s.now().Unix())
	return err
}

// remember stores an ID a later message must name (an issued request).
func (s *SSO) remember(ctx context.Context, kind, id string, expires time.Time) error {
	if err := s.prune(ctx); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, s.q("INSERT INTO "+s.table()+" (kind, message_id, expires_at) VALUES (?, ?, ?)"), kind, id, expires.Unix())
	return err
}

// consume removes an unexpired remembered ID: true only the first time.
func (s *SSO) consume(ctx context.Context, kind, id string) (bool, error) {
	res, err := s.DB.ExecContext(ctx, s.q("DELETE FROM "+s.table()+" WHERE kind = ? AND message_id = ? AND expires_at > ?"), kind, id, s.now().Unix())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// claim records an ID as used: true the first time, false on a replay (the
// unique index on kind + message_id refuses the second insert).
func (s *SSO) claim(ctx context.Context, kind, id string, expires time.Time) (bool, error) {
	if err := s.prune(ctx); err != nil {
		return false, err
	}
	var n int
	if err := s.DB.QueryRowContext(ctx, s.q("SELECT COUNT(*) FROM "+s.table()+" WHERE kind = ? AND message_id = ?"), kind, id).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	if _, err := s.DB.ExecContext(ctx, s.q("INSERT INTO "+s.table()+" (kind, message_id, expires_at) VALUES (?, ?, ?)"), kind, id, expires.Unix()); err != nil {
		// A concurrent claim of the same ID loses on the unique index.
		return false, nil //nolint:nilerr // the unique index refusing is the replay answer
	}
	return true, nil
}

// IsRefusal reports whether err is a refused sign-in (as opposed to a
// failure of this server).
func IsRefusal(err error) bool {
	var r *Refusal
	return errors.As(err, &r)
}

// SealState seals the OAuth state and PKCE verifier for the callback cookie
// (AES-256-GCM under a key derived from the access secret), valid until
// expires.
func SealState(key []byte, state, verifier string, expires time.Time) (string, error) {
	raw, _ := json.Marshal(map[string]any{"s": state, "v": verifier, "e": expires.Unix()})
	t := &TwoFactor{Key: append([]byte("sso state\x00"), key...)}
	return t.seal(raw)
}

// OpenState opens a SealState value; ok is false when it is not one of
// ours or has expired.
func OpenState(key []byte, sealed string, now time.Time) (state, verifier string, ok bool) {
	t := &TwoFactor{Key: append([]byte("sso state\x00"), key...)}
	raw, err := t.open(sealed)
	if err != nil {
		return "", "", false
	}
	var v struct {
		S string `json:"s"`
		V string `json:"v"`
		E int64  `json:"e"`
	}
	if json.Unmarshal(raw, &v) != nil || v.S == "" || v.V == "" || now.Unix() >= v.E {
		return "", "", false
	}
	return v.S, v.V, true
}
