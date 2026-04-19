package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"

	"github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/migrations"
	"github.com/escalated-dev/escalated-go/router"
	"github.com/escalated-dev/escalated-go/store"
	"github.com/go-chi/chi/v5"
	_ "github.com/lib/pq"
)

type demoUser struct {
	ID      int64
	Name    string
	Email   string
	IsAdmin bool
	IsAgent bool
}

var (
	users = []demoUser{
		{1, "Alice Admin", "alice@demo.test", true, true},
		{2, "Bob Agent", "bob@demo.test", false, true},
		{3, "Carol Agent", "carol@demo.test", false, true},
		{4, "Frank Customer", "frank@acme.example", false, false},
		{5, "Grace Customer", "grace@acme.example", false, false},
		{6, "Henry Customer", "henry@globex.example", false, false},
	}
	sessions   = map[string]int64{}
	sessionsMu sync.RWMutex
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://escalated:escalated@db:5432/escalated?sslmode=disable"
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := migrations.Migrate(db, "escalated_"); err != nil {
		log.Fatal(err)
	}

	cfg := escalated.DefaultConfig()
	cfg.DB = db
	cfg.RoutePrefix = "/support"
	cfg.UIEnabled = false
	cfg.AdminCheck = func(r *http.Request) bool {
		u := currentDemoUser(r)
		return u != nil && u.IsAdmin
	}
	cfg.AgentCheck = func(r *http.Request) bool {
		u := currentDemoUser(r)
		return u != nil && (u.IsAgent || u.IsAdmin)
	}

	esc, err := escalated.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	_ = esc

	r := chi.NewRouter()

	r.Get("/demo", picker)
	r.Post("/demo/login/{id}", loginAs)
	r.Post("/demo/logout", logoutDemo)
	r.Get("/", home)

	// Mount the escalated routes
	router.MountChi(r, esc)

	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8000"
	}
	log.Printf("[demo] listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}

func currentDemoUser(r *http.Request) *demoUser {
	c, err := r.Cookie("demo_session")
	if err != nil {
		return nil
	}
	sessionsMu.RLock()
	id, ok := sessions[c.Value]
	sessionsMu.RUnlock()
	if !ok {
		return nil
	}
	for i := range users {
		if users[i].ID == id {
			return &users[i]
		}
	}
	return nil
}

func home(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("APP_ENV") == "demo" {
		http.Redirect(w, r, "/demo", http.StatusFound)
		return
	}
	fmt.Fprintln(w, "Escalated Go demo host. Set APP_ENV=demo to enable /demo.")
}

func picker(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("APP_ENV") != "demo" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, pickerHTML())
}

func loginAs(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("APP_ENV") != "demo" {
		http.NotFound(w, r)
		return
	}
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var u *demoUser
	for i := range users {
		if users[i].ID == id {
			u = &users[i]
			break
		}
	}
	if u == nil {
		http.NotFound(w, r)
		return
	}
	sid := fmt.Sprintf("demo-%d", id)
	sessionsMu.Lock()
	sessions[sid] = id
	sessionsMu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "demo_session", Value: sid, Path: "/", HttpOnly: true})

	dest := "/support/api/tickets"
	if u.IsAdmin || u.IsAgent {
		dest = "/support/api/departments"
	}
	http.Redirect(w, r, dest, http.StatusFound)
}

func logoutDemo(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("APP_ENV") != "demo" {
		http.NotFound(w, r)
		return
	}
	c, err := r.Cookie("demo_session")
	if err == nil {
		sessionsMu.Lock()
		delete(sessions, c.Value)
		sessionsMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "demo_session", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/demo", http.StatusFound)
}

func pickerHTML() string {
	var b string
	b = `<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8"><title>Escalated · Go Demo</title>
<style>*{box-sizing:border-box}body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;background:#0f172a;color:#e2e8f0;margin:0;padding:2rem}
.wrap{max-width:720px;margin:0 auto}h1{font-size:1.5rem;margin:0 0 .25rem}p.lede{color:#94a3b8;margin:0 0 2rem}
.group{margin-bottom:1.5rem}.group h2{font-size:.75rem;text-transform:uppercase;letter-spacing:.08em;color:#64748b;margin:0 0 .5rem}
form{display:block;margin:0}
button.user{display:flex;width:100%;align-items:center;justify-content:space-between;padding:.75rem 1rem;background:#1e293b;border:1px solid #334155;border-radius:8px;color:#f1f5f9;font-size:.95rem;cursor:pointer;margin-bottom:.5rem;text-align:left}
button.user:hover{background:#273549;border-color:#475569}
.meta{color:#94a3b8;font-size:.8rem}
.badge{font-size:.7rem;padding:.15rem .5rem;border-radius:999px;background:#334155;color:#cbd5e1;margin-left:.5rem}
.badge.admin{background:#7c3aed;color:#fff}.badge.agent{background:#0ea5e9;color:#fff}
</style></head><body><div class="wrap"><h1>Escalated Go Demo</h1>
<p class="lede">Click a user to log in. Every restart reseeds the in-memory user table.</p>`
	for _, u := range users {
		badge := ""
		if u.IsAdmin {
			badge = `<span class="badge admin">Admin</span>`
		} else if u.IsAgent {
			badge = `<span class="badge agent">Agent</span>`
		}
		b += fmt.Sprintf(`<form method="POST" action="/demo/login/%d"><button type="submit" class="user"><span>%s %s</span><span class="meta">%s</span></button></form>`,
			u.ID, u.Name, badge, u.Email)
	}
	b += `</div></body></html>`
	return b
}

var _ = store.NewPostgresStore // keep import for the side effect
