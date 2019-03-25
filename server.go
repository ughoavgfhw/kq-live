package main

import (
	"encoding/json"
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
	http.Handle("/static/", http.FileServer(assets.FS))
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
	http.HandleFunc("/ws", func(w http.ResponseWriter, req *http.Request) {
		conn, err := upgrader.Upgrade(w, req, nil)
		if err != nil {
			fmt.Println(err)
			return
		}
		dataChan := make(chan string)
		go func() {
			for {
				_, r, err := conn.NextReader()
				if err != nil {
					fmt.Println(err)
					close(dataChan)
					break
				}
				dec := json.NewDecoder(r)
				dec.DisallowUnknownFields()
				var tok json.Token
				// TODO: Send back an error on parse error, handle inputs.
				if tok, err = dec.Token(); err != nil {
					fmt.Println("failed to parse message;", err)
					continue
				} else if v, ok := tok.(json.Delim); !ok {
					fmt.Println("failed to parse message;", err)
					continue
				} else if v != '{' {
					fmt.Println("failed to parse message;", err)
					continue
				}
				if tok, err = dec.Token(); err != nil {
					fmt.Println("failed to parse message;", err)
					continue
				} else if v, ok := tok.(string); !ok {
					fmt.Println("failed to parse message;", err)
					continue
				} else if v != "type" {
					fmt.Println("failed to parse message;", err)
					continue
				}
				var typ string
				if tok, err = dec.Token(); err != nil {
					fmt.Println("failed to parse message;", err)
					continue
				} else if v, ok := tok.(string); !ok {
					fmt.Println("failed to parse message;", err)
					continue
				} else {
					typ = v
				}
				if tok, err = dec.Token(); err != nil {
					fmt.Println("failed to parse message;", err)
					continue
				} else if v, ok := tok.(string); !ok {
					fmt.Println("failed to parse message;", err)
					continue
				} else if v != "data" {
					fmt.Println("failed to parse message;", err)
					continue
				}
				// TODO: Parse an expected structure based on type.
				var data interface{}
				if err = dec.Decode(&data); err != nil {
					fmt.Println("failed to parse message;", err)
					continue
				}
				if tok, err = dec.Token(); err != nil {
					fmt.Println("failed to parse message;", err)
					continue
				} else if v, ok := tok.(json.Delim); !ok {
					fmt.Println("failed to parse message;", err)
					continue
				} else if v != '}' {
					fmt.Println("failed to parse message;", err)
					continue
				}
				if tok, err = dec.Token(); err != io.EOF {
					fmt.Println("failed to parse message; expected EOF; got", tok)
				}
				if typ == "client_start" {
					for _, v := range data.(map[string]interface{})["sections"].([]interface{}) {
						dataChan <- v.(string)
					}
				}
			}
		}()
		c := make(chan interface{}, 256)
		var writeEnd chan<- interface{} = c
		reg <- &writeEnd
		go func() {
			var timeBuff []byte
			doPredictions := false
			for {
				var v interface{}
				select {
				case v = <-c:
					if !doPredictions { continue }
				case s, ok := <-dataChan:
					if !ok { break }
					if s == "prediction" { doPredictions = true }
					continue
				}
				type dataPart struct {
					Tag string `json:"tag"`
					Data interface{} `json:"data,omitempty"`
				}
				type packetData struct {
					Section string `json:"section"`
					Parts []dataPart `json:"parts"`
				}
				type packet struct {
					// Assume the encoder processes fields in declared order.
					Type string `json:"type"`
					Data packetData `json:"data"`
				}
				p := packet{Type: "data"}
				p.Data.Section = "prediction"
				switch v := v.(type) {
				case time.Time:
					timeBuff = v.AppendFormat(timeBuff[:0], time.RFC3339Nano)
					p.Data.Parts = []dataPart{{Tag: "reset", Data: string(timeBuff)}}
				case dataPoint:
					timeBuff = v.when.AppendFormat(timeBuff[:0], time.RFC3339Nano)
					d := make(map[string]interface{})
					d["time"] = string(timeBuff)
					if v.event != "" { d["event"] = v.event }
					d["scores"] = v.vals
					p.Data.Parts = []dataPart{{Tag: "next", Data: d}}
				}
				w, e := conn.NextWriter(websocket.TextMessage)
				if e != nil { fmt.Println(e); break }
				enc := json.NewEncoder(w)
				enc.SetEscapeHTML(false)
				e = enc.Encode(p)
				ce := w.Close()
				if e != nil { fmt.Println(e); break }
				if ce != nil { fmt.Println(ce); break }
			}
			unreg <- &writeEnd
			conn.Close()
		}()
	})
	panic(http.ListenAndServe(":8080", nil))
}
