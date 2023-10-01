package main

import "net/http"

func render(r *Reloader, w http.ResponseWriter, name string, data interface{}) (err error) {
	tmpl := r.templates[name]
	if err = tmpl.Execute(w, data); err != nil {
		panic(err)
	}
	return
}
