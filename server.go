package main

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ughoavgfhw/kq-live/assets"
)

type dataPoint struct {
	when time.Time
	vals []float64
	event string
}
func runRegistry(in <-chan interface{}, reg <-chan *chan<- interface{}, unreg <-chan *chan<- interface{}) {
	registry := make(map[*chan<- interface{}]struct{})
	for {
		select {
		case c := <-reg: registry[c] = struct{}{}
		case c := <-unreg: delete(registry, c)
		case x := <-in:
			for c, _ := range registry {
				select {
				case *c <- x:
				default:  // Drop packets to a client instead of blocking the entire server
				}
			}
		}
	}
}

func startWebServer(dataSource <-chan interface{}) {
	reg := make(chan *chan<- interface{}, 1)
	unreg := make(chan *chan<- interface{})
	go runRegistry(dataSource, reg, unreg)
	http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		var content http.File
		var err error
		// TODO: Serve the gzip-encoded form if available.
		switch req.FormValue("type") {
		case "", "line":
			content, err = assets.FS.Open("/line_chart.html")
		case "meter":
			content, err = assets.FS.Open("/meter.html")
		default:
			w.WriteHeader(400)
			// Doesn't handle ranges or set all of the headers of ServeContent,
			// but ServeContent can't be used after setting a status code.
			content, err = assets.FS.Open("/line_chart.html")
			if err != nil { panic(err) }
			io.Copy(w, content)
			return
		}
		if err != nil { panic(err) }
		var modtime time.Time
		if info, err := content.Stat(); err != nil { modtime = info.ModTime() }
		http.ServeContent(w, req, "index.html", modtime, content)
	})
	var upgrader websocket.Upgrader
	http.HandleFunc("/predictions", func(w http.ResponseWriter, req *http.Request) {
		conn, err := upgrader.Upgrade(w, req, nil)
		if err != nil {
			fmt.Println(err)
			return
		}
		go func() {
			for {
				if _, _, e := conn.NextReader(); e != nil {
					conn.Close()
					break
				}
			}
		}()
		c := make(chan interface{}, 256)
		var writeEnd chan<- interface{} = c
		reg <- &writeEnd
		go func() {
			var timeBuff []byte
			for v := range c {
				w, e := conn.NextWriter(websocket.TextMessage)
				if e != nil { fmt.Println(e); break }
				switch v := v.(type) {
				case time.Time:
					timeBuff = v.AppendFormat(timeBuff[:0], time.RFC3339Nano)
					_, e = fmt.Fprintf(w, "reset,%s,4", timeBuff)
					ce := w.Close()
					if e != nil { fmt.Println(e); break }
					if ce != nil { fmt.Println(ce); break }
				case dataPoint:
					timeBuff = v.when.AppendFormat(timeBuff[:0], time.RFC3339Nano)
					_, e = fmt.Fprintf(w, "next,%s,%s", timeBuff, v.event)
					for _, val := range v.vals { _, e = fmt.Fprintf(w, ",%v", val) }
					ce := w.Close()
					if e != nil { fmt.Println(e); break }
					if ce != nil { fmt.Println(ce); break }
				}
			}
			unreg <- &writeEnd
			conn.Close()
		}()
	})
	panic(http.ListenAndServe(":8080", nil))
}
