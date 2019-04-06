{{define "EmptyImageUri"}}data:image/svg+xml,%3csvg xmlns='http://www.w3.org/2000/svg'/%3e{{end}}

{{define "TeamName" -}}
	function TeamName(root, properties) {
		this.elem = document.createElement('div');
		this.elem.id = properties.team + 'TeamName';
		this.elem.className =
			['teamName', properties.team, properties.side].join(' ');
		this.textNode = document.createTextNode(properties.name);
		this.elem.appendChild(this.textNode);
		root.appendChild(this.elem);
	}
	TeamName.prototype.updateName = function(name) {
		this.textNode.data = name;
	};
{{- end}}

{{define "ScoreMarkers" -}}
	{{template "ScoreMarker" .}}
	function Score(root, properties) {
		this.container = document.createElement('div');
		this.container.id = properties.team + 'ScoreMarkers';
		this.container.className =
			['scoreMarkers', properties.team, properties.side].join(' ');
		root.appendChild(this.container);

		this.team = properties.team;
		this.side = properties.side;
		this.markers = [];
		this.score = properties.score;
		this.markerCount = null;
		this.updateVictoryRule(properties.victoryRule);
	}
	Score.prototype.updateScore = function(score) {
		if (this.score == score) return;
		for (var i = this.score; i > score; --i) {
			this.markers[i - 1].setStatus('empty');
		}
		for (var i = this.score; i < score; ++i) {
			this.markers[i].setStatus('win');
		}

		this.score = score;
	};
	Score.prototype.updateVictoryRule = function(rule) {
		if (rule == null) rule = {};
		var markerCount;
		if (rule.rule === 'BestOfN') {
			markerCount = Math.ceil(rule.length / 2);
		} else if (rule.rule === 'StraightN') {
			markerCount = rule.length;
		}
		if (this.markerCount == markerCount) return;

		this.markerCount = markerCount;
		for (var i = this.markers.length; i > markerCount; --i) {
			this.markers.pop().remove();
		}
		for (var i = this.markers.length; i < markerCount; ++i) {
			this.markers.push(new ScoreMarker(this.container, {
				team: this.team,
				side: this.side,
				index: i,
				status: i < this.score ? 'win' : 'empty'
			}));
		}
	};
{{- end}}

{{define "TextScore" -}}
	function Score(root, properties) {
		this.elem = document.createElement('div');
		this.elem.id = properties.team + 'ScoreMarkers';
		this.elem.className =
			['scoreMarkers', properties.team, properties.side].join(' ');
		this.textNode = document.createTextNode(properties.score);
		this.elem.appendChild(this.textNode);
		root.appendChild(this.elem);
	}
	Score.prototype.updateScore = function(score) {
		this.textNode.data = score;
	};
	Score.prototype.updateVictoryRule = function(length) {};
{{- end}}

{{define "ism_PreloadedSide" -}}
	{{- $perTeamWin := ne (printf "%T" .win) "string" -}}
	{{- $perTeamEmpty := ne (printf "%T" .empty) "string" -}}
	{
		blue: {
			win: load({{if $perTeamWin -}}
				{{with .win.blue -}}
					{{.}}
				{{- else -}}
					"{{template "EmptyImageUri"}}"
				{{- end}}
			{{- else -}}
				{{.win}}
			{{- end}}),
			empty: load({{if $perTeamEmpty -}}
				{{with .empty.blue -}}
					{{.}}
				{{- else -}}
					"{{template "EmptyImageUri"}}"
				{{- end}}
			{{- else -}}
				{{.empty}}
			{{- end}})
		},
		gold: {
			win: load({{if $perTeamWin -}}
				{{with .win.gold -}}
					{{.}}
				{{- else -}}
					"{{template "EmptyImageUri"}}"
				{{- end}}
			{{- else -}}
				{{.win}}
			{{- end}}),
			empty: load({{if $perTeamEmpty -}}
				{{with .empty.gold -}}
					{{.}}
				{{- else -}}
					"{{template "EmptyImageUri"}}"
				{{- end}}
			{{- else -}}
				{{.empty}}
			{{- end}})
		}
	}
{{- end}}
{{define "ImageScoreMarker" -}}
	function ScoreMarker(parent, properties) {
		var preloaded =
			ScoreMarker.preloadedImages[properties.side][properties.team];
		this.images = {
			empty: preloaded.empty.src,
			win: preloaded.win.src
		};

		this.elem = document.createElement('img');
		this.elem.id = properties.team + 'Score' + properties.index;
		this.elem.src = this.images[properties.status];
		this.elem.className =
			['scoreMarker', properties.team, properties.side,
			 properties.status].join(' ');
		parent.appendChild(this.elem);
	}
	ScoreMarker.preloadedImages = function() {
		var cache = {};
		var load = function(url) {
			if (url in cache) return cache[url];
			var i = new Image();
			i.src = url;
			cache[url] = i;
			return i;
		};

		{{- if .left -}}
		return {
			left: {{template "ism_PreloadedSide" .left}},
			right: {{template "ism_PreloadedSide" .right}},
		};
		{{- else -}}
		var side = {{template "ism_PreloadedSide" .}};
		return { left: side, right: side };
		{{- end}}
	}();
	ScoreMarker.prototype.remove = function() {
		this.elem.remove();
		return this;
	};
	ScoreMarker.prototype.setStatus = function(status) {
		this.elem.src = this.images[status];
	}
{{- end}}

{{define "Score"}}{{template "TextScore" .}}{{end}}

{{define "JS" -}}
	{{template "Score" .}}
	{{template "TeamName" .}}
	function Scoreboard(root) {
		this.state = {
			teams: {
				blue: {teamName: '', score: 0},
				gold: {teamName: '', score: 0},
			},
			match: { victoryRule: null },
		};

		// Initialize layout and draw initial values.
		this.scores = {
			blue: new Score(root, {
				team: "blue",
				score: this.state.teams.blue.score,
				side: "{{if .GoldOnLeft}}right{{else}}left{{end}}",
				victoryRule: this.state.match.victoryRule
			}),
			gold: new Score(root, {
				team: "gold",
				score: this.state.teams.gold.score,
				side: "{{if .GoldOnLeft}}left{{else}}right{{end}}",
				victoryRule: this.state.match.victoryRule
			})
		};
		this.teamNames = {
			blue: new TeamName(root, {
				team: "blue",
				name: this.state.teams.blue.teamName,
				side: "{{if .GoldOnLeft}}right{{else}}left{{end}}"
			}),
			gold: new TeamName(root, {
				team: "gold",
				name: this.state.teams.gold.teamName,
				side: "{{if .GoldOnLeft}}left{{else}}right{{end}}"
			})
		};

		var self = this;
		this.conn = new Connection("currentMatch", {
			scores: function(data) {
				self.state.teams.blue.score = data.blue || 0;
				self.state.teams.gold.score = data.gold || 0;
				self.scores.blue.updateScore(self.state.teams.blue.score);
				self.scores.gold.updateScore(self.state.teams.gold.score);
			},
			settings: function(data) {
				self.state.match.victoryRule = data.victoryRule;
				self.scores.blue.updateVictoryRule(data.victoryRule);
				self.scores.gold.updateVictoryRule(data.victoryRule);
			},
			teams: function(data) {
				self.state.teams.blue.teamName = data.blue || '';
				self.state.teams.gold.teamName = data.gold || '';
				self.teamNames.blue.updateName(self.state.teams.blue.teamName);
				self.teamNames.gold.updateName(self.state.teams.gold.teamName);
			}
		});
	}
{{- end}}

{{define "JS_init" -}}
	new Scoreboard(document.getElementById("scoreboard"));
{{- end}}

{{define "Style"}}data:text/css,{{end}}

{{define "Head" -}}
	<title>kq-live scoreboard</title>
	<script async>{{template "JS"}}
	window.addEventListener("load", function() {
		{{- template "JS_init" . -}}
	});</script>
	<link rel="stylesheet" href="{{template "Style" .}}"/>
{{- end}}

{{define "Body" -}}
	<div id="scoreboard">
		{{- /* scores and names added dynamically */ -}}
	</div>
{{- end}}
