package handlers

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/store"
)

// AttachmentHandler serves attachment download endpoints.
type AttachmentHandler struct {
	store       store.Store
	routePrefix string

	// AgentCheck, AdminCheck and UserID decide who may download. Agents and
	// admins may download any attachment. Anyone else may download only from a
	// ticket they requested, and never an attachment on an internal note. The
	// routers wire these from Config. A nil check counts as "no", so a handler
	// mounted without them refuses every download.
	AgentCheck func(r *http.Request) bool
	AdminCheck func(r *http.Request) bool
	UserID     func(r *http.Request) models.UserID
}

// NewAttachmentHandler creates a new AttachmentHandler.
func NewAttachmentHandler(s store.Store, routePrefix string) *AttachmentHandler {
	return &AttachmentHandler{
		store:       s,
		routePrefix: routePrefix,
	}
}

// Download handles GET /attachments/{id}/download — streams the file from disk
// to an agent, an admin, or the customer who requested the owning ticket.
func (h *AttachmentHandler) Download(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid attachment id"})
		return
	}

	staff := h.isStaff(r)
	var uid models.UserID
	if h.UserID != nil {
		uid = h.UserID(r)
	}
	// Checked before the lookup, so an anonymous caller cannot even learn
	// whether an id exists.
	if !staff && uid.Empty() {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	a, err := h.store.GetAttachmentByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if a == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "attachment not found"})
		return
	}

	if !staff {
		allowed, err := h.requesterMayDownload(r, a, uid)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if !allowed {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
	}

	f, err := os.Open(a.StoragePath)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found on disk"})
		return
	}
	defer f.Close()

	// Attachments are user-supplied (inbound mail parts carry the sender's
	// MIME type), so they are never rendered on the app origin: always a
	// download, always an opaque type, and never sniffed. Mirrors Laravel's
	// AttachmentController.
	name := safeDownloadFilename(a.OriginalFilename)
	hdr := w.Header()
	hdr.Set("Content-Type", "application/octet-stream")
	hdr.Set("Content-Disposition", contentDispositionAttachment(name))
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Cache-Control", "private, no-store, max-age=0")
	hdr.Set("Referrer-Policy", "no-referrer")
	hdr.Set("Content-Security-Policy", "default-src 'none'; sandbox")

	// ServeContent sets Content-Length itself; the name passed here has no
	// effect because Content-Type is already set.
	http.ServeContent(w, r, name, a.CreatedAt, f)
}

// safeDownloadFilename keeps only the base name and drops control characters,
// falling back to "attachment" when nothing is left.
func safeDownloadFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "attachment"
	}
	return name
}

// contentDispositionAttachment builds an RFC 6266 "attachment" header with an
// ASCII-only quoted filename (quotes, backslashes and non-ASCII replaced) and
// an RFC 5987 filename* carrying the exact UTF-8 name.
func contentDispositionAttachment(name string) string {
	var ascii strings.Builder
	for _, r := range name {
		switch {
		case r == '"' || r == '\\' || r == ';' || r == '%':
			ascii.WriteByte('_')
		case r < 0x20 || r >= 0x7f:
			ascii.WriteByte('_')
		default:
			ascii.WriteRune(r)
		}
	}
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, ascii.String(), rfc5987Escape(name))
}

// rfc5987Escape percent-encodes everything outside RFC 5987's attr-char set.
func rfc5987Escape(s string) string {
	const attrChars = "!#$&+-.^_`|~"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.IndexByte(attrChars, c) >= 0 {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

func (h *AttachmentHandler) isStaff(r *http.Request) bool {
	return (h.AgentCheck != nil && h.AgentCheck(r)) || (h.AdminCheck != nil && h.AdminCheck(r))
}

// requesterMayDownload reports whether uid requested the attachment's ticket and
// the attachment is not on an internal note, which customers never see.
func (h *AttachmentHandler) requesterMayDownload(r *http.Request, a *models.Attachment, uid models.UserID) (bool, error) {
	t, err := h.store.GetTicket(r.Context(), a.TicketID)
	if err != nil || t == nil {
		return false, err
	}
	if !requestedBy(t, uid) {
		return false, nil
	}
	if a.ReplyID == nil {
		return true, nil
	}
	reply, err := h.store.GetReply(r.Context(), *a.ReplyID)
	if err != nil {
		return false, err
	}
	return reply != nil && !reply.IsInternal, nil
}

// AttachmentURL returns the download URL for an attachment given its ID.
func AttachmentURL(routePrefix string, id int64) string {
	return fmt.Sprintf("%s/attachments/%d/download", routePrefix, id)
}
