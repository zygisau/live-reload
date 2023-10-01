package main

import (
	"flag"
	"fmt"
	"html/template"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
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
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
	broadcastCondMu sync.Mutex
	broadcastCond   *sync.Cond
	versionCounter  uint64
)

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

		data := getData(r.Host)
		render(reloader, w, "index", data)
	})
}

// broadcast every {broadcastPeriod} seconds to all connected clients
// each thread will check for a version and if it's the same, it will try to ping websocket
// if it fails, it will break out of the loop and close the thread
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
