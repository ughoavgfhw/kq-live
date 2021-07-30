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

type serverEventKey int

const (
	ScoreUpdateKey serverEventKey = iota
	TeamUpdateKey
	VictoryRuleKey
	TeamListKey
	PlayerDataKey
)

type ScoreUpdate struct {
	Blue int
	Gold int
}

type TeamUpdate struct {
	Blue string
	Gold string
}

type ControlCommandType int

type ControlCommand struct {
	Type ControlCommandType
	Data interface{}
}

const (
	InvalidControlCommand ControlCommandType = iota
	// Note: ClientStartRequest must always be the only command in an event.
	ClientStartRequest // Data is ClientStartOptions
	AdvanceMatch       // Data is nil
	SetVictoryRule     // Data is MatchVictoryRule
	SetCurrentTeams    // Data is TeamUpdate
	SetScores          // Data is ScoreUpdate
	SetTeamList        // Data is teamList
	SetPlayerData      // Data is map[string][]playerData
)

type ClientStartOptions struct {
	ClientIdentifier *chan<- interface{} // The registration token.
	Sections         map[string]bool
}

func runRegistry(in <-chan interface{}, reg <-chan *chan<- interface{}, unreg <-chan *chan<- interface{}) {
	singleRecipient := func(x interface{}) *chan<- interface{} {
		e, ok := x.(*Event)
		if !ok {
			return nil
		}
		// Currently only client start request events have single recipients.
		cmd, ok := e.Data[ControlCommandKey].([]ControlCommand)
		if e.Type == ControlEvent && ok && len(cmd) == 1 && cmd[0].Type == ClientStartRequest {
			return cmd[0].Data.(ClientStartOptions).ClientIdentifier
		}
		return nil
	}

	registry := make(map[*chan<- interface{}]struct{})
	for {
		select {
		case c := <-reg:
			registry[c] = struct{}{}
		case c := <-unreg:
			delete(registry, c)
		case x := <-in:
			if r := singleRecipient(x); r == nil {
				for c, _ := range registry {
					select {
					case *c <- x:
					default: // Drop packets to a client instead of blocking the entire server
					}
				}
			} else if _, ok := registry[r]; ok {
				// This event should only be sent to a single recipient. It should not be dropped unless that recipient is unregistered.
				*r <- x // Blocking
			}
		}
	}
}

type gameTracker struct {
	Stop            func()
	VictoryRule     func() MatchVictoryRule
	SetVictoryRule  func(rule MatchVictoryRule, event *Event)
	SwapSides       func(event *Event)
	AdvanceMatch    func(event *Event)
	CurrentTeams    func() (blueTeam string, goldTeam string)
	SetCurrentTeams func(blue, gold string, event *Event)
	Scores          func() (blueScore int, goldScore int)
	SetScores       func(blue, gold int, event *Event)
	OnDeckTeams     func() (blueTeam string, goldTeam string)
	SetOnDeckTeams  func(blue, gold string)
}

func startGameTracker() gameTracker {
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
				}
			case 1:
				tracker.SwapSides()
				match := tracker.CurrentMatch()
				if event := cmd.data.(*Event); event != nil {
					if tracker.TeamASide() == kq.BlueSide {
						event.Data[TeamUpdateKey] = TeamUpdate{
							Blue: match.TeamA,
							Gold: match.TeamB,
						}
						event.Data[ScoreUpdateKey] = ScoreUpdate{
							Blue: match.ScoreA,
							Gold: match.ScoreB,
						}
					} else {
						event.Data[TeamUpdateKey] = TeamUpdate{
							Blue: match.TeamB,
							Gold: match.TeamA,
						}
						event.Data[ScoreUpdateKey] = ScoreUpdate{
							Blue: match.ScoreB,
							Gold: match.ScoreA,
						}
					}
					reply <- event // Just to indicate we are done with the synchronized section.
				}
			case 2:
				prev := tracker.CurrentMatch()
				tracker.AdvanceMatch()
				next := tracker.CurrentMatch()
				if event := cmd.data.(*Event); event != nil {
					event.Data[VictoryRuleKey] = tracker.VictoryRule()
					if tracker.TeamASide() == kq.BlueSide {
						event.Data[TeamUpdateKey] = TeamUpdate{
							Blue: next.TeamA,
							Gold: next.TeamB,
						}
						event.Data[ScoreUpdateKey] = ScoreUpdate{
							Blue: next.ScoreA,
							Gold: next.ScoreB,
						}
					} else {
						event.Data[TeamUpdateKey] = TeamUpdate{
							Blue: next.TeamB,
							Gold: next.TeamA,
						}
						event.Data[ScoreUpdateKey] = ScoreUpdate{
							Blue: next.ScoreB,
							Gold: next.ScoreA,
						}
					}
					reply <- event // Just to indicate we are done with the synchronized section.
				}

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
		SetVictoryRule: func(rule MatchVictoryRule, event *Event) {
			send <- command{0, rule}
			if event != nil {
				event.Data[VictoryRuleKey] = rule
			}
		},
		SwapSides: func(event *Event) {
			send <- command{1, event}
			if event != nil {
				<-reply
			}
		},
		AdvanceMatch: func(event *Event) {
			send <- command{2, event}
			if event != nil {
				<-reply
			}
		},
		CurrentTeams: func() (blueTeam string, goldTeam string) {
			send <- command{3, nil}
			r := (<-reply).(teams)
			return r.blue, r.gold
		},
		SetCurrentTeams: func(blue, gold string, event *Event) {
			send <- command{3, teams{blue, gold}}
			if event != nil {
				event.Data[TeamUpdateKey] = TeamUpdate{blue, gold}
			}
		},
		Scores: func() (blueScore int, goldScore int) {
			send <- command{4, nil}
			r := (<-reply).(scores)
			return r.blue, r.gold
		},
		SetScores: func(blue, gold int, event *Event) {
			send <- command{4, scores{blue, gold}}
			if event != nil {
				event.Data[ScoreUpdateKey] = ScoreUpdate{blue, gold}
			}
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

func watchTeamsFile(eventOutput EventStream) *FileWatcher {
	return WatchFile("teams.conf", func(f *os.File) {
		var teams teamList
		var players = make(map[string][]playerData)
		defer func() {
			eventOutput.AddEvent(NewControlEvent([]ControlCommand{
				{Type: SetTeamList, Data: teams},
				{Type: SetPlayerData, Data: players},
			}))
		}()
		if f == nil {
			fmt.Println("No teams.conf file")
			return
		}
		s := bufio.NewScanner(f)
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
	})
}

func handleWSIncoming(r io.Reader, registration *chan<- interface{}, eventOutput EventStream) {
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
		s := data.(map[string]interface{})["sections"].([]interface{})
		sections := make(map[string]bool)
		for _, v := range s {
			sections[v.(string)] = true
		}
		eventOutput.AddEvent(NewControlEvent([]ControlCommand{{
			Type: ClientStartRequest,
			Data: ClientStartOptions{
				ClientIdentifier: registration,
				Sections:         sections,
			},
		}}))
	case "data":
		m := data.(map[string]interface{})
		if m["section"].(string) != "control" {
			break
		}
		var commands []ControlCommand
		for _, part := range m["parts"].([]interface{}) {
			tag := part.(map[string]interface{})["tag"]
			d := part.(map[string]interface{})["data"]
			switch tag {
			case "advanceMatch":
				commands = append(commands, ControlCommand{AdvanceMatch, nil})
			case "reset":
				for _, p := range d.([]interface{}) {
					switch p {
					case "matchSettings":
						commands = append(commands, ControlCommand{SetVictoryRule, BestOfN(0)})
					case "currentTeams":
						commands = append(commands, ControlCommand{SetCurrentTeams, TeamUpdate{"", ""}})
					case "currentScores":
						commands = append(commands, ControlCommand{SetScores, ScoreUpdate{0, 0}})
					}
				}
			case "matchSettings":
				vr := d.(map[string]interface{})["victoryRule"]
				if vr == nil {
					commands = append(commands, ControlCommand{SetVictoryRule, BestOfN(0)})
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
				commands = append(commands, ControlCommand{SetVictoryRule, rule})
			case "currentTeams":
				commands = append(commands, ControlCommand{SetCurrentTeams, TeamUpdate{
					d.(map[string]interface{})["blue"].(string),
					d.(map[string]interface{})["gold"].(string),
				}})
			case "currentScores":
				commands = append(commands, ControlCommand{SetScores, ScoreUpdate{
					int(d.(map[string]interface{})["blue"].(float64)),
					int(d.(map[string]interface{})["gold"].(float64)),
				}})
			}
		}
		if len(commands) > 0 {
			eventOutput.AddEvent(NewControlEvent(commands))
		}
	}
}

func startWebServer(bindAddr string, eventStream EventStream) {
	mixed := make(chan interface{})
	tracker := startGameTracker()
	go func() {
		var currTeams teamList
		var currPlayers map[string][]playerData
		var e *Event
		for e = eventStream.Next(); e != nil; e = eventStream.Next() {
			switch e.Type {
			case CabMessageEvent:
				if dp := e.Data[StatsUpdateKey]; dp != nil {
					switch dp.(dataPoint).winner {
					case "blue":
						b, g := tracker.Scores()
						tracker.SetScores(b+1, g, e)
					case "gold":
						b, g := tracker.Scores()
						tracker.SetScores(b, g+1, e)
					}
				}

			case ControlEvent:
				for _, command := range e.Data[ControlCommandKey].([]ControlCommand) {
					switch command.Type {
					case AdvanceMatch:
						tracker.AdvanceMatch(e)
					case SetVictoryRule:
						tracker.SetVictoryRule(command.Data.(MatchVictoryRule), e)
					case SetCurrentTeams:
						update := command.Data.(TeamUpdate)
						tracker.SetCurrentTeams(update.Blue, update.Gold, e)
					case SetScores:
						update := command.Data.(ScoreUpdate)
						tracker.SetScores(update.Blue, update.Gold, e)

					case SetTeamList:
						currTeams = command.Data.(teamList)
						e.Data[TeamListKey] = currTeams

					case SetPlayerData:
						currPlayers = command.Data.(map[string][]playerData)
						e.Data[PlayerDataKey] = currPlayers

					case ClientStartRequest:
						sections := command.Data.(ClientStartOptions).Sections
						// Attach current state.
						if sections["control"] || sections["currentMatch"] {
							e.Data[VictoryRuleKey] = tracker.VictoryRule()
							blueTeam, goldTeam := tracker.CurrentTeams()
							e.Data[TeamUpdateKey] = TeamUpdate{blueTeam, goldTeam}
							blueScore, goldScore := tracker.Scores()
							e.Data[ScoreUpdateKey] = ScoreUpdate{blueScore, goldScore}
						}
						if sections["control"] {
							e.Data[TeamListKey] = currTeams
						}
						if sections["tournamentData"] {
							e.Data[PlayerDataKey] = currPlayers
						}
					}
				}
			}
			mixed <- e
		}
	}()

	defer watchTeamsFile(eventStream).Close()

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
				ev, ok := v.(*Event)
				if !ok {
					continue
				}
				if t, ok := ev.Data[GameStartTimeKey].(time.Time); ok {
					w, e := conn.NextWriter(websocket.TextMessage)
					if e != nil {
						fmt.Println(e)
						break
					}
					timeBuff = t.AppendFormat(timeBuff[:0], time.RFC3339Nano)
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
				}
				if dp, ok := ev.Data[StatsUpdateKey].(dataPoint); ok {
					w, e := conn.NextWriter(websocket.TextMessage)
					if e != nil {
						fmt.Println(e)
						break
					}
					timeBuff = dp.when.AppendFormat(timeBuff[:0], time.RFC3339Nano)
					_, e = fmt.Fprintf(w, "next,%s,%s", timeBuff, dp.event)
					for _, val := range dp.vals {
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
		shutdown := make(chan struct{})
		c := make(chan interface{}, 256)
		var writeEnd chan<- interface{} = c
		reg <- &writeEnd
		go func() {
			for {
				_, r, err := conn.NextReader()
				if err != nil {
					fmt.Println(err)
					close(shutdown)
					break
				}
				handleWSIncoming(r, &writeEnd, eventStream)
			}
		}()
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
				case _, ok := <-shutdown:
					if !ok {
						return
					}
					continue
				}
				if e, ok := v.(*Event); ok {
					cmd, ok := e.Data[ControlCommandKey].([]ControlCommand)
					if e.Type == ControlEvent && ok && len(cmd) == 1 && cmd[0].Type == ClientStartRequest && cmd[0].Data.(ClientStartOptions).ClientIdentifier == &writeEnd {
						for s := range cmd[0].Data.(ClientStartOptions).Sections {
							switch s {
							case "prediction":
								doPredictions = true
							case "control":
								doControl = true
							case "currentMatch":
								doCurrentMatch = true
							case "famineTracker":
								doFamineUpdates = true
							case "tournamentData":
								doTournamentData = true
							}
						}
					}
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
				send := func(p *packet) {
					defer func() { p.Data.Parts = p.Data.Parts[:0] }()
					w, e := conn.NextWriter(websocket.TextMessage)
					if e != nil {
						fmt.Println(e)
						return
					}
					enc := json.NewEncoder(w)
					enc.SetEscapeHTML(false)
					e = enc.Encode(p)
					ce := w.Close()
					if e != nil {
						fmt.Println(e)
						return
					}
					if ce != nil {
						fmt.Println(ce)
						return
					}
				}
				p := packet{Type: "data"}
				switch v := v.(type) {
				case *Event:
					if doPredictions {
						p.Data.Section = "prediction"
						if t, ok := v.Data[GameStartTimeKey].(time.Time); ok {
							timeBuff = t.AppendFormat(timeBuff[:0], time.RFC3339Nano)
							p.Data.Parts = append(p.Data.Parts, dataPart{Tag: "reset", Data: string(timeBuff)})
						}
						if dp, ok := v.Data[StatsUpdateKey].(dataPoint); ok {
							timeBuff = dp.when.AppendFormat(timeBuff[:0], time.RFC3339Nano)
							d := make(map[string]interface{})
							d["time"] = string(timeBuff)
							if dp.event != "" {
								d["event"] = dp.event
							}
							d["scores"] = dp.vals
							s := make(map[string]interface{})
							s["stats"] = dp.stats
							s["status"] = dp.status
							s["map"] = dp.mp
							s["duration"] = dp.dur
							if dp.winner != "" {
								s["winner"] = dp.winner
								s["winType"] = dp.winType
							}
							p.Data.Parts = append(p.Data.Parts,
								dataPart{Tag: "next", Data: d},
								dataPart{Tag: "stats", Data: s})
						}
						if len(p.Data.Parts) > 0 {
							send(&p)
						}
					}

					if doFamineUpdates {
						p.Data.Section = "famineTracker"
						if fu, ok := v.Data[FamineUpdateKey].(FamineUpdate); ok {
							d := map[string]interface{}{
								"berriesLeft": fu.BerriesLeft,
								"inFamine":    !fu.FamineStart.IsZero(),
							}
							if !fu.FamineStart.IsZero() {
								dur := 90*time.Second - fu.CurrTime.Sub(fu.FamineStart)
								if dur < 0 {
									dur = 0
								}
								d["famineLeftSeconds"] = float64(dur) / float64(time.Second)
							}
							p.Data.Parts = append(p.Data.Parts, dataPart{Tag: "update", Data: d})
						}
						if len(p.Data.Parts) > 0 {
							send(&p)
						}
					}

					if doControl {
						p.Data.Section = "control"
						if vr, ok := v.Data[VictoryRuleKey].(MatchVictoryRule); ok {
							type ds struct {
								VictoryRule struct {
									Rule   string `json:"rule"`
									Length int    `json:"length"`
								} `json:"victoryRule"`
							}
							var d ds
							switch vr := vr.(type) {
							case BestOfN:
								d.VictoryRule.Rule = "BestOfN"
								d.VictoryRule.Length = int(vr)
							case StraightN:
								d.VictoryRule.Rule = "StraightN"
								d.VictoryRule.Length = int(vr)
							}
							p.Data.Parts = []dataPart{{Tag: "matchSettings", Data: d}}
						}
						if tu, ok := v.Data[TeamUpdateKey].(TeamUpdate); ok {
							t := make(map[string]interface{})
							t["blue"] = tu.Blue
							t["gold"] = tu.Gold
							p.Data.Parts = append(p.Data.Parts, dataPart{Tag: "currentTeams", Data: t})
						}
						if su, ok := v.Data[ScoreUpdateKey].(ScoreUpdate); ok {
							s := make(map[string]interface{})
							s["blue"] = su.Blue
							s["gold"] = su.Gold
							p.Data.Parts = append(p.Data.Parts, dataPart{Tag: "currentScores", Data: s})
						}
						if tl, ok := v.Data[TeamListKey].(teamList); ok {
							p.Data.Parts = append(p.Data.Parts, dataPart{Tag: "teamList", Data: tl})
						}
						if len(p.Data.Parts) > 0 {
							send(&p)
						}
					}

					if doCurrentMatch {
						p.Data.Section = "currentMatch"
						if vr, ok := v.Data[VictoryRuleKey].(MatchVictoryRule); ok {
							type ds struct {
								VictoryRule struct {
									Rule   string `json:"rule"`
									Length int    `json:"length"`
								} `json:"victoryRule"`
							}
							var d ds
							switch vr := vr.(type) {
							case BestOfN:
								d.VictoryRule.Rule = "BestOfN"
								d.VictoryRule.Length = int(vr)
							case StraightN:
								d.VictoryRule.Rule = "StraightN"
								d.VictoryRule.Length = int(vr)
							}
							p.Data.Parts = []dataPart{{Tag: "settings", Data: d}}
						}
						if tu, ok := v.Data[TeamUpdateKey].(TeamUpdate); ok {
							t := make(map[string]interface{})
							t["blue"] = tu.Blue
							t["gold"] = tu.Gold
							p.Data.Parts = append(p.Data.Parts, dataPart{Tag: "teams", Data: t})
						}
						if su, ok := v.Data[ScoreUpdateKey].(ScoreUpdate); ok {
							s := make(map[string]interface{})
							s["blue"] = su.Blue
							s["gold"] = su.Gold
							p.Data.Parts = append(p.Data.Parts, dataPart{Tag: "scores", Data: s})
						}
						if len(p.Data.Parts) > 0 {
							send(&p)
						}
					}

					if doTournamentData {
						if pd, ok := v.Data[PlayerDataKey].(map[string][]playerData); ok {
							p.Data.Section = "tournamentData"
							p.Data.Parts = append(p.Data.Parts, dataPart{Tag: "teams", Data: pd})
						}
						if len(p.Data.Parts) > 0 {
							send(&p)
						}
					}
				}
			}
		}()
	})
	panic(http.ListenAndServe(bindAddr, nil))
}
