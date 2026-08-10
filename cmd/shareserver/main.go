package main

import (
	"html/template"
	"log"
	"net/http"
	"shareserver/internal/app"
	"shareserver/internal/auth"
	"shareserver/internal/config"
	"shareserver/internal/db"
	httpx "shareserver/internal/http"
	"shareserver/internal/share"
	"shareserver/internal/storage"
)

// main loads runtime config, opens storage, starts cleanup, and serves HTTP.
func main() {
	c := config.Load()
	d, err := db.Open(c.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	defer d.Close()
	if err := auth.EnsureAdmin(d, c.AdminUser, c.AdminPassword, c.Dev); err != nil {
		log.Fatal(err)
	}
	store := share.NewStore(d)
	integrity := storage.NewIntegrity(c.BlobDir, store)
	a := &app.App{C: c, DB: d, T: template.New(""), Integrity: integrity}
	h := &httpx.Handler{A: a, Store: store, Integrity: integrity}
	h.StartCleanup()
	log.Println("listening", c.Addr)
	log.Fatal(http.ListenAndServe(c.Addr, httpx.New(a)))
}
