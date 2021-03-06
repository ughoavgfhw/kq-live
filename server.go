package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
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
	_ = template.Must(assets.ParseTemplate(tpl.New(name), name+".tpl"))
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
	when  time.Time
	vals  []float64
	event string

	stats               []playerStat
	status              []struct{ Speed, Warrior bool }
	mp, winner, winType string
	dur                 time.Duration
}

func runRegistry(in <-chan interface{}, reg <-chan *chan<- interface{}, unreg <-chan *chan<- interface{}) {
	registry := make(map[*chan<- interface{}]struct{})
	for {
		select {
		case c := <-reg:
			registry[c] = struct{}{}
		case c := <-unreg:
			delete(registry, c)
		case x := <-in:
			for c, _ := range registry {
				select {
				case *c <- x:
				default: // Drop packets to a client instead of blocking the entire server
				}
			}
		}
	}
}

type gameTracker struct {
	Stop            func()
	VictoryRule     func() MatchVictoryRule
	SetVictoryRule  func(rule MatchVictoryRule)
	SwapSides       func()
	AdvanceMatch    func()
	CurrentTeams    func() (blueTeam string, goldTeam string)
	SetCurrentTeams func(blue, gold string)
	Scores          func() (blueScore int, goldScore int)
	SetScores       func(blue, gold int)
	OnDeckTeams     func() (blueTeam string, goldTeam string)
	SetOnDeckTeams  func(blue, gold string)
}

func startGameTracker(sendChangesTo chan<- interface{}) gameTracker {
	type teams struct{ blue, gold string }
	type scores struct{ blue, gold int }
	type command struct {
		cmd  int
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
				if cmd.data == nil {
					reply <- tracker.VictoryRule()
				} else {
					tracker.SetVictoryRule(cmd.data.(MatchVictoryRule))
					sendChangesTo <- tracker.VictoryRule()
				}
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
		VictoryRule: func() MatchVictoryRule {
			send <- command{0, nil}
			return (<-reply).(MatchVictoryRule)
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
		OnDeckTeams: func() (blueTeam string, goldTeam string) {
			send <- command{5, nil}
			r := (<-reply).(teams)
			return r.blue, r.gold
		},
		SetOnDeckTeams: func(blue, gold string) {
			send <- command{5, teams{blue, gold}}
		},
	}
}

type famineUpdate struct {
	berriesLeft int
	famineStart time.Time
	currTime    time.Time
}

var playerPhotoDir = http.Dir("photos")

func openPlayerPhoto(name string) (string, http.File) {
	var filename string
	var f http.File
	var err error
	for _, ext := range []string{"", ".jpg", ".png", ".gif"} {
		var fn = name + ext
		f, err = playerPhotoDir.Open(fn)
		if err == nil {
			filename = fn
			break
		}
	}
	return filename, f
}

var defaultPhotoUri string

func init() {
	name, f := openPlayerPhoto("default")
	if f != nil {
		f.Close()
		defaultPhotoUri = "/teamPictures/photo/" + name
	}
}
func getPlayerPhotoUri(name string) string {
	if n, f := openPlayerPhoto(name); f != nil {
		f.Close()
		return "/teamPictures/photo/" + url.PathEscape(n)
	}
	return defaultPhotoUri
}

type teamList []string
type playerData struct {
	Name     string `json:"name"`
	PhotoUri string `json:"photoUri,omitempty"`
	Pronouns string `json:"pronouns,omitempty"`
	Scene    string `json:"scene,omitempty"`
}

var currTeamsMu sync.Mutex
var currTeams teamList
var currPlayers map[string][]playerData

func watchTeamsFile(c chan<- interface{}) *FileWatcher {
	return WatchFile("teams.conf", func(f *os.File) {
		if f == nil {
			fmt.Println("No teams.conf file")
			c <- teamList{}
			c <- map[string][]playerData{}
			currTeamsMu.Lock()
			currTeams = currTeams[:0]
			currPlayers = make(map[string][]playerData)
			currTeamsMu.Unlock()
			return
		}
		s := bufio.NewScanner(f)
		var teams teamList
		var players = make(map[string][]playerData)
		var currTeamName string
		for s.Scan() {
			str := s.Text()
			if len(str) == 0 {
				continue
			}
			if str[0] == '\t' {
				if currTeamName == "" {
					fmt.Println("Invalid teams.conf: player data outside a team")
				} else {
					var pd playerData
					switch parts := strings.Split(str[1:], ","); true {
					case len(parts) >= 3:
						pd.Pronouns = parts[2]
						fallthrough
					case len(parts) == 2:
						pd.Scene = parts[1]
						fallthrough
					default:
						pd.Name = parts[0]
						pd.PhotoUri = getPlayerPhotoUri(parts[0])
					}
					players[currTeamName] = append(players[currTeamName], pd)
				}
			} else {
				teams = append(teams, str)
				currTeamName = str
				// Preallocate the memory, and make sure there is an entry in
				// the map even if there is no player info for the team.
				players[currTeamName] = make([]playerData, 0, 5)
			}
		}
		if err := s.Err(); err != nil {
			fmt.Printf("Error reading teams: %v\n", err)
			return
		}
		fmt.Printf("Loaded %v teams\n", len(teams))
		c <- teams
		c <- players
		currTeamsMu.Lock()
		currTeams = teams
		currPlayers = players
		currTeamsMu.Unlock()
	})
}

func handleWSIncoming(r io.Reader, dataChan chan<- string, tracker gameTracker) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var tok json.Token
	var err error
	// TODO: Send back an error on parse error, handle inputs.
	if tok, err = dec.Token(); err != nil {
		fmt.Println("failed to parse message;", err)
		return
	} else if v, ok := tok.(json.Delim); !ok {
		fmt.Println("failed to parse message;", err)
		return
	} else if v != '{' {
		fmt.Println("failed to parse message;", err)
		return
	}
	if tok, err = dec.Token(); err != nil {
		fmt.Println("failed to parse message;", err)
		return
	} else if v, ok := tok.(string); !ok {
		fmt.Println("failed to parse message;", err)
		return
	} else if v != "type" {
		fmt.Println("failed to parse message;", err)
		return
	}
	var typ string
	if tok, err = dec.Token(); err != nil {
		fmt.Println("failed to parse message;", err)
		return
	} else if v, ok := tok.(string); !ok {
		fmt.Println("failed to parse message;", err)
		return
	} else {
		typ = v
	}
	if tok, err = dec.Token(); err != nil {
		fmt.Println("failed to parse message;", err)
		return
	} else if v, ok := tok.(string); !ok {
		fmt.Println("failed to parse message;", err)
		return
	} else if v != "data" {
		fmt.Println("failed to parse message;", err)
		return
	}
	// TODO: Parse an expected structure based on type.
	var data interface{}
	if err = dec.Decode(&data); err != nil {
		fmt.Println("failed to parse message;", err)
		return
	}
	if tok, err = dec.Token(); err != nil {
		fmt.Println("failed to parse message;", err)
		return
	} else if v, ok := tok.(json.Delim); !ok {
		fmt.Println("failed to parse message;", err)
		return
	} else if v != '}' {
		fmt.Println("failed to parse message;", err)
		return
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
		if m["section"].(string) != "control" {
			break
		}
		for _, part := range m["parts"].([]interface{}) {
			tag := part.(map[string]interface{})["tag"]
			d := part.(map[string]interface{})["data"]
			switch tag {
			case "advanceMatch":
				tracker.AdvanceMatch()
			case "reset":
				for _, p := range d.([]interface{}) {
					switch p {
					case "matchSettings":
						tracker.SetVictoryRule(BestOfN(0))
					case "currentTeams":
						tracker.SetCurrentTeams("", "")
					case "currentScores":
						tracker.SetScores(0, 0)
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
				if rule == nil {
					break
				}
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

func startWebServer(bindAddr string, dataSource <-chan interface{}) {
	mixed := make(chan interface{})
	tracker := startGameTracker(mixed)
	go func() {
		for v := range dataSource {
			mixed <- v
			// huge hack to have this here
			switch dp, _ := v.(dataPoint); dp.winner {
			case "":
				break
			case "blue":
				b, g := tracker.Scores()
				tracker.SetScores(b+1, g)
			case "gold":
				b, g := tracker.Scores()
				tracker.SetScores(b, g+1)
			}
		}
	}()

	defer watchTeamsFile(mixed).Close()

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
			if err != nil {
				panic(err)
			}
			io.Copy(w, content)
			return
		}
		if err != nil {
			panic(err)
		}
		var modtime time.Time
		if info, err := content.Stat(); err != nil {
			modtime = info.ModTime()
		}
		http.ServeContent(w, req, "index.html", modtime, content)
	})
	http.HandleFunc("/control/scores", func(w http.ResponseWriter, req *http.Request) {
		// TODO: Serve the gzip-encoded form if available.
		content, err := assets.FS.Open("/score_control.html")
		if err != nil {
			panic(err)
		}
		var modtime time.Time
		if info, err := content.Stat(); err != nil {
			modtime = info.ModTime()
		}
		http.ServeContent(w, req, "score_control.html", modtime, content)
	})
	scoreboardTpl := requireTemplate("scoreboard", http.Dir("config"))
	http.HandleFunc("/scoreboard", func(w http.ResponseWriter, rep *http.Request) {
		err := scoreboardTpl.Execute(w, map[string]interface{}{"GoldOnLeft": false})
		if err != nil {
			panic(err)
		}
	})
	statsTpl := requireTemplate("stats", assets.FS)
	http.HandleFunc("/stats", func(w http.ResponseWriter, rep *http.Request) {
		err := statsTpl.Execute(w, nil)
		if err != nil {
			panic(err)
		}
	})
	statsboardTpl := requireTemplate("statsboard", assets.FS)
	http.HandleFunc("/statsboard/", func(w http.ResponseWriter, req *http.Request) {
		var side string
		switch req.URL.Path {
		case "/statsboard/blue", "/statsboard/blue/":
			side = "blue"
		case "/statsboard/gold", "/statsboard/gold/":
			side = "gold"
		default:
			panic(req.URL)
		}
		err := statsboardTpl.Execute(w, map[string]interface{}{"Side": side})
		if err != nil {
			panic(err)
		}
	})
	famineTpl := requireTemplate("famine", assets.FS)
	http.HandleFunc("/famineTracker", func(w http.ResponseWriter, req *http.Request) {
		err := famineTpl.Execute(w, nil)
		if err != nil {
			panic(err)
		}
	})
	teamPicsTpl := requireTemplate("team_pictures", assets.FS)
	http.HandleFunc("/teamPictures", func(w http.ResponseWriter, req *http.Request) {
		err := teamPicsTpl.Execute(w, map[string]interface{}{"GoldOnLeft": false, "DefaultPlayerPhoto": nil})
		if err != nil {
			panic(err)
		}
	})
	postGameStatsTpl := requireTemplate("post_game_stats", assets.FS)
	http.HandleFunc("/postGameStats", func(w http.ResponseWriter, req *http.Request) {
		err := postGameStatsTpl.Execute(w, map[string]interface{}{"GoldOnLeft": false})
		if err != nil {
			panic(err)
		}
	})
	statusTpl := requireTemplate("status", assets.FS)
	http.HandleFunc("/status", func(w http.ResponseWriter, req *http.Request) {
		err := statusTpl.Execute(w, map[string]interface{}{"GoldOnLeft": false, "DefaultPlayerPhoto": nil})
		if err != nil {
			panic(err)
		}
	})
	http.HandleFunc("/teamPictures/photo/", func(w http.ResponseWriter, req *http.Request) {
		name, _ := url.PathUnescape(req.URL.EscapedPath()[20:])
		fn, f := openPlayerPhoto(name)
		if f == nil {
			http.NotFound(w, req)
			return
		}
		var modtime time.Time
		if info, err := f.Stat(); err != nil {
			modtime = info.ModTime()
		}
		http.ServeContent(w, req, fn, modtime, f)
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
				switch v := v.(type) {
				case time.Time:
					w, e := conn.NextWriter(websocket.TextMessage)
					if e != nil {
						fmt.Println(e)
						break
					}
					timeBuff = v.AppendFormat(timeBuff[:0], time.RFC3339Nano)
					_, e = fmt.Fprintf(w, "reset,%s,6", timeBuff)
					ce := w.Close()
					if e != nil {
						fmt.Println(e)
						break
					}
					if ce != nil {
						fmt.Println(ce)
						break
					}
				case dataPoint:
					w, e := conn.NextWriter(websocket.TextMessage)
					if e != nil {
						fmt.Println(e)
						break
					}
					timeBuff = v.when.AppendFormat(timeBuff[:0], time.RFC3339Nano)
					_, e = fmt.Fprintf(w, "next,%s,%s", timeBuff, v.event)
					for _, val := range v.vals {
						_, e = fmt.Fprintf(w, ",%v", val)
					}
					ce := w.Close()
					if e != nil {
						fmt.Println(e)
						break
					}
					if ce != nil {
						fmt.Println(ce)
						break
					}
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
				handleWSIncoming(r, dataChan, tracker)
			}
		}()
		c := make(chan interface{}, 256)
		var writeEnd chan<- interface{} = c
		reg <- &writeEnd
		go func() {
			defer func() {
				unreg <- &writeEnd
				conn.Close()
			}()
			var timeBuff []byte
			doPredictions := false
			doControl := false
			doCurrentMatch := false
			doFamineUpdates := false
			doTournamentData := false
			for {
				var v interface{}
				select {
				case v = <-c:
				case s, ok := <-dataChan:
					if !ok {
						return
					}
					switch s {
					case "prediction":
						doPredictions = true
					case "control":
						doControl = true
						// Send current state. Definitely a hack but it works for now.
						go func() {
							c <- tracker.VictoryRule()
							var ms MatchScores
							ms.TeamA, ms.TeamB = tracker.CurrentTeams()
							ms.ScoreA, ms.ScoreB = tracker.Scores()
							c <- &ms
							currTeamsMu.Lock()
							c <- currTeams
							currTeamsMu.Unlock()
						}()
					case "currentMatch":
						doCurrentMatch = true
						// Send current state. Definitely a hack but it works for now.
						go func() {
							c <- tracker.VictoryRule()
							var ms MatchScores
							ms.TeamA, ms.TeamB = tracker.CurrentTeams()
							ms.ScoreA, ms.ScoreB = tracker.Scores()
							c <- &ms
						}()
					case "famineTracker":
						doFamineUpdates = true
					case "tournamentData":
						doTournamentData = true
						go func() {
							currTeamsMu.Lock()
							c <- currPlayers
							currTeamsMu.Unlock()
						}()
					}
					continue
				}
				type dataPart struct {
					Tag  string      `json:"tag"`
					Data interface{} `json:"data,omitempty"`
				}
				type packetData struct {
					Section string     `json:"section"`
					Parts   []dataPart `json:"parts"`
				}
				type packet struct {
					// Assume the encoder processes fields in declared order.
					Type string     `json:"type"`
					Data packetData `json:"data"`
				}
				p := packet{Type: "data"}
				switch v := v.(type) {
				case time.Time:
					if !doPredictions {
						continue
					}
					p.Data.Section = "prediction"
					timeBuff = v.AppendFormat(timeBuff[:0], time.RFC3339Nano)
					p.Data.Parts = []dataPart{{Tag: "reset", Data: string(timeBuff)}}
				case dataPoint:
					if !doPredictions {
						continue
					}
					p.Data.Section = "prediction"
					timeBuff = v.when.AppendFormat(timeBuff[:0], time.RFC3339Nano)
					d := make(map[string]interface{})
					d["time"] = string(timeBuff)
					if v.event != "" {
						d["event"] = v.event
					}
					d["scores"] = v.vals
					s := make(map[string]interface{})
					s["stats"] = v.stats
					s["status"] = v.status
					s["map"] = v.mp
					s["duration"] = v.dur
					if v.winner != "" {
						s["winner"] = v.winner
						s["winType"] = v.winType
					}
					p.Data.Parts = []dataPart{{Tag: "next", Data: d},
						{Tag: "stats", Data: s}}
				case MatchVictoryRule:
					var tag string
					if doControl {
						p.Data.Section = "control"
						tag = "matchSettings"
					} else if doCurrentMatch {
						p.Data.Section = "currentMatch"
						tag = "settings"
					} else {
						continue
					}
					type ds struct {
						VictoryRule struct {
							Rule   string `json:"rule"`
							Length int    `json:"length"`
						} `json:"victoryRule"`
					}
					var d ds
					switch vr := v.(type) {
					case BestOfN:
						d.VictoryRule.Rule = "BestOfN"
						d.VictoryRule.Length = int(vr)
					case StraightN:
						d.VictoryRule.Rule = "StraightN"
						d.VictoryRule.Length = int(vr)
					}
					p.Data.Parts = []dataPart{{Tag: tag, Data: d}}
				case *MatchScores:
					//!!!!: Race condition here, as the MatchScores are being updated in a different goroutine. So far it seems to have been fine but this should be fixed when redesigning.
					var teamTag, scoreTag string
					if doControl {
						p.Data.Section = "control"
						teamTag, scoreTag = "currentTeams", "currentScores"
					} else if doCurrentMatch {
						p.Data.Section = "currentMatch"
						teamTag, scoreTag = "teams", "scores"
					} else {
						continue
					}
					// TODO: Currently assuming A is blue
					t := make(map[string]interface{})
					t["blue"] = v.TeamA
					t["gold"] = v.TeamB
					s := make(map[string]interface{})
					s["blue"] = v.ScoreA
					s["gold"] = v.ScoreB
					p.Data.Parts = []dataPart{{Tag: teamTag, Data: t},
						{Tag: scoreTag, Data: s}}

				case teamList:
					if !doControl {
						continue
					}
					p.Data.Section = "control"
					p.Data.Parts = []dataPart{{Tag: "teamList", Data: v}}

				case map[string][]playerData:
					if !doTournamentData {
						continue
					}
					p.Data.Section = "tournamentData"
					p.Data.Parts = []dataPart{{Tag: "teams", Data: v}}

				case famineUpdate:
					if !doFamineUpdates {
						continue
					}
					p.Data.Section = "famineTracker"
					d := map[string]interface{}{
						"berriesLeft": v.berriesLeft,
						"inFamine":    !v.famineStart.IsZero(),
					}
					if !v.famineStart.IsZero() {
						dur := 90*time.Second - v.currTime.Sub(v.famineStart)
						if dur < 0 {
							dur = 0
						}
						d["famineLeftSeconds"] = float64(dur) / float64(time.Second)
					}
					p.Data.Parts = []dataPart{{Tag: "update", Data: d}}
				}
				w, e := conn.NextWriter(websocket.TextMessage)
				if e != nil {
					fmt.Println(e)
					break
				}
				enc := json.NewEncoder(w)
				enc.SetEscapeHTML(false)
				e = enc.Encode(p)
				ce := w.Close()
				if e != nil {
					fmt.Println(e)
					break
				}
				if ce != nil {
					fmt.Println(ce)
					break
				}
			}
		}()
	})
	panic(http.ListenAndServe(bindAddr, nil))
}
