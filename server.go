package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// Set at compile time in order to send last-modified headers with these flags:
//   -ldflags "-X main.compileTimeStr=$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
var compileTimeStr string
var compileTime, timeParseErr = time.Parse(time.RFC3339, compileTimeStr)

const homeLineChart = `<!doctype html>
<html>
<head>
	<script src="https://cdn.plot.ly/plotly-latest.min.js"></script>
	<script>
		window.addEventListener('load', function() {
			var multi = location.hash == '#multi';
			var timeline = document.getElementById('timeline');
			var multi_timeline = [timeline];
			if (multi) {
				for (var i = 0; i < 6; ++i) {
					var next = document.createElement('div');
					timeline.parentElement.insertBefore(next, timeline);
					multi_timeline.unshift(next);
					timeline = next;
				}
			}
			var layout = {yaxis: {range: [0, 1], nticks: 3}};
			for (var i = 0; i < multi_timeline.length; ++i) {
				Plotly.newPlot(multi_timeline[i], [{ x: [], y: [], text: [] }], layout);
			}
			var ws = new WebSocket('ws://localhost:8080/ws');
			ws.addEventListener('message', function(e) {
				var data = e.data.split(',');
				var command = data[0];
				var time = data[1];
				if (command == 'reset') {
					if (multi) {
						var next = multi_timeline.pop();
						multi_timeline.unshift(next);
						timeline.parentElement.removeChild(next);
						timeline.parentElement.insertBefore(next, timeline);
						timeline = next;
					}
					data[2] = parseInt(data[2], 10) || 1;
					var traces = [];
					for (var i = 0; i < data[2]; ++i) {
						traces.push({ x: [time], y: [0.5], text: [''] });
					}
					Plotly.react(timeline, traces, layout);
				} else {
					for (var i = 3; i < data.length; ++i) {
						Plotly.extendTraces(timeline,
											{x: [[time]], y: [[data[i]]], text: [[data[2]]]}, [i-3]);
					}
				}
			});
		});
	</script>
</head>
<body>
	<div id="timeline"></div>
</body>
</html>
`

const homeMeter = `<!doctype html>
<html>
<head>
	<script>
		function setPath(score, pointer) {
			var radius = 40;
			var radians = (1 - score) * Math.PI;
			var x = radius * Math.cos(radians);
			var y = radius * Math.sin(radians);

			var tan = Math.tan(radians);
			var y1 = 2 / Math.sqrt(1 + tan*tan), x1 = y1 * -tan;
			if (score == 0 || score == 1) { x1 = 0; y1 = 2; }
			if (score == 0.5) { x1 = 2; y1 = 0; }
			var path = ['M', 50 + x1, 50 - y1,
						'L', 50 - x1, 50 + y1,
						'L', 50 + x, 50 - y, 'Z'].join(' ');
			pointer.setAttributeNS(null, 'd', path);
		}

		window.addEventListener('load', function() {
			var which = 0;
			try { which = parseInt(location.hash.substr(1), 10); } catch {}
			if (isNaN(which)) which = 0;
			var pointer = document.getElementById('pointer');
			setPath(0.5, pointer);
			var ws = new WebSocket('ws://localhost:8080/ws');
			ws.addEventListener('message', function(e) {
				var data = e.data.split(',');
				var command = data[0];
				if (command == 'reset') {
					setPath(0.5, pointer);
				} else {
					setPath(data[3 + which], pointer);
				}
			});
		});
	</script>
</head>
<body>
	<svg xmlns="http://www.w3.org/2000/svg" width="100" height="51">
		<path d="M 50 0 A 50 50 0 0 0 0 50 L 50 50 Z"
			  fill="rgb(50, 180, 255)" />
		<path d="M 100 50 A 50 50 0 0 0 50 0 L 50 50 Z"
			  fill="rgb(255, 180, 0)" />
		<path id="pointer" fill="rgb(133, 0, 0)" />
	</svg>
</body>
</html>
`

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
	if len(compileTimeStr) > 0 && timeParseErr != nil {
		panic(timeParseErr)
	}
	reg := make(chan *chan<- interface{}, 1)
	unreg := make(chan *chan<- interface{})
	go runRegistry(dataSource, reg, unreg)
	http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		var content io.ReadSeeker
		switch req.FormValue("type") {
		case "", "line":
			content = strings.NewReader(homeLineChart)
		case "meter":
			content = strings.NewReader(homeMeter)
		default:
			w.WriteHeader(400)
			// Doesn't handle ranges or set all of the headers of ServeContent,
			// but ServeContent can't be used after setting a status code.
			io.Copy(w, strings.NewReader(homeLineChart))
			return
		}
		http.ServeContent(w, req, "index.html", compileTime, content)
	})
	var upgrader websocket.Upgrader
	http.HandleFunc("/ws", func(w http.ResponseWriter, req *http.Request) {
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
