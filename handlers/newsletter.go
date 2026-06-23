package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/renderer"
	"github.com/escalated-dev/escalated-go/services/newsletter"
)

type NewsletterHandler struct {
	store      *newsletter.SQLStore
	renderer   renderer.Renderer
	newsRender *newsletter.Renderer
	planner    *newsletter.NewsletterPlanner
	tracker    *newsletter.NewsletterTracker
	mailer     newsletter.Mailer
	userID     func(*http.Request) models.UserID
	permission func(*http.Request, string) bool
	cfg        NewsletterHandlerConfig
}

type NewsletterHandlerConfig struct {
	Enabled            bool
	DefaultFrom        string
	DefaultReplyTo     string
	DefaultTheme       string
	TrackingEnabled    bool
	ThemesDir          string
	RateLimitPerMinute int
	BatchSize          int
}

func NewNewsletterHandler(
	store *newsletter.SQLStore,
	rend renderer.Renderer,
	newsRender *newsletter.Renderer,
	planner *newsletter.NewsletterPlanner,
	tracker *newsletter.NewsletterTracker,
	mailer newsletter.Mailer,
	userID func(*http.Request) models.UserID,
	permission func(*http.Request, string) bool,
	cfg NewsletterHandlerConfig,
) *NewsletterHandler {
	if userID == nil {
		userID = func(*http.Request) models.UserID { return "" }
	}
	return &NewsletterHandler{store: store, renderer: rend, newsRender: newsRender, planner: planner, tracker: tracker, mailer: mailer, userID: userID, permission: permission, cfg: cfg}
}

func (h *NewsletterHandler) require(w http.ResponseWriter, r *http.Request, perm string) bool {
	if h.permission != nil && !h.permission(r, perm) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return false
	}
	return true
}

func (h *NewsletterHandler) CampaignIndex(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, r, "newsletters.manage") {
		return
	}
	tab := r.URL.Query().Get("tab")
	if tab == "" {
		tab = "drafts"
	}
	statuses := []string{"draft"}
	switch tab {
	case "scheduled":
		statuses = []string{"scheduled", "sending", "paused"}
	case "sent":
		statuses = []string{"sent", "failed"}
	}
	items, err := h.store.ListNewsletters(r.Context(), statuses, 50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = h.renderer.Render(w, r, "Escalated/Admin/Newsletters/Index", map[string]any{"newsletters": items, "tab": tab})
}

func (h *NewsletterHandler) CampaignCreate(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, r, "newsletters.manage") {
		return
	}
	_ = h.renderer.Render(w, r, "Escalated/Admin/Newsletters/Compose", h.composeProps(r))
}

func (h *NewsletterHandler) CampaignStore(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, r, "newsletters.manage") {
		return
	}
	n, ok := h.validateNewsletterForm(w, r)
	if !ok {
		return
	}
	if n.Status == models.NewsletterScheduled || n.Status == models.NewsletterSending {
		if !h.require(w, r, "newsletters.send") {
			return
		}
		if h.mailer == nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"from_email": "Outbound mail is not configured."})
			return
		}
	}
	uid := h.userID(r)
	if !uid.Empty() {
		n.CreatedBy = &uid
	}
	if err := h.store.CreateNewsletter(r.Context(), n); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if n.Status == models.NewsletterSending {
		if err := h.planner.Plan(r.Context(), n); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	redirectTo(w, r, fmt.Sprintf("/admin/newsletters/%d", n.ID))
}

func (h *NewsletterHandler) CampaignPreview(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, r, "newsletters.manage") {
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	subject := stringValue(body["subject"])
	fromEmail := stringValue(body["from_email"])
	if fromEmail == "" {
		fromEmail = "preview@example.test"
	}
	theme := stringPtr(stringValue(body["theme"]))
	md := stringPtr(stringValue(body["body_markdown"]))
	n := &models.Newsletter{Subject: subject, FromEmail: fromEmail, Theme: theme, BodyMarkdown: md, Status: models.NewsletterDraft}
	name := "Preview User"
	c := &models.Contact{Email: "preview@example.test", Name: &name}
	d := &models.NewsletterDelivery{TrackingToken: "preview", EmailAtSend: c.Email}
	html, err := h.newsRender.Render(d, n, c, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"html": html})
}

func (h *NewsletterHandler) CampaignTestSend(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, r, "newsletters.send") {
		return
	}
	n, ok := h.validateNewsletterForm(w, r)
	if !ok {
		return
	}
	if h.mailer == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"from_email": "Outbound mail is not configured."})
		return
	}
	token, _ := newsletterToken()
	email := n.FromEmail
	name := "Tester"
	c := &models.Contact{Email: email, Name: &name}
	d := &models.NewsletterDelivery{TrackingToken: token, EmailAtSend: email, IsTest: true}
	htmlBody, err := h.newsRender.Render(d, n, c, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := h.mailer.SendNewsletter(r.Context(), newsletter.MailMessage{
		To: email, From: formatNewsletterFrom(n.FromEmail, n.FromName), ReplyTo: strPtrValue(n.ReplyTo),
		Subject: "[TEST] " + n.Subject, HTML: htmlBody, TestSend: true,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *NewsletterHandler) CampaignShow(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, r, "newsletters.manage") {
		return
	}
	id, err := idFromPathName(r, "newsletter")
	if err != nil {
		http.Error(w, "invalid newsletter", http.StatusBadRequest)
		return
	}
	n, err := h.store.GetNewsletter(r.Context(), id)
	if err != nil || n == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	deliveries, err := h.store.ListDeliveries(r.Context(), n.ID, r.URL.Query().Get("status"), false, 100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tab := r.URL.Query().Get("tab")
	if tab == "" {
		tab = "overview"
	}
	_ = h.renderer.Render(w, r, "Escalated/Admin/Newsletters/Show", map[string]any{"newsletter": n, "deliveries": deliveries, "topClicks": []any{}, "tab": tab})
}

func (h *NewsletterHandler) CampaignEdit(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, r, "newsletters.manage") {
		return
	}
	id, err := idFromPathName(r, "newsletter")
	if err != nil {
		http.Error(w, "invalid newsletter", http.StatusBadRequest)
		return
	}
	n, err := h.store.GetNewsletter(r.Context(), id)
	if err != nil || n == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if n.Status != models.NewsletterDraft && n.Status != models.NewsletterScheduled {
		http.Error(w, "Only drafts and scheduled newsletters can be edited", http.StatusUnprocessableEntity)
		return
	}
	props := h.composeProps(r)
	props["newsletter"] = n
	_ = h.renderer.Render(w, r, "Escalated/Admin/Newsletters/Edit", props)
}

func (h *NewsletterHandler) CampaignUpdate(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, r, "newsletters.manage") {
		return
	}
	id, err := idFromPathName(r, "newsletter")
	if err != nil {
		http.Error(w, "invalid newsletter", http.StatusBadRequest)
		return
	}
	n, ok := h.validateNewsletterForm(w, r)
	if !ok {
		return
	}
	n.ID = id
	if n.Status == models.NewsletterScheduled || n.Status == models.NewsletterSending {
		if !h.require(w, r, "newsletters.send") {
			return
		}
	}
	if err := h.store.UpdateNewsletter(r.Context(), n); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if n.Status == models.NewsletterSending {
		if err := h.planner.Plan(r.Context(), n); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	redirectTo(w, r, fmt.Sprintf("/admin/newsletters/%d", id))
}

func (h *NewsletterHandler) CampaignDestroy(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, r, "newsletters.manage") {
		return
	}
	id, err := idFromPathName(r, "newsletter")
	if err != nil {
		http.Error(w, "invalid newsletter", http.StatusBadRequest)
		return
	}
	n, err := h.store.GetNewsletter(r.Context(), id)
	if err != nil || n == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if n.Status != models.NewsletterDraft {
		http.Error(w, "Only drafts can be deleted", http.StatusUnprocessableEntity)
		return
	}
	if err := h.store.DeleteNewsletter(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	redirectTo(w, r, "/admin/newsletters")
}

func (h *NewsletterHandler) ListIndex(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, r, "newsletters.manage") {
		return
	}
	lists, err := h.store.ListLists(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rows := make([]map[string]any, 0, len(lists))
	for _, l := range lists {
		memberCount, _ := h.store.CountListMembers(r.Context(), l.ID)
		optedOut, _ := h.store.CountListOptedOut(r.Context(), l.ID)
		rows = append(rows, map[string]any{"id": l.ID, "name": l.Name, "description": l.Description, "kind": l.Kind, "filter_json": l.FilterJSON, "member_count": memberCount, "opted_out_count": optedOut})
	}
	_ = h.renderer.Render(w, r, "Escalated/Admin/Newsletters/Lists/Index", map[string]any{"lists": rows})
}

func (h *NewsletterHandler) ListCreate(w http.ResponseWriter, r *http.Request) {
	if h.require(w, r, "newsletters.manage") {
		_ = h.renderer.Render(w, r, "Escalated/Admin/Newsletters/Lists/Create", map[string]any{})
	}
}

func (h *NewsletterHandler) ListStore(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, r, "newsletters.manage") {
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	name := strings.TrimSpace(stringValue(body["name"]))
	kind := models.NewsletterListKind(stringValue(body["kind"]))
	if name == "" || (kind != models.NewsletterListStatic && kind != models.NewsletterListDynamic) {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	uid := h.userID(r)
	var createdBy *models.UserID
	if !uid.Empty() {
		createdBy = &uid
	}
	list := &models.NewsletterList{Name: name, Description: stringPtr(stringValue(body["description"])), Kind: kind, FilterJSON: mapValue(body["filter_json"]), CreatedBy: createdBy}
	if err := h.store.CreateList(r.Context(), list); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	redirectTo(w, r, fmt.Sprintf("/admin/newsletters/lists/%d", list.ID))
}

func (h *NewsletterHandler) ListShow(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, r, "newsletters.manage") {
		return
	}
	id, err := idFromPathName(r, "list")
	if err != nil {
		http.Error(w, "invalid list", http.StatusBadRequest)
		return
	}
	list, err := h.store.GetList(r.Context(), id)
	if err != nil || list == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	members, _ := h.store.ListMembers(r.Context(), id, 100)
	matchCount := 0
	if list.Kind == models.NewsletterListDynamic {
		resolver := newsletter.NewContactSegmentResolver(h.store)
		matchCount, _ = resolver.CountMatches(r.Context(), list.FilterJSON)
	}
	_ = h.renderer.Render(w, r, "Escalated/Admin/Newsletters/Lists/Show", map[string]any{"list": list, "members": members, "matchCount": matchCount})
}

func (h *NewsletterHandler) ListUpdate(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, r, "newsletters.manage") {
		return
	}
	id, err := idFromPathName(r, "list")
	if err != nil {
		http.Error(w, "invalid list", http.StatusBadRequest)
		return
	}
	list, err := h.store.GetList(r.Context(), id)
	if err != nil || list == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if v := stringValue(body["name"]); v != "" {
		list.Name = v
	}
	if _, ok := body["description"]; ok {
		list.Description = stringPtr(stringValue(body["description"]))
	}
	if _, ok := body["filter_json"]; ok {
		list.FilterJSON = mapValue(body["filter_json"])
	}
	if err := h.store.UpdateList(r.Context(), list); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	redirectTo(w, r, fmt.Sprintf("/admin/newsletters/lists/%d", id))
}

func (h *NewsletterHandler) ListDestroy(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, r, "newsletters.manage") {
		return
	}
	id, err := idFromPathName(r, "list")
	if err != nil {
		http.Error(w, "invalid list", http.StatusBadRequest)
		return
	}
	if err := h.store.DeleteList(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	redirectTo(w, r, "/admin/newsletters/lists")
}

func (h *NewsletterHandler) ListAddMember(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, r, "newsletters.manage") {
		return
	}
	id, _ := idFromPathName(r, "list")
	list, err := h.store.GetList(r.Context(), id)
	if err != nil || list == nil || list.Kind != models.NewsletterListStatic {
		http.Error(w, "Static list required", http.StatusUnprocessableEntity)
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	contactID := int64(numberValue(body["contact_id"]))
	ok, err := h.store.ContactExists(r.Context(), contactID)
	if err != nil || !ok {
		http.Error(w, "contact_id does not exist", http.StatusBadRequest)
		return
	}
	uid := h.userID(r)
	var addedBy *models.UserID
	if !uid.Empty() {
		addedBy = &uid
	}
	_ = h.store.AddMember(r.Context(), id, contactID, addedBy)
	redirectTo(w, r, fmt.Sprintf("/admin/newsletters/lists/%d", id))
}

func (h *NewsletterHandler) ListRemoveMember(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, r, "newsletters.manage") {
		return
	}
	id, _ := idFromPathName(r, "list")
	list, err := h.store.GetList(r.Context(), id)
	if err != nil || list == nil || list.Kind != models.NewsletterListStatic {
		http.Error(w, "Static list required", http.StatusUnprocessableEntity)
		return
	}
	contactID, _ := idFromPathName(r, "contactId")
	_ = h.store.RemoveMember(r.Context(), id, contactID)
	redirectTo(w, r, fmt.Sprintf("/admin/newsletters/lists/%d", id))
}

func (h *NewsletterHandler) ListImportCSV(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, r, "newsletters.manage") {
		return
	}
	id, _ := idFromPathName(r, "list")
	list, err := h.store.GetList(r.Context(), id)
	if err != nil || list == nil || list.Kind != models.NewsletterListStatic {
		http.Error(w, "Static list required", http.StatusUnprocessableEntity)
		return
	}
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "file is required", http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()
	imported := h.importCSVRows(r, id, file)
	redirectTo(w, r, fmt.Sprintf("/admin/newsletters/lists/%d?status=%s", id, url.QueryEscape(fmt.Sprintf("Imported %d contacts", imported))))
}

func (h *NewsletterHandler) TemplateIndex(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, r, "newsletters.manage") {
		return
	}
	templates, err := h.store.ListTemplates(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = h.renderer.Render(w, r, "Escalated/Admin/Newsletters/Templates/Index", map[string]any{"templates": templates})
}

func (h *NewsletterHandler) TemplateCreate(w http.ResponseWriter, r *http.Request) {
	if h.require(w, r, "newsletters.manage") {
		_ = h.renderer.Render(w, r, "Escalated/Admin/Newsletters/Templates/Create", map[string]any{"themes": h.themes()})
	}
}

func (h *NewsletterHandler) TemplateStore(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, r, "newsletters.manage") {
		return
	}
	t, ok := h.validateTemplate(w, r)
	if !ok {
		return
	}
	uid := h.userID(r)
	if !uid.Empty() {
		t.CreatedBy = &uid
	}
	if err := h.store.CreateTemplate(r.Context(), t); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	redirectTo(w, r, "/admin/newsletters/templates")
}

func (h *NewsletterHandler) TemplateShow(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, r, "newsletters.manage") {
		return
	}
	id, _ := idFromPathName(r, "template")
	t, err := h.store.GetTemplate(r.Context(), id)
	if err != nil || t == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	_ = h.renderer.Render(w, r, "Escalated/Admin/Newsletters/Templates/Show", map[string]any{"template": t, "themes": h.themes(), "isNew": false})
}

func (h *NewsletterHandler) TemplateUpdate(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, r, "newsletters.manage") {
		return
	}
	id, _ := idFromPathName(r, "template")
	t, ok := h.validateTemplate(w, r)
	if !ok {
		return
	}
	t.ID = id
	if err := h.store.UpdateTemplate(r.Context(), t); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	redirectTo(w, r, fmt.Sprintf("/admin/newsletters/templates/%d", id))
}

func (h *NewsletterHandler) TemplateDestroy(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, r, "newsletters.manage") {
		return
	}
	id, _ := idFromPathName(r, "template")
	if err := h.store.DeleteTemplate(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	redirectTo(w, r, "/admin/newsletters/templates")
}

func (h *NewsletterHandler) SettingsShow(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, r, "newsletters.manage") {
		return
	}
	settings := map[string]any{
		"default_from":          h.settingOrDefault(r, "default_from", h.cfg.DefaultFrom),
		"default_reply_to":      h.settingOrDefault(r, "default_reply_to", h.cfg.DefaultReplyTo),
		"default_theme":         h.settingOrDefault(r, "default_theme", h.cfg.DefaultTheme),
		"rate_limit_per_minute": h.settingOrDefault(r, "rate_limit_per_minute", strconv.Itoa(h.cfg.RateLimitPerMinute)),
		"batch_size":            h.settingOrDefault(r, "batch_size", strconv.Itoa(h.cfg.BatchSize)),
		"tracking_enabled":      h.settingOrDefault(r, "tracking_enabled", boolStored(h.cfg.TrackingEnabled)),
	}
	_ = h.renderer.Render(w, r, "Escalated/Admin/Newsletters/Settings", map[string]any{"settings": settings, "themes": h.themes()})
}

func (h *NewsletterHandler) SettingsUpdate(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, r, "newsletters.manage") {
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	keys := []string{"default_from", "default_reply_to", "default_theme", "rate_limit_per_minute", "batch_size", "tracking_enabled"}
	for _, key := range keys {
		_ = h.store.SetSetting(r.Context(), "newsletter."+key, fmt.Sprint(body[key]))
	}
	redirectTo(w, r, "/admin/newsletters/settings")
}

var pixelBytes = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
	0x0d, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0xfc, 0xff, 0xff, 0x3f,
	0x03, 0x00, 0x05, 0xfe, 0x02, 0xfe, 0xdc, 0xcc, 0x59, 0xe7, 0x00, 0x00,
	0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

func (h *NewsletterHandler) OpenPixel(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(pathToken(r), ".gif"), ".png"), ".jpg")
	h.tracker.RecordOpen(r.Context(), token)
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, no-store, max-age=0")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pixelBytes)
}

func (h *NewsletterHandler) Click(w http.ResponseWriter, r *http.Request) {
	dest, err := decodeTrackedURL(r.URL.Query().Get("u"))
	if err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	h.tracker.RecordClick(r.Context(), pathToken(r), dest)
	http.Redirect(w, r, dest, http.StatusFound)
}

func (h *NewsletterHandler) UnsubscribeShow(w http.ResponseWriter, r *http.Request) {
	token := pathToken(r)
	d, _ := h.store.GetDeliveryByToken(r.Context(), token)
	email := ""
	if d != nil {
		email = d.EmailAtSend
	}
	writeHTML(w, unsubscribeHTML(token, email, false))
}

func (h *NewsletterHandler) UnsubscribeStore(w http.ResponseWriter, r *http.Request) {
	ip := r.RemoteAddr
	if tooManyUnsubscribes(ip) {
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
		return
	}
	token := pathToken(r)
	d, _ := h.store.GetDeliveryByToken(r.Context(), token)
	email := ""
	if d != nil {
		email = d.EmailAtSend
		_ = h.store.UpdateContactOptOut(r.Context(), d.ContactID, time.Now())
	}
	writeHTML(w, unsubscribeHTML(token, email, true))
}

func (h *NewsletterHandler) ViewInBrowser(w http.ResponseWriter, r *http.Request) {
	token := pathToken(r)
	d, _ := h.store.GetDeliveryByToken(r.Context(), token)
	if d == nil {
		writeHTML(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Email unavailable</title></head><body><p>This email is no longer available.</p></body></html>`)
		return
	}
	n, _ := h.store.GetNewsletter(r.Context(), d.NewsletterID)
	c, _ := h.store.GetContact(r.Context(), d.ContactID)
	var tpl *models.NewsletterTemplate
	if n != nil && n.TemplateID != nil {
		tpl, _ = h.store.GetTemplate(r.Context(), *n.TemplateID)
	}
	out, err := h.newsRender.Render(d, n, c, tpl)
	if err != nil {
		writeHTML(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Email unavailable</title></head><body><p>This email is no longer available.</p></body></html>`)
		return
	}
	writeHTML(w, out)
}

func (h *NewsletterHandler) WebhookPostmark(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	token := tokenFromMessageID(stringValue(body["MessageID"]))
	switch stringValue(body["RecordType"]) {
	case "Open":
		h.tracker.RecordOpen(r.Context(), token)
	case "Click":
		h.tracker.RecordClick(r.Context(), token, stringValue(body["OriginalLink"]))
	case "Bounce":
		typ := "soft"
		if hardPostmark(stringValue(body["Type"])) {
			typ = "hard"
		}
		reason := stringValue(body["Description"])
		h.tracker.RecordBounce(r.Context(), token, typ, &reason)
	case "SpamComplaint":
		h.tracker.RecordComplaint(r.Context(), token)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *NewsletterHandler) WebhookMailgun(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	eventData := mapValue(body["event-data"])
	headers := mapValue(mapValue(eventData["message"])["headers"])
	token := tokenFromMessageID(stringValue(headers["message-id"]))
	switch stringValue(eventData["event"]) {
	case "opened":
		h.tracker.RecordOpen(r.Context(), token)
	case "clicked":
		h.tracker.RecordClick(r.Context(), token, stringValue(eventData["url"]))
	case "failed":
		typ := "soft"
		if stringValue(eventData["severity"]) == "permanent" {
			typ = "hard"
		}
		ds := mapValue(eventData["delivery-status"])
		reason := stringValue(ds["description"])
		h.tracker.RecordBounce(r.Context(), token, typ, &reason)
	case "complained":
		h.tracker.RecordComplaint(r.Context(), token)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *NewsletterHandler) WebhookSES(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	message := mapValue(body["Message"])
	if raw, ok := body["Message"].(string); ok {
		_ = json.Unmarshal([]byte(raw), &message)
	}
	if len(message) == 0 {
		message = body
	}
	mail := mapValue(message["mail"])
	token := tokenFromMessageID(stringValue(mail["messageId"]))
	switch stringValue(message["eventType"]) {
	case "Open":
		h.tracker.RecordOpen(r.Context(), token)
	case "Click":
		h.tracker.RecordClick(r.Context(), token, stringValue(mapValue(message["click"])["link"]))
	case "Bounce":
		typ := "soft"
		bounce := mapValue(message["bounce"])
		if stringValue(bounce["bounceType"]) == "Permanent" {
			typ = "hard"
		}
		reason := stringValue(bounce["bounceSubType"])
		h.tracker.RecordBounce(r.Context(), token, typ, &reason)
	case "Complaint":
		h.tracker.RecordComplaint(r.Context(), token)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *NewsletterHandler) WebhookSendgrid(w http.ResponseWriter, r *http.Request) {
	var events []map[string]any
	_ = json.NewDecoder(r.Body).Decode(&events)
	for _, event := range events {
		token := tokenFromMessageID(stringValue(event["smtp-id"]))
		if token == "" {
			token = tokenFromMessageID(stringValue(event["sg_message_id"]))
		}
		switch stringValue(event["event"]) {
		case "open":
			h.tracker.RecordOpen(r.Context(), token)
		case "click":
			h.tracker.RecordClick(r.Context(), token, stringValue(event["url"]))
		case "bounce":
			typ := "soft"
			if stringValue(event["type"]) == "blocked" {
				typ = "hard"
			}
			reason := stringValue(event["reason"])
			h.tracker.RecordBounce(r.Context(), token, typ, &reason)
		case "dropped":
			reason := stringValue(event["reason"])
			h.tracker.RecordBounce(r.Context(), token, "hard", &reason)
		case "spamreport":
			h.tracker.RecordComplaint(r.Context(), token)
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *NewsletterHandler) validateNewsletterForm(w http.ResponseWriter, r *http.Request) (*models.Newsletter, bool) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return nil, false
	}
	subject := strings.TrimSpace(stringValue(body["subject"]))
	fromEmail := strings.TrimSpace(stringValue(body["from_email"]))
	targetListID := int64(numberValue(body["target_list_id"]))
	status := models.NewsletterStatus(stringValue(body["status"]))
	if status == "" {
		status = models.NewsletterDraft
	}
	if subject == "" || !validEmail(fromEmail) || targetListID <= 0 || (status != models.NewsletterDraft && status != models.NewsletterScheduled && status != models.NewsletterSending) {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return nil, false
	}
	list, err := h.store.GetList(r.Context(), targetListID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, false
	}
	if list == nil {
		http.Error(w, "target_list_id does not exist", http.StatusBadRequest)
		return nil, false
	}
	var scheduledAt *time.Time
	if raw := stringValue(body["scheduled_at"]); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			scheduledAt = &t
		}
	}
	var templateID *int64
	if v := int64(numberValue(body["template_id"])); v > 0 {
		tpl, err := h.store.GetTemplate(r.Context(), v)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return nil, false
		}
		if tpl == nil {
			http.Error(w, "template_id does not exist", http.StatusBadRequest)
			return nil, false
		}
		templateID = &v
	}
	return &models.Newsletter{
		Subject: subject, FromEmail: fromEmail, FromName: stringPtr(stringValue(body["from_name"])),
		ReplyTo: stringPtr(stringValue(body["reply_to"])), TargetListID: targetListID, TemplateID: templateID,
		Theme: stringPtr(stringValue(body["theme"])), BodyMarkdown: stringPtr(stringValue(body["body_markdown"])),
		Status: status, ScheduledAt: scheduledAt,
	}, true
}

func (h *NewsletterHandler) validateTemplate(w http.ResponseWriter, r *http.Request) (*models.NewsletterTemplate, bool) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return nil, false
	}
	name, theme, md := stringValue(body["name"]), stringValue(body["theme"]), stringValue(body["body_markdown"])
	if name == "" || theme == "" || md == "" {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return nil, false
	}
	return &models.NewsletterTemplate{Name: name, Theme: theme, SubjectTemplate: stringPtr(stringValue(body["subject_template"])), BodyMarkdown: md, MergeFieldsSchema: mapValue(body["merge_fields_schema"])}, true
}

func (h *NewsletterHandler) composeProps(r *http.Request) map[string]any {
	lists, _ := h.store.ListLists(r.Context())
	listProps := make([]map[string]any, 0, len(lists))
	for _, l := range lists {
		c, _ := h.store.CountListMembers(r.Context(), l.ID)
		listProps = append(listProps, map[string]any{"id": l.ID, "name": l.Name, "member_count": c})
	}
	templates, _ := h.store.ListTemplates(r.Context())
	return map[string]any{
		"lists": listProps, "templates": templates, "themes": h.themes(), "mailConfigured": h.mailer != nil,
		"canSend": true, "defaultFromEmail": h.cfg.DefaultFrom, "defaultReplyTo": h.cfg.DefaultReplyTo, "defaultTheme": h.cfg.DefaultTheme,
	}
}

func (h *NewsletterHandler) themes() []string {
	dir := h.cfg.ThemesDir
	if dir == "" {
		dir = filepath.Join("templates", "newsletter_themes")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{"default", "branded"}
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".html") {
			out = append(out, strings.TrimSuffix(e.Name(), ".html"))
		}
	}
	if len(out) == 0 {
		return []string{"default", "branded"}
	}
	return out
}

func (h *NewsletterHandler) settingOrDefault(r *http.Request, key, fallback string) string {
	v, _ := h.store.GetSetting(r.Context(), "newsletter."+key)
	if v == "" {
		return fallback
	}
	return v
}

func (h *NewsletterHandler) importCSVRows(r *http.Request, listID int64, src io.Reader) int {
	reader := csv.NewReader(src)
	reader.FieldsPerRecord = -1
	imported := 0
	uid := h.userID(r)
	var addedBy *models.UserID
	if !uid.Empty() {
		addedBy = &uid
	}
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(row) == 0 || !validEmail(row[0]) {
			continue
		}
		email := models.NormalizeEmail(row[0])
		c, _ := h.store.GetContactByEmail(r.Context(), email)
		if c == nil {
			c = &models.Contact{Email: email}
			_ = h.store.CreateContact(r.Context(), c)
		}
		_ = h.store.AddMember(r.Context(), listID, c.ID, addedBy)
		imported++
	}
	return imported
}

var unsubMu sync.Mutex
var unsubAttempts = map[string]struct {
	count     int
	expiresAt time.Time
}{}

func tooManyUnsubscribes(ip string) bool {
	unsubMu.Lock()
	defer unsubMu.Unlock()
	now := time.Now()
	entry := unsubAttempts[ip]
	if entry.expiresAt.Before(now) {
		unsubAttempts[ip] = struct {
			count     int
			expiresAt time.Time
		}{count: 1, expiresAt: now.Add(time.Minute)}
		return false
	}
	entry.count++
	unsubAttempts[ip] = entry
	return entry.count > 60
}

var messageTokenRE = regexp.MustCompile(`n-\d+-([A-Za-z0-9]+)@`)
var localTokenRE = regexp.MustCompile(`^n-\d+-([A-Za-z0-9]+)$`)

func tokenFromMessageID(messageID string) string {
	if m := messageTokenRE.FindStringSubmatch(messageID); len(m) == 2 {
		return m[1]
	}
	local := strings.Split(messageID, "@")[0]
	if m := localTokenRE.FindStringSubmatch(local); len(m) == 2 {
		return m[1]
	}
	return ""
}

func decodeTrackedURL(encoded string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	dest := string(decoded)
	u, err := url.Parse(dest)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("bad url")
	}
	return dest, nil
}

func pathToken(r *http.Request) string {
	if v := r.PathValue("token"); v != "" {
		return v
	}
	path := strings.TrimRight(r.URL.Path, "/")
	return path[strings.LastIndex(path, "/")+1:]
}

func redirectTo(w http.ResponseWriter, r *http.Request, target string) {
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func writeHTML(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

func unsubscribeHTML(token, email string, confirmed bool) string {
	msg := "Confirm that you want to unsubscribe from marketing emails."
	if confirmed {
		msg = "You have been unsubscribed."
	}
	return `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Unsubscribe</title></head><body><main><h1>Unsubscribe</h1><p>` +
		html.EscapeString(msg) + `</p><p>` + html.EscapeString(email) + `</p><form method="post" action="/escalated/n/u/` +
		html.EscapeString(token) + `"><button type="submit">Unsubscribe</button></form></main></body></html>`
}

func validEmail(s string) bool {
	s = strings.TrimSpace(s)
	return strings.Contains(s, "@") && strings.Contains(s[strings.Index(s, "@")+1:], ".") && len(s) <= 320
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func numberValue(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		i, _ := strconv.Atoi(n)
		return i
	default:
		return 0
	}
}

func mapValue(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	if s, ok := v.(string); ok && s != "" {
		var out map[string]any
		_ = json.Unmarshal([]byte(s), &out)
		return out
	}
	return nil
}

func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func strPtrValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func boolStored(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func hardPostmark(t string) bool {
	return t == "HardBounce" || t == "BadEmailAddress" || t == "BlockedRecipient"
}

func formatNewsletterFrom(email string, name *string) string {
	if name != nil && *name != "" {
		return fmt.Sprintf(`"%s" <%s>`, *name, email)
	}
	return email
}

func newsletterToken() (string, error) {
	var b strings.Builder
	for len(b.String()) < 40 {
		t, err := randomFragment()
		if err != nil {
			return "", err
		}
		b.WriteString(t)
	}
	return b.String()[:40], nil
}

func randomFragment() (string, error) {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
