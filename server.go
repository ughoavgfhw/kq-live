package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/websocket"
	kq "github.com/ughoavgfhw/libkq/common"

	"github.com/ughoavgfhw/kq-live/assets"
)

func requireTemplate(name string, configFS http.FileSystem) *template.Template {
	// The config can load assets from configFS, and the functions are always
	// associated with the top-level template, which means the configFS must be
	// used by all of them.
	tpl := template.Must(assets.LoadTemplate("/base.tpl", configFS))
	_ = template.Must(assets.ParseTemplate(tpl.New(name), name + ".tpl"))
	// The config must only be added after the main template, since it may
	// override the defaults.
	if config, err := configFS.Open("/config.tpl"); err == nil {
		_ = template.Must(assets.ParseTemplateFile(tpl.New("config"), config))
	} else {
		if os.IsNotExist(err) {
			fmt.Println("No config found for ", name)
		} else {
			panic(err)
		}
	}
	return tpl
}

type dataPoint struct {
	when time.Time
	vals []float64
	event string

	stats []playerStat
	mp, winner, winType string
	dur time.Duration
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

type gameTracker struct {
	Stop func()
	SetVictoryRule func(rule MatchVictoryRule)
	SwapSides func()
	AdvanceMatch func()
	CurrentTeams func() (blueTeam string, goldTeam string)
	SetCurrentTeams func(blue, gold string)
	Scores func() (blueScore int, goldScore int)
	SetScores func(blue, gold int)
	OnDeckTeams func() (blueTeam string, goldTeam string)
	SetOnDeckTeams func(blue, gold string)
}

func startGameTracker(sendChangesTo chan<- interface{}) gameTracker {
	type teams struct { blue, gold string }
	type scores struct { blue, gold int }
	type command struct {
		cmd int
		data interface{}
	}
	send := make(chan command)
	reply := make(chan interface{})

	go func() {
		defer close(reply)
		tracker := StartUnstructuredPlay(BestOfN(0))
		for cmd := range send {
			switch cmd.cmd {
			case 0:
				tracker.SetVictoryRule(cmd.data.(MatchVictoryRule))
				sendChangesTo <- tracker.VictoryRule()
			case 1:
				tracker.SwapSides()
				sendChangesTo <- tracker.CurrentMatch()
			case 2:
				prev := tracker.CurrentMatch()
				tracker.AdvanceMatch()
				next := tracker.CurrentMatch()
				sendChangesTo <- next
				switch true {
				case prev.ScoreA > prev.ScoreB:
					fmt.Printf("%s defeats %s, %d-%d\n", prev.TeamA, prev.TeamB, prev.ScoreA, prev.ScoreB)
				case prev.ScoreA < prev.ScoreB:
					fmt.Printf("%s defeats %s, %d-%d\n", prev.TeamB, prev.TeamA, prev.ScoreB, prev.ScoreA)
				default:
					fmt.Printf("%s and %s tie, %d-%d\n", prev.TeamA, prev.TeamB, prev.ScoreA, prev.ScoreB)
				}
				if next.TeamA != "" || next.TeamB != "" {
					fmt.Printf("Up next: %s vs %s\n", next.TeamA, next.TeamB)
				}
			case 3:
				ms := tracker.CurrentMatch()
				if cmd.data == nil {
					if tracker.TeamASide() == kq.BlueSide {
						reply <- teams{ms.TeamA, ms.TeamB}
					} else {
						reply <- teams{ms.TeamB, ms.TeamA}
					}
				} else {
					t := cmd.data.(teams)
					if tracker.TeamASide() == kq.BlueSide {
						ms.TeamA, ms.TeamB = t.blue, t.gold
					} else {
						ms.TeamB, ms.TeamA = t.blue, t.gold
					}
					sendChangesTo <- ms
				}
			case 4:
				ms := tracker.CurrentMatch()
				if cmd.data == nil {
					if tracker.TeamASide() == kq.BlueSide {
						reply <- scores{ms.ScoreA, ms.ScoreB}
					} else {
						reply <- scores{ms.ScoreB, ms.ScoreA}
					}
				} else {
					s := cmd.data.(scores)
					if tracker.TeamASide() == kq.BlueSide {
						ms.ScoreA, ms.ScoreB = s.blue, s.gold
					} else {
						ms.ScoreB, ms.ScoreA = s.blue, s.gold
					}
					sendChangesTo <- ms
				}
			case 5:
				ms := tracker.UpcomingMatch(0)
				if cmd.data == nil {
					if tracker.TeamASide() == kq.BlueSide {
						reply <- teams{ms.TeamA, ms.TeamB}
					} else {
						reply <- teams{ms.TeamB, ms.TeamA}
					}
				} else {
					t := cmd.data.(teams)
					if tracker.TeamASide() == kq.BlueSide {
						ms.TeamA, ms.TeamB = t.blue, t.gold
					} else {
						ms.TeamB, ms.TeamA = t.blue, t.gold
					}
				}
			}
		}
	}()

	return gameTracker{
		Stop: func() {
			close(send)
			<-reply
		},
		SetVictoryRule: func(rule MatchVictoryRule) {
			send <- command{0, rule}
		},
		SwapSides: func() {
			send <- command{1, nil}
		},
		AdvanceMatch: func() {
			send <- command{2, nil}
		},
		CurrentTeams: func() (blueTeam string, goldTeam string) {
			send <- command{3, nil}
			r := (<-reply).(teams)
			return r.blue, r.gold
		},
		SetCurrentTeams: func(blue, gold string) {
			send <- command{3, teams{blue, gold}}
		},
		Scores: func() (blueScore int, goldScore int) {
			send <- command{4, nil}
			r := (<-reply).(scores)
			return r.blue, r.gold
		},
		SetScores: func(blue, gold int) {
			send <- command{4, scores{blue, gold}}
		},
		OnDeckTeams: func() (blueTeam string, goldTeam string)	 {
			send <- command{5, nil}
			r := (<-reply).(teams)
			return r.blue, r.gold
		},
		SetOnDeckTeams: func(blue, gold string) {
			send <- command{5, teams{blue, gold}}
		},
	}
}

func startWebServer(dataSource <-chan interface{}) {
	mixed := make(chan interface{})
	go func() {
		for v := range dataSource {
			mixed <- v
		}
	}()
	tracker := startGameTracker(mixed)
	_ = tracker

	reg := make(chan *chan<- interface{}, 1)
	unreg := make(chan *chan<- interface{})
	go runRegistry(mixed, reg, unreg)
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
	statsTpl := requireTemplate("stats", assets.FS)
	http.HandleFunc("/stats", func(w http.ResponseWriter, rep *http.Request) {
		err := statsTpl.Execute(w, nil)
		if err != nil { panic(err) }
	})
	statsboardTpl := requireTemplate("statsboard", assets.FS)
	http.HandleFunc("/statsboard/", func(w http.ResponseWriter, req *http.Request) {
		var side string
		switch req.URL.Path {
			case "/statsboard/blue", "/statsboard/blue/": side = "blue"
			case "/statsboard/gold", "/statsboard/gold/": side = "gold"
			default: panic(req.URL)
		}
		err := statsboardTpl.Execute(w, map[string]interface{}{"Side": side})
		if err != nil { panic(err) }
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
					_, e = fmt.Fprintf(w, "reset,%s,5", timeBuff)
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
				switch typ {
				case "client_start":
					for _, v := range data.(map[string]interface{})["sections"].([]interface{}) {
						dataChan <- v.(string)
					}
				case "data":
					m := data.(map[string]interface{})
					if m["section"].(string) != "control" { break }
					for _, part := range m["parts"].([]interface{}) {
						tag := part.(map[string]interface{})["tag"]
						d := part.(map[string]interface{})["data"]
						switch tag {
						case "advanceMatch": tracker.AdvanceMatch()
						case "reset":
							for _, p := range d.([]interface{}) {
								switch p {
								case "matchSettings": tracker.SetVictoryRule(BestOfN(0))
								case "currentTeams": tracker.SetCurrentTeams("", "")
								case "currentScores": tracker.SetScores(0, 0)
								}
							}
						case "matchSettings":
							vr := d.(map[string]interface{})["victoryRule"]
							if vr == nil {
								tracker.SetVictoryRule(BestOfN(0))
								break
							}
							var rule MatchVictoryRule
							switch vr.(map[string]interface{})["rule"] {
							case "BestOfN":
								rule = BestOfN(vr.(map[string]interface{})["length"].(float64))
							case "StraightN":
								rule = StraightN(vr.(map[string]interface{})["length"].(float64))
							}
							if rule == nil { break }
							tracker.SetVictoryRule(rule)
						case "currentTeams":
							tracker.SetCurrentTeams(
								d.(map[string]interface{})["blue"].(string),
								d.(map[string]interface{})["gold"].(string))
						case "currentScores":
							tracker.SetScores(
								int(d.(map[string]interface{})["blue"].(float64)),
								int(d.(map[string]interface{})["gold"].(float64)))
						}
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
					s := make(map[string]interface{})
					s["stats"] = v.stats
					s["map"] = v.mp
					s["duration"] = v.dur
					if v.winner != "" {
						s["winner"] = v.winner
						s["winType"] = v.winType
					}
					p.Data.Parts = []dataPart{{Tag: "next", Data: d},
											  {Tag: "stats", Data: s}}
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
