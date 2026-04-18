package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

func main() {
	http.HandleFunc("/demo", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "<!-- TODO: wire the escalated-go router, a demo users table, click-to-login -->")
		fmt.Fprintln(w, "Escalated Go demo — scaffold. Set APP_ENV=demo.")
	})
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if os.Getenv("APP_ENV") == "demo" {
			http.Redirect(w, r, "/demo", http.StatusFound)
			return
		}
		w.Write([]byte("Escalated Go demo host."))
	})
	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8000"
	}
	log.Printf("[demo] listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
