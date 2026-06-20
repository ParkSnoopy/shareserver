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
)

func main() {
	c := config.Load()
	d, err := db.Open(c.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	if err := auth.EnsureAdmin(d, c.AdminUser, c.AdminPassword, c.Dev); err != nil {
		log.Fatal(err)
	}
	a := &app.App{C: c, DB: d, T: template.New("")}
	h := &httpx.Handler{A: a}
	h.StartCleanup()
	log.Println("listening", c.Addr)
	log.Fatal(http.ListenAndServe(c.Addr, httpx.New(a)))
}
