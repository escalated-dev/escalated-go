package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/renderer"
	"github.com/escalated-dev/escalated-go/services"
)

// ImportHandler serves the admin Import wizard, as escalated-laravel's
// Admin\ImportController, on its paths and with its props:
//
//	GET  /admin/import                            Index     adapters, recent jobs
//	GET  /admin/import/{platform}/connect         Connect   credential form
//	POST /admin/import/{platform}/test-connection JSON {success[, error]}
//	GET  /admin/import/{platform}/mapping         Mapping   defaults + source fields
//	POST /admin/import/{platform}/review          Review    echo of the choices; writes nothing
//	POST /admin/import/{platform}/start           creates the job and queues the run
//	GET  /admin/import/progress/{job}             Progress
//	GET  /admin/import/status/{job}               JSON poll
//	POST /admin/import/{pause,resume,cancel}/{job}
//
// The two-segment paths overlap ("progress/{job}" against
// "{platform}/connect"), which ServeMux refuses to register side by side, so
// each method has one route, /admin/import/{a}/{b}, dispatched here.
//
// Tested credentials wait, sealed, in an HttpOnly cookie scoped to the import
// paths (Escalated has no session) until the import starts, and are never
// sent to the browser in a page.
type ImportHandler struct {
	Svc         *services.ImportService
	Renderer    renderer.Renderer
	RoutePrefix string
	UserID      func(r *http.Request) models.UserID
}

func (h *ImportHandler) base() string {
	prefix := h.RoutePrefix
	if prefix == "" {
		prefix = "/escalated"
	}
	return prefix + "/admin/import"
}

func (h *ImportHandler) ready(w http.ResponseWriter) bool {
	if h == nil || h.Svc == nil {
		http.Error(w, "imports are not configured", http.StatusNotFound)
		return false
	}
	return true
}

// Index renders Escalated/Admin/Import/Index.
func (h *ImportHandler) Index(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) {
		return
	}
	jobs, err := h.Svc.Recent(r.Context(), 20)
	if err != nil {
		http.Error(w, "could not load the imports", http.StatusInternalServerError)
		return
	}
	adapters := make([]map[string]any, 0, len(h.Svc.Adapters))
	for _, a := range h.Svc.Adapters {
		adapters = append(adapters, map[string]any{
			"name": a.Name(), "display_name": a.DisplayName(), "credential_fields": a.CredentialFields(),
		})
	}
	rows := make([]map[string]any, 0, len(jobs))
	for _, j := range jobs {
		rows = append(rows, importJobProps(j))
	}
	_ = h.Renderer.Render(w, r, "Escalated/Admin/Import/Index", map[string]any{"adapters": adapters, "jobs": rows})
}

// Get dispatches GET /admin/import/{a}/{b}.
func (h *ImportHandler) Get(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) {
		return
	}
	a, b := pathParam(r, "a"), pathParam(r, "b")
	switch {
	case a == "progress":
		h.progress(w, r, b)
	case a == "status":
		h.status(w, r, b)
	case b == "connect":
		h.connect(w, r, a)
	case b == "mapping":
		h.mapping(w, r, a)
	default:
		http.NotFound(w, r)
	}
}

// Post dispatches POST /admin/import/{a}/{b}.
func (h *ImportHandler) Post(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) {
		return
	}
	a, b := pathParam(r, "a"), pathParam(r, "b")
	switch {
	case a == "pause" || a == "resume" || a == "cancel":
		h.control(w, r, a, b)
	case b == "test-connection":
		h.testConnection(w, r, a)
	case b == "review":
		h.review(w, r, a)
	case b == "start":
		h.start(w, r, a)
	default:
		http.NotFound(w, r)
	}
}

func (h *ImportHandler) adapter(w http.ResponseWriter, r *http.Request, platform string) services.ImportAdapter {
	a := h.Svc.Adapter(platform)
	if a == nil {
		http.Error(w, "import adapter not found: "+platform, http.StatusNotFound)
	}
	return a
}

func (h *ImportHandler) connect(w http.ResponseWriter, r *http.Request, platform string) {
	a := h.adapter(w, r, platform)
	if a == nil {
		return
	}
	_ = h.Renderer.Render(w, r, "Escalated/Admin/Import/Connect", map[string]any{
		"platform": a.Name(), "display_name": a.DisplayName(), "credential_fields": a.CredentialFields(),
	})
}

func importBody(r *http.Request) map[string]any {
	body := map[string]any{}
	_ = json.NewDecoder(r.Body).Decode(&body)
	return body
}

func (h *ImportHandler) cookieName(a services.ImportAdapter) string {
	return "escalated_import_" + strings.NewReplacer("-", "_", ".", "_").Replace(a.Name())
}

func (h *ImportHandler) setCredentials(w http.ResponseWriter, r *http.Request, a services.ImportAdapter, sealed string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: h.cookieName(a), Value: sealed, Path: h.base(), MaxAge: maxAge,
		HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode,
	})
}

func (h *ImportHandler) storedCredentials(r *http.Request, a services.ImportAdapter) map[string]string {
	c, err := r.Cookie(h.cookieName(a))
	if err != nil {
		return nil
	}
	return h.Svc.Credentials().Open(c.Value)
}

func (h *ImportHandler) testConnection(w http.ResponseWriter, r *http.Request, platform string) {
	a := h.adapter(w, r, platform)
	if a == nil {
		return
	}
	credentials := map[string]string{}
	if posted, ok := importBody(r)["credentials"].(map[string]any); ok {
		for k, v := range posted {
			if v != nil {
				credentials[k] = fmt.Sprint(v)
			} else {
				credentials[k] = ""
			}
		}
	}
	ok, err := a.TestConnection(r.Context(), credentials)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": err.Error()})
		return
	}
	if ok {
		sealed, err := h.Svc.Credentials().Seal(credentials)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": err.Error()})
			return
		}
		h.setCredentials(w, r, a, sealed, int((2 * time.Hour).Seconds()))
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"success": ok})
}

func (h *ImportHandler) toConnect(w http.ResponseWriter, r *http.Request, a services.ImportAdapter) {
	seeOther(w, r, h.base()+"/"+a.Name()+"/connect")
}

func (h *ImportHandler) mapping(w http.ResponseWriter, r *http.Request, platform string) {
	a := h.adapter(w, r, platform)
	if a == nil {
		return
	}
	credentials := h.storedCredentials(r, a)
	if credentials == nil {
		h.toConnect(w, r, a)
		return
	}
	mappings := map[string]any{}
	for _, entityType := range a.EntityTypes() {
		fields, err := a.AvailableSourceFields(r.Context(), entityType, credentials)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		if fields == nil {
			fields = []map[string]any{}
		}
		defaults := a.DefaultFieldMappings(entityType)
		if defaults == nil {
			defaults = map[string]string{}
		}
		mappings[entityType] = map[string]any{"defaults": defaults, "source_fields": fields}
	}
	_ = h.Renderer.Render(w, r, "Escalated/Admin/Import/Mapping", map[string]any{
		"platform": a.Name(), "display_name": a.DisplayName(), "entity_types": a.EntityTypes(), "mappings": mappings,
	})
}

func (h *ImportHandler) review(w http.ResponseWriter, r *http.Request, platform string) {
	body := importBody(r)
	mappings, _ := body["field_mappings"].(map[string]any)
	if mappings == nil {
		mappings = map[string]any{}
	}
	types := []string{}
	if list, ok := body["entity_types"].([]any); ok {
		for _, t := range list {
			types = append(types, fmt.Sprint(t))
		}
	}
	_ = h.Renderer.Render(w, r, "Escalated/Admin/Import/Review", map[string]any{
		"platform": platform, "field_mappings": mappings, "entity_types": types,
	})
}

func (h *ImportHandler) start(w http.ResponseWriter, r *http.Request, platform string) {
	a := h.adapter(w, r, platform)
	if a == nil {
		return
	}
	credentials := h.storedCredentials(r, a)
	if credentials == nil {
		h.toConnect(w, r, a)
		return
	}
	mappings, _ := importBody(r)["field_mappings"].(map[string]any)
	job := &services.ImportJob{Platform: a.Name(), Status: "mapping", FieldMappings: mappings}
	if h.UserID != nil {
		if uid := h.UserID(r); uid != "" {
			job.UserID = &uid
		}
	}
	if err := h.Svc.Create(r.Context(), job, credentials); err != nil {
		http.Error(w, "could not start the import: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// The job holds them now, sealed.
	h.setCredentials(w, r, a, "", -1)
	h.Svc.Queue(job.ID)
	seeOther(w, r, h.base()+"/progress/"+job.ID)
}

func (h *ImportHandler) job(w http.ResponseWriter, r *http.Request, id string) *services.ImportJob {
	job, err := h.Svc.Get(r.Context(), id)
	if err != nil {
		http.Error(w, "could not load the import", http.StatusInternalServerError)
		return nil
	}
	if job == nil {
		http.Error(w, "import job not found", http.StatusNotFound)
	}
	return job
}

func (h *ImportHandler) progress(w http.ResponseWriter, r *http.Request, id string) {
	job := h.job(w, r, id)
	if job == nil {
		return
	}
	_ = h.Renderer.Render(w, r, "Escalated/Admin/Import/Progress", map[string]any{"job": importJobProps(job)})
}

func (h *ImportHandler) status(w http.ResponseWriter, r *http.Request, id string) {
	job := h.job(w, r, id)
	if job == nil {
		return
	}
	errs := job.ErrorLog
	if len(errs) > 50 {
		errs = errs[len(errs)-50:]
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": job.Status, "progress": job.Progress, "error_log": errs,
		"started_at": job.StartedAt, "completed_at": job.CompletedAt,
	})
}

func (h *ImportHandler) control(w http.ResponseWriter, r *http.Request, action, id string) {
	job := h.job(w, r, id)
	if job == nil {
		return
	}
	switch action {
	case "pause":
		if job.Status == "importing" {
			job.Status = "paused"
			if err := h.Svc.Save(r.Context(), job); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	case "resume":
		if job.Resumable() {
			// A failed job goes back through mapping, as Laravel's does.
			if job.Status == "failed" {
				job.Status = "mapping"
				if err := h.Svc.Save(r.Context(), job); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
			h.Svc.Queue(job.ID)
		}
	case "cancel":
		now := time.Now().UTC()
		job.Status = "failed"
		job.CompletedAt = &now
		job.Credentials = nil
		if err := h.Svc.Save(r.Context(), job); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	seeOther(w, r, h.base()+"/progress/"+job.ID)
}

// importJobProps is the job as the screens read it. Laravel sends the model
// as is; its credentials are left out here: they are a secret, and no screen
// reads them.
func importJobProps(j *services.ImportJob) map[string]any {
	var userID any
	if j.UserID != nil {
		userID = string(*j.UserID)
	}
	return map[string]any{
		"id":             j.ID,
		"user_id":        userID,
		"platform":       j.Platform,
		"status":         j.Status,
		"field_mappings": j.FieldMappings,
		"progress":       j.Progress,
		"error_log":      j.ErrorLog,
		"started_at":     j.StartedAt,
		"completed_at":   j.CompletedAt,
		"created_at":     uiTimeValue(j.CreatedAt),
		"updated_at":     uiTimeValue(j.UpdatedAt),
	}
}
