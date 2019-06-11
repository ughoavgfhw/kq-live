{{define "JS" -}}
	function StatsboardCell(root) {
		this.splitWarrior = window.location.search == '?kills=split';

		this.root = root;
		if (this.splitWarrior) {
			var l = document.createElement('div');
			l.className = 'killLabel';
			root.appendChild(l);
		}
		this.milKills = document.createElement('div');
		this.milKills.className = 'military kills';
		root.appendChild(this.milKills);
		this.kills = document.createElement('div');
		this.kills.className = 'kills';
		root.appendChild(this.kills);
		this.deaths = document.createElement('div');
		this.deaths.className = 'deaths';
		root.appendChild(this.deaths);
		this.status = document.createElement('div');
		this.status.className = 'statusIcon';
		root.appendChild(this.status);
	}
	StatsboardCell.prototype.update = function(stats) {
		if (this.splitWarrior) {
			var k = stats.Kills - stats.DroneKills;
			if (Number.isNaN(k)) console.error(stats);
			this.milKills.innerText = '' + k;
			this.kills.innerText = '' + stats.DroneKills;
		} else {
			this.milKills.innerText = '' + stats.Kills;
			this.kills.innerText = '' + stats.Assists;
		}
		this.deaths.innerText = '' + stats.Deaths;
		this.root.setAttribute('statsboardqueenkills', stats.QueenKills);
	}
	StatsboardCell.prototype.setStatus = function(status) {
		var st = (status.Speed && status.Warrior) ? 'sw' :
					status.Speed ? 's' :
					status.Warrior ? 'w' : '';
		this.status.setAttribute('statsboardstatus', st);
	}
	function Statsboard(root, side) {
		this.side = side;
		this.cells = [];
		var positions = ['checks', 'skulls', 'queen', 'abs', 'stripes'];
		for (var i = 0; i < 5; ++i) {
			var cell = document.createElement('div');
			cell.className = 'statsboardCell ' + positions[i];
			root.appendChild(cell);
			this.cells.push(new StatsboardCell(cell));
		}

		var self = this;
		this.conn = new Connection("prediction", {
			reset: function(data) { self.reset(); },
			stats: function(data) { self.update(data); }
		});

		this.reset();
	}
	Statsboard.prototype.reset = function() {
		var stats = {Kills: 0, DroneKills: 0, Assists: 0, Deaths: 0, QueenKills: 0};
		for (var i = 0; i < 5; ++i) {
			this.cells[i].update(stats);
			this.cells[i].setStatus({});
		}
	};
	Statsboard.prototype.update = function(data) {
		var positions;
		switch (this.side) {
			case 'blue': positions = [9, 7, 1, 5, 3]; break;
			case 'gold': positions = [8, 6, 0, 4, 2]; break;
		}
		for (var i = 0; i < 5; ++i) {
			this.cells[i].update(data.stats[positions[i]]);
			this.cells[i].setStatus(data.status[positions[i]]);
		}
	};
{{- end}}
{{define "JS_init" -}}
new Statsboard(document.getElementById('statsboard{{.Side}}'), {{.Side}});
{{- end}}

{{define "CSS" -}}
	{{/* Backgrounds are done via sprite sheet, one sheet per color. Right now they are loaded via assetUri, which may cause both to be sent over the wire when only one is needed. */ -}}
	#statsboardblue .statsboardCell {
		background-image: url("{{assetUri "/blue_bar.png"}}");
	}
	#statsboardgold .statsboardCell {
		background-image: url("{{assetUri "/gold_bar.png"}}");
	}
	.statsboardCell {
		background-size: auto 64px;
		display: inline-block;
		position: relative;
		width: 83px;
		height: 64px;
		margin: 10px 40px 0 40px;
		font-size: 30px;
		color: white;
		{{/* Triple shadow to make it darker. */ -}}
		text-shadow: 0 0 0.2em black, 0 0 0.2em black, 0 0 0.2em black;
	}
	.statsboardCell.checks { background-position: 0; }
	.statsboardCell.skulls { background-position: -100px; }
	.statsboardCell.queen { background-position: -200px; }
	.statsboardCell.abs { background-position: -300px; }
	.statsboardCell.stripes { background-position: -400px; }
	.statsboardCell .kills, .statsboardCell .deaths {
		display: inline-block;
		position: absolute;
		top: 1em;
	}
	.statsboardCell .kills { left: -1ch; }
	.statsboardCell .military.kills { top: 0; }
	.statsboardCell .deaths { right: -1ch; }
	.statsboardCell .killLabel, .statsboardCell .statusIcon {
		position: absolute;
		display: inline-block;
		background-image: url("{{assetUri "/statsboard_sprites.png"}}");
		background-size: auto 96px;
		width: 40px;
	}
	.statsboardCell .killLabel {
		height: 64px;
		background-position-x: -80px;
		left: -40px;
	}
	.statsboardCell .statusIcon {
		height: 32px;
		background-position: -40px -32px;
		right: 0;
	}
	.statsboardCell .statusIcon[statsboardstatus="s"] { background-position-y: -64px; }
	.statsboardCell .statusIcon[statsboardstatus="w"] { background-position: 0 0; }
	.statsboardCell .statusIcon[statsboardstatus="sw"] { background-position-y: 0; }
	[statsboardqueenkills]::before {
		display: block;
		position: absolute;
		bottom: 53px;
		left: 11px;
		right: 0;
		text-align: center;
	}
	{{/* Note: If assetUri chose to embed the crown, it would be effectively loaded 6 times. To avoid this, they are directly linked via /static/. */ -}}
	[statsboardqueenkills="1"]::before {
		content: url("/static/kill_crown.png");
	}
	[statsboardqueenkills="2"]::before {
		content: url("/static/kill_crown.png") " " url("/static/kill_crown.png");
	}
	[statsboardqueenkills="3"]::before {
		content: url("/static/kill_crown.png") " " url("/static/kill_crown.png") " " url("/static/kill_crown.png");
		left: 0;
	}
	.queen[statsboardqueenkills]::before { left: 0; }
	.queen[statsboardqueenkills="2"]::before { right: 25px; }
{{- end}}

{{define "Head" -}}
	<title>kq-live statsboard</title>
	<script async>{{template "JS"}}
	window.addEventListener("load", function() {
		{{- template "JS_init" . -}}
	});</script>
	<style>{{template "CSS"}}</style>
{{- end}}

{{define "Body" -}}
<div id="statsboard{{.Side}}"></div>
{{- end}}
