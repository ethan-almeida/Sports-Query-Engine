package ui

import (
	"embed"
	"net/http"
)

//go:embed index.html
var files embed.FS

func Handler() (http.Handler, error) {
	page, err := files.ReadFile("index.html")
	if err != nil {
		return nil, err
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(page)
	}), nil
}
