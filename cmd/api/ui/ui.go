package ui

import (
	"embed"
	"fmt"
	"net/http"
)

//go:embed index.html
var files embed.FS

var page = loadPage()

func loadPage() []byte {
	page, err := files.ReadFile("index.html")
	if err != nil {
		panic(fmt.Sprintf("ui: load embedded page: %v", err))
	}
	return page
}

func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(page)
	})
}
