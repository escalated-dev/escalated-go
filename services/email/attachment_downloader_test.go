package email

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/escalated-dev/escalated-go/models"
)

// fakeStorage records Put calls and stages the path it returns.
type fakeStorage struct {
	returnPath string
	returnErr  error
	calls      []struct {
		filename    string
		contentType string
		content     string
	}
}

func (s *fakeStorage) Put(_ context.Context, filename string, content io.Reader, contentType string) (string, error) {
	body, _ := io.ReadAll(content)
	s.calls = append(s.calls, struct {
		filename    string
		contentType string
		content     string
	}{filename, contentType, string(body)})
	if s.returnErr != nil {
		return "", s.returnErr
	}
	return s.returnPath, nil
}

// fakeAttachmentStore records CreateAttachment calls.
type fakeAttachmentStore struct {
	returnErr error
	created   []*models.Attachment
}

func (s *fakeAttachmentStore) CreateAttachment(_ context.Context, a *models.Attachment) error {
	if s.returnErr != nil {
		return s.returnErr
	}
	a.ID = int64(len(s.created) + 1)
	s.created = append(s.created, a)
	return nil
}

func TestAttachmentDownloader_Download_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("hello pdf"))
	}))
	defer server.Close()

	storage := &fakeStorage{returnPath: "/var/escalated/report.pdf"}
	store := &fakeAttachmentStore{}
	d := NewAttachmentDownloader(DownloadConfig{}, storage, store)

	pending := PendingAttachment{
		Name:        "report.pdf",
		ContentType: "application/pdf",
		SizeBytes:   9,
		DownloadURL: server.URL + "/att/report",
	}

	a, err := d.Download(context.Background(), pending, 42, nil)
	if err != nil {
		t.Fatalf("Download err = %v", err)
	}

	if a.TicketID != 42 {
		t.Errorf("TicketID = %d, want 42", a.TicketID)
	}
	if a.OriginalFilename != "report.pdf" {
		t.Errorf("OriginalFilename = %q, want report.pdf", a.OriginalFilename)
	}
	if a.StoragePath != storage.returnPath {
		t.Errorf("StoragePath = %q, want %q", a.StoragePath, storage.returnPath)
	}
	if a.Size != 9 {
		t.Errorf("Size = %d, want 9", a.Size)
	}
	if a.MimeType != "application/pdf" {
		t.Errorf("MimeType = %q, want application/pdf", a.MimeType)
	}
	if len(store.created) != 1 {
		t.Fatalf("store.created length = %d, want 1", len(store.created))
	}
	if len(storage.calls) != 1 || storage.calls[0].content != "hello pdf" {
		t.Errorf("storage.calls = %+v, want one write with %q", storage.calls, "hello pdf")
	}
}

func TestAttachmentDownloader_Download_HTTP404_NoAttachmentCreated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	storage := &fakeStorage{}
	store := &fakeAttachmentStore{}
	d := NewAttachmentDownloader(DownloadConfig{}, storage, store)

	_, err := d.Download(context.Background(), PendingAttachment{
		Name: "x.txt", DownloadURL: server.URL,
	}, 1, nil)
	if err == nil {
		t.Fatalf("expected error for 404, got nil")
	}
	if len(store.created) != 0 {
		t.Errorf("store.created = %d, want 0 on error", len(store.created))
	}
	if len(storage.calls) != 0 {
		t.Errorf("storage.calls = %d, want 0 on error", len(storage.calls))
	}
}

func TestAttachmentDownloader_Download_SizeLimitExceeded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, 100))
	}))
	defer server.Close()

	storage := &fakeStorage{}
	store := &fakeAttachmentStore{}
	d := NewAttachmentDownloader(DownloadConfig{MaxBytes: 10}, storage, store)

	_, err := d.Download(context.Background(), PendingAttachment{
		Name: "big.bin", DownloadURL: server.URL,
	}, 1, nil)

	if !errors.Is(err, ErrAttachmentTooLarge) {
		t.Fatalf("err = %v, want ErrAttachmentTooLarge", err)
	}
	if len(store.created) != 0 {
		t.Errorf("store.created = %d, want 0 on size overflow", len(store.created))
	}
}

func TestAttachmentDownloader_Download_SendsBasicAuth(t *testing.T) {
	var gotUser, gotPass string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, _ := r.BasicAuth()
		gotUser = u
		gotPass = p
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	d := NewAttachmentDownloader(
		DownloadConfig{BasicAuth: &BasicAuth{Username: "api", Password: "key-secret"}},
		&fakeStorage{returnPath: "/x"},
		&fakeAttachmentStore{},
	)

	_, err := d.Download(context.Background(), PendingAttachment{
		Name: "x", DownloadURL: server.URL,
	}, 1, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	if gotUser != "api" || gotPass != "key-secret" {
		t.Errorf("basic auth user=%q pass=%q, want api/key-secret", gotUser, gotPass)
	}
}

func TestAttachmentDownloader_Download_MissingURL(t *testing.T) {
	d := NewAttachmentDownloader(DownloadConfig{}, &fakeStorage{}, &fakeAttachmentStore{})
	_, err := d.Download(context.Background(), PendingAttachment{Name: "x"}, 1, nil)
	if err == nil {
		t.Fatalf("expected error for empty URL")
	}
}

func TestAttachmentDownloader_Download_FallsBackToResponseContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png bytes"))
	}))
	defer server.Close()

	storage := &fakeStorage{returnPath: "/p"}
	d := NewAttachmentDownloader(DownloadConfig{}, storage, &fakeAttachmentStore{})

	// ContentType left empty on the PendingAttachment; the response
	// header should fill it in.
	a, err := d.Download(context.Background(), PendingAttachment{
		Name: "img", ContentType: "", DownloadURL: server.URL,
	}, 1, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if a.MimeType != "image/png" {
		t.Errorf("MimeType = %q, want image/png", a.MimeType)
	}
}

func TestAttachmentDownloader_Download_SafeFilename(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("x"))
	}))
	defer server.Close()

	storage := &fakeStorage{returnPath: "/p"}
	d := NewAttachmentDownloader(DownloadConfig{}, storage, &fakeAttachmentStore{})

	_, err := d.Download(context.Background(), PendingAttachment{
		Name: "../../etc/passwd", DownloadURL: server.URL,
	}, 1, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	if len(storage.calls) != 1 || storage.calls[0].filename != "passwd" {
		t.Errorf("filename = %q, want %q (basename)", storage.calls[0].filename, "passwd")
	}
}

func TestAttachmentDownloader_DownloadAll_ContinuesPastFailures(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/good", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/bad", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusInternalServerError)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	d := NewAttachmentDownloader(DownloadConfig{}, &fakeStorage{returnPath: "/p"}, &fakeAttachmentStore{})

	pending := []PendingAttachment{
		{Name: "a", DownloadURL: server.URL + "/good"},
		{Name: "b", DownloadURL: server.URL + "/bad"},
		{Name: "c", DownloadURL: server.URL + "/good"},
	}
	results, errs := d.DownloadAll(context.Background(), pending, 1, nil)

	if len(results) != 3 || len(errs) != 3 {
		t.Fatalf("lengths = %d/%d, want 3/3", len(results), len(errs))
	}
	if results[0] == nil || errs[0] != nil {
		t.Errorf("index 0: want success, got %+v / %v", results[0], errs[0])
	}
	if results[1] != nil || errs[1] == nil {
		t.Errorf("index 1: want failure, got %+v / %v", results[1], errs[1])
	}
	if results[2] == nil || errs[2] != nil {
		t.Errorf("index 2: want success, got %+v / %v", results[2], errs[2])
	}
}

func TestLocalFileStorage_Put_WritesFile(t *testing.T) {
	root := t.TempDir()
	storage, err := NewLocalFileStorage(root)
	if err != nil {
		t.Fatalf("NewLocalFileStorage err = %v", err)
	}

	path, err := storage.Put(context.Background(), "hello.txt", strings.NewReader("payload"), "text/plain")
	if err != nil {
		t.Fatalf("Put err = %v", err)
	}

	if !strings.HasPrefix(path, root) {
		t.Errorf("path %q not under root %q", path, root)
	}
	if !strings.HasSuffix(path, "hello.txt") {
		t.Errorf("path %q should end with filename", path)
	}

	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile err = %v", err)
	}
	if string(bytes) != "payload" {
		t.Errorf("file content = %q, want %q", string(bytes), "payload")
	}
}

func TestLocalFileStorage_Put_DifferentCallsProduceDifferentPaths(t *testing.T) {
	root := t.TempDir()
	storage, _ := NewLocalFileStorage(root)

	p1, _ := storage.Put(context.Background(), "x.txt", strings.NewReader("a"), "text/plain")
	p2, _ := storage.Put(context.Background(), "x.txt", strings.NewReader("b"), "text/plain")

	if p1 == p2 {
		t.Errorf("two Puts returned same path %q", p1)
	}
}

func TestNewLocalFileStorage_RejectsEmptyRoot(t *testing.T) {
	_, err := NewLocalFileStorage("")
	if err == nil {
		t.Errorf("expected error on empty root")
	}
}

func TestNewLocalFileStorage_CreatesMissingRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nested", "path")
	s, err := NewLocalFileStorage(root)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if s.Root != root {
		t.Errorf("Root = %q, want %q", s.Root, root)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		t.Errorf("root not created as directory")
	}
}
