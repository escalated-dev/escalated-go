package email

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/models"
)

// ErrAttachmentTooLarge is returned by AttachmentDownloader when a
// provider-hosted attachment exceeds DownloadConfig.MaxBytes. The
// partial body is not persisted.
var ErrAttachmentTooLarge = errors.New("attachment exceeds configured size limit")

// AttachmentStore is the minimal contract for persisting an Attachment
// row. Host apps pass their store.Store (which already implements
// CreateAttachment).
type AttachmentStore interface {
	CreateAttachment(ctx context.Context, a *models.Attachment) error
}

// AttachmentStorage is the minimal contract for writing attachment
// bytes to a backend. Implementations can persist to local filesystem,
// S3, etc. Put returns a storage-specific path/key that can later be
// used to retrieve the file.
type AttachmentStorage interface {
	Put(ctx context.Context, filename string, content io.Reader, contentType string) (path string, err error)
}

// BasicAuth carries HTTP basic auth credentials for provider downloads
// (Mailgun's stored-message URLs require this via the API key).
type BasicAuth struct {
	Username string
	Password string
}

// DownloadConfig controls how PendingAttachment URLs are fetched.
type DownloadConfig struct {
	// HTTPClient used for downloads. nil → http.DefaultClient with a
	// 30-second timeout.
	HTTPClient *http.Client

	// MaxBytes rejects attachments larger than this value. 0 = no limit.
	MaxBytes int64

	// BasicAuth attached to the GET request when non-nil. Typical use:
	// `{Username: "api", Password: mailgunApiKey}`.
	BasicAuth *BasicAuth
}

// AttachmentDownloader fetches provider-hosted attachments surfaced by
// InboundEmailService.ProcessResult.PendingAttachmentDownloads and
// persists them as Attachment rows tied to a ticket (and optionally
// a reply). Host apps run this in a background worker after
// InboundEmailService returns; the webhook response can be sent
// back to the provider immediately regardless of download latency.
type AttachmentDownloader struct {
	cfg     DownloadConfig
	storage AttachmentStorage
	store   AttachmentStore
}

// NewAttachmentDownloader constructs a downloader. A nil HTTPClient in
// cfg gets replaced with a default client carrying a 30s timeout.
func NewAttachmentDownloader(cfg DownloadConfig, storage AttachmentStorage, store AttachmentStore) *AttachmentDownloader {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &AttachmentDownloader{cfg: cfg, storage: storage, store: store}
}

// Download fetches one PendingAttachment and persists it. Returns the
// persisted Attachment on success.
func (d *AttachmentDownloader) Download(
	ctx context.Context,
	p PendingAttachment,
	ticketID int64,
	replyID *int64,
) (*models.Attachment, error) {
	if p.DownloadURL == "" {
		return nil, errors.New("pending attachment has no download URL")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.DownloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request for %s: %w", p.DownloadURL, err)
	}
	if d.cfg.BasicAuth != nil {
		req.SetBasicAuth(d.cfg.BasicAuth.Username, d.cfg.BasicAuth.Password)
	}

	resp, err := d.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", p.DownloadURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch %s: unexpected status %d", p.DownloadURL, resp.StatusCode)
	}

	reader := io.Reader(resp.Body)
	if d.cfg.MaxBytes > 0 {
		// Read one extra byte beyond the limit so we can distinguish
		// "exactly at limit" from "over limit".
		reader = io.LimitReader(resp.Body, d.cfg.MaxBytes+1)
	}

	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p.DownloadURL, err)
	}
	if d.cfg.MaxBytes > 0 && int64(len(body)) > d.cfg.MaxBytes {
		return nil, fmt.Errorf("%w: %d > %d bytes for %s",
			ErrAttachmentTooLarge, len(body), d.cfg.MaxBytes, p.Name)
	}

	filename := safeFilename(p.Name)
	contentType := p.ContentType
	if contentType == "" {
		contentType = resp.Header.Get("Content-Type")
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	path, err := d.storage.Put(ctx, filename, strings.NewReader(string(body)), contentType)
	if err != nil {
		return nil, fmt.Errorf("store %s: %w", filename, err)
	}

	attachment := &models.Attachment{
		TicketID:         ticketID,
		ReplyID:          replyID,
		OriginalFilename: filename,
		MimeType:         contentType,
		Size:             int64(len(body)),
		StoragePath:      path,
		CreatedAt:        time.Now().UTC(),
	}
	if err := d.store.CreateAttachment(ctx, attachment); err != nil {
		return nil, fmt.Errorf("persist attachment row: %w", err)
	}
	return attachment, nil
}

// DownloadAll processes a list of PendingAttachments. Returns a slice
// of persisted attachments (one per input, nil for the failures) and
// a slice of errors (nil for the successes). Continues past failures
// so a bad URL doesn't prevent other attachments from persisting.
func (d *AttachmentDownloader) DownloadAll(
	ctx context.Context,
	attachments []PendingAttachment,
	ticketID int64,
	replyID *int64,
) ([]*models.Attachment, []error) {
	results := make([]*models.Attachment, len(attachments))
	errs := make([]error, len(attachments))
	for i, p := range attachments {
		a, err := d.Download(ctx, p, ticketID, replyID)
		results[i] = a
		errs[i] = err
	}
	return results, errs
}

// safeFilename strips path separators and empty-string inputs so
// attackers can't escape the storage root via "../../etc/passwd"
// style names. Falls back to "attachment" when the name is unusable.
func safeFilename(name string) string {
	clean := filepath.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	if clean == "" || clean == "." || clean == "/" {
		return "attachment"
	}
	return clean
}
