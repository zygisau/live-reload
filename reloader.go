package main

import (
	"fmt"
	"html/template"
	"sync"
	"sync/atomic"

	"github.com/fsnotify/fsnotify"
)

type Reloader struct {
	templates map[string]*template.Template

	*fsnotify.Watcher
	*sync.RWMutex
}

func (r *Reloader) Get(name string) *template.Template {
	r.RLock()
	defer r.Unlock()
	if t, ok := r.templates[name]; ok {
		return t
	}
	return nil
}

// New returns an initialized Reloader that starts watching the given
// directories for all events.
func New(dirs ...string) *Reloader {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		panic(err)
	}

	for _, path := range dirs {
		watcher.Add(path)
	}

	return &Reloader{
		Watcher: watcher,
		RWMutex: &sync.RWMutex{},
	}
}

func AddClamp(f uint8) uint8 {
	return (f + 1) % 255
}

func (r *Reloader) Watch() {
	go func() {
		for {
			select {
			case evt := <-r.Watcher.Events:
				if eventIsWanted(evt.Op) {
					fmt.Printf("File: %s Event: %s. Hot reloading.\n",
						evt.Name, evt.String())

					if err := r.reload(evt.Name); err != nil {
						fmt.Println(err)
					}

					atomic.AddUint64(&versionCounter, 1)
					broadcastCond.Broadcast()
				}
			case err := <-r.Watcher.Errors:
				fmt.Println(err)
			}
		}
	}()
}

func eventIsWanted(op fsnotify.Op) bool {
	switch op {
	case fsnotify.Write, fsnotify.Create:
		return true
	default:
		return false
	}
}

func (r *Reloader) reload(name string) error {

	// Just for example purposes, and sssuming 'index.gohtml' is in the
	// same directory as this file.
	if name == TemplatePath+"reload.go" {
		return nil
	}

	if len(name) >= len(TemplateExt) &&
		name[len(name)-len(TemplateExt):] == TemplateExt {

		tmpl := template.Must(template.ParseFiles(name))

		// Gather what would be the key in our template map.
		// 'name' is in the format: "path/identifier.extension",
		// so trim the 'path/' and the '.extension' to get the
		// name (minus new extension) used inside of our map.
		key := name[0 : len(name)-len(TemplateExt)]

		r.Lock()
		r.templates[key] = tmpl
		r.Unlock()
		return nil
	}

	return fmt.Errorf("Unable to reload file %s", name)

}
