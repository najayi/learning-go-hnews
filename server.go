package main

import (
	"net/http"
	"time"
)

func (app *application) Serve() error {
	srv := http.Server{
		Addr:         ":8080",
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
		Handler:      app.routes(),
	}
	return srv.ListenAndServe()
}
