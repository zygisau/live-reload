package main

import (
	"flag"
	"fmt"
	"html/template"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write the file to the client.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the client.
	pongWait = 60 * time.Second

	// Send pings to client with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Poll file for changes with this period.
	broadcastPeriod = 10 * time.Second

	// TemplateExt is the extension for the physical template files. Failure
	// to set this to the same extension your physical template files have
	// will result in the failure to reload the files.
	TemplateExt = ".html"

	// TemplatePath is the path to the directory containing the template files.
	// It defaults to the current directory, provided you call r.Watch("./")
	TemplatePath = "./"
)

var (
	addr     = flag.String("addr", ":8080", "http service address")
	filename string
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
	broadcastCondMu sync.Mutex
	broadcastCond   *sync.Cond
	versionCounter  uint64
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
	return f + 1%255
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

	return fmt.Errorf("Unable to reload file %s\n", name)

}

func handleWebSocket(w http.ResponseWriter, r *http.Request) *websocket.Conn {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println("WebSocket upgrade error:", err)
		return nil
	}

	return conn
}

func waitForBroadcast(conn *websocket.Conn) {
	// Wait for a broadcast signal
	broadcastCond.L.Lock()
	var oldVersion uint64
	for {
		oldVersion = versionCounter
		broadcastCond.Wait()

		if oldVersion == versionCounter {
			// check if connection is still alive
			if err := conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				fmt.Errorf("<Websocket %v> Error writing: %v",
					conn.RemoteAddr(), err)
				break
			}
			continue
		}

		err := conn.WriteJSON(websocketEvent{Type: "build_complete"})
		if err != nil {
			fmt.Errorf("<Websocket %v> Error writing: %v",
				conn.RemoteAddr(), err)
			break
		}
	}
	broadcastCond.L.Unlock()
}

type websocketEvent struct {
	Type string `json:"type"`
}

func getServeWs() http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var conn *websocket.Conn
		if conn = handleWebSocket(w, r); conn == nil {
			fmt.Println("Error handling websocket")
			return
		}
		go waitForBroadcast(conn)
	})
}

type Todo struct {
	Title string
	Done  bool
}

type TodoPageData struct {
	Host      string
	PageTitle string
	Todos     []Todo
}

func render(r *Reloader, w http.ResponseWriter, name string, data interface{}) (err error) {
	tmpl := r.templates[name]
	if err = tmpl.Execute(w, data); err != nil {
		panic(err)
	}
	return
}

func getServeHome(reloader *Reloader) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		data := TodoPageData{
			Host:      r.Host,
			PageTitle: "My TODO list",
			Todos: []Todo{
				{Title: "Task 1", Done: false},
				{Title: "Task 2", Done: true},
				{Title: "Task 3", Done: true},
			},
		}
		render(reloader, w, "index", data)
	})
}

func broadcastInterval() {
	ticker := time.NewTicker(broadcastPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			broadcastCond.Broadcast()
		}
	}
}

func main() {
	broadcastCond = sync.NewCond(&broadcastCondMu)
	go broadcastInterval()
	r := New("./")
	r.templates = map[string]*template.Template{
		"index": template.Must(template.ParseFiles("index.html")),
	}

	r.Watch()
	http.Handle("/", getServeHome(r))
	http.Handle("/ws", getServeWs())
	http.ListenAndServe(*addr, nil)
}
