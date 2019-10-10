{{define "JS" -}}
function textNodesByPlayerId(cells) {
	return [7, 2, 9, 4, 8, 3, 6, 1, 5, 0].map(function(pos) {
		return cells[pos].firstChild;
	});
}

function formatTime(secs) {
	secs /= 1e9;
	var mins = Math.floor(secs / 60);
	secs = Math.round(secs - mins * 60);
	return '' + mins + ':' + (secs < 10 ? '0' : '') + secs;
}
function format(val, key) {
	if (!val) return '';
	if (key.endsWith('Time')) {
		return formatTime(val);
	} else {
		return '' + val;
	}
}

function PostGameStats(root) {
	this.root = root;
	this.gameDurCell = root.getElementsByClassName('gameDur')[0].firstChild;

	var rows = root.getElementsByTagName('tr');
	this.dataCells = {
		WarriorTime: textNodesByPlayerId(rows[2].getElementsByTagName('td')),
		SnailTime: textNodesByPlayerId(rows[3].getElementsByTagName('td')),
		BerriesRun: textNodesByPlayerId(rows[4].getElementsByTagName('td')),
		QueenKills: textNodesByPlayerId(rows[5].getElementsByTagName('td')),
		WarriorKills: textNodesByPlayerId(rows[6].getElementsByTagName('td')),
		DroneKills: textNodesByPlayerId(rows[7].getElementsByTagName('td')),
		MilitaryDeaths: textNodesByPlayerId(rows[8].getElementsByTagName('td')),
		DroneDeaths: textNodesByPlayerId(rows[9].getElementsByTagName('td')),
		MilitaryAssists: textNodesByPlayerId(rows[10].getElementsByTagName('td')),
		DroneAssists: textNodesByPlayerId(rows[11].getElementsByTagName('td'))
	};
	this.keys = Object.keys(this.dataCells);

	this.reset();
	var self = this;
	this.conn = new Connection('prediction', {
		reset: function(data) { self.reset(); },
		stats: function(data) { self.update(data); }
	});
}

PostGameStats.prototype.reset = function() {
	this.gameDurCell.textContent = '';
	for (var k of this.keys) {
		for (var c of this.dataCells[k]) {
			c.textContent = '';
		}
	}
}

PostGameStats.prototype.update = function(data) {
	this.gameDurCell.textContent =
		'Game Duration: ' + formatTime(data.duration || 0);
	for (var i = 0; i < 10; ++i) {
		if (data.stats[i].MilitaryDeaths === undefined) {
			data.stats[i].MilitaryDeaths = (data.stats[i].Deaths || 0) -
				(data.stats[i].DroneDeaths || 0);
		}
		if (data.stats[i].MilitaryAssists === undefined) {
			data.stats[i].MilitaryAssists = (data.stats[i].Assists || 0) -
				(data.stats[i].DroneAssists || 0);
		}
		for (var k of this.keys) {
			this.dataCells[k][i].textContent = format(data.stats[i][k], k);
		}
	}
}
{{- end}}

{{define "JS_init" -}}
new PostGameStats(document.getElementById('postGameStats'));
{{- end}}

{{define "CSS" -}}
	#postGameStats {
		width: 1920px;
		height: 1080px;
		font-size: 36px;
		text-align: center;
	}
	#postGameStats col.team { width: 144px; }
	#postGameStats col.mid { width: 478px; }
	#postGameStats tr th.mid, #postGameStats tr th:nth-child(6) {
		{{- if .GoldOnLeft}}
		border-left: solid 1px #FFB400;
		border-right: solid 1px #32B4FF;
		{{- else}}
		border-left: solid 1px #32B4FF;
		border-right: solid 1px #FFB400;
		{{- end}}
	}
	#postGameStats .camBox { height: 408px; }
	#postGameStats .teamBar th {
		height: 96px;
		background-size: auto 96px;
		background-repeat: no-repeat;
	}
	{{- if .GoldOnLeft}}
	#postGameStats .teamBar th.left { background-image: url("{{assetUri "/gold_bar.png"}}"); }
	#postGameStats .teamBar th.right { background-image: url("{{assetUri "/blue_bar.png"}}"); }
	{{- else}}
	#postGameStats .teamBar th.left { background-image: url("{{assetUri "/blue_bar.png"}}"); }
	#postGameStats .teamBar th.right { background-image: url("{{assetUri "/gold_bar.png"}}"); }
	{{- end}}
	#postGameStats .teamBar th.checks { background-position: 10px; }
	#postGameStats .teamBar th.skulls { background-position: -140px; }
	#postGameStats .teamBar th.queen { background-position: -290px; }
	#postGameStats .teamBar th.abs { background-position: -440px; }
	#postGameStats .teamBar th.stripes { background-position: -590px; }
{{- end}}

{{define "Head" -}}
	<title>kq-live post-game stats</title>
	<script async>{{template "JS"}}
	window.addEventListener("load", function() {
		{{- template "JS_init" . -}}
	});</script>
	<style>{{template "CSS" .}}</style>
{{- end}}

{{define "postgameRow" -}}
	<tr>
		<td> </td><td> </td><td> </td><td> </td><td> </td>
		<th>{{.}}</th>
		<td> </td><td> </td><td> </td><td> </td><td> </td>
	</tr>
{{- end}}

{{define "Body" -}}
<table id="postGameStats" cellspacing="0" cellpadding="0">
	<col class="team" span="5"><col class="mid"><col class="team" span="5">
	<tr class="camBox">
		<th colspan="5"></th>
		<th class="mid"></th>
		<th colspan="5"></th>
	</tr>
	<tr class="teamBar">
		<th class="left checks"></th>
		<th class="left skulls"></th>
		<th class="left queen"></th>
		<th class="left abs"></th>
		<th class="left stripes"></th>
		<th class="gameDur">Game Duration: 00:00</th>
		<th class="right checks"></th>
		<th class="right skulls"></th>
		<th class="right queen"></th>
		<th class="right abs"></th>
		<th class="right stripes"></th>
	</tr>
	{{template "postgameRow" "Time as Warrior"}}
	{{template "postgameRow" "Time on Snail"}}
	{{template "postgameRow" "Berries Run"}}
	{{template "postgameRow" "Queen Kills"}}
	{{template "postgameRow" "Warrior Kills"}}
	{{template "postgameRow" "Drone Kills"}}
	{{template "postgameRow" "Military Deaths"}}
	{{template "postgameRow" "Drone Deaths"}}
	{{template "postgameRow" "Military Assists"}}
	{{template "postgameRow" "Drone Assists"}}
</table>
{{- end}}
