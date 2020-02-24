{{define "JS" -}}
function TeamPicsPlayerPhoto(root) {
	this.name = root.getElementsByClassName('playerPicName')[0];
	this.img = root.getElementsByTagName('img')[0];
	this.img.src = TeamPicsPlayerPhoto.defaultUri;
}
TeamPicsPlayerPhoto.prototype.update = function(player) {
	this.name.innerText =
		[player.name || '', player.scene || '', player.pronouns || ''].join(' ');
	this.img.src = player.photoUri || TeamPicsPlayerPhoto.defaultUri;
}

function TeamPictures(root, color) {
	this.color = color;
	this.teamName = root.getElementsByClassName('teamPicsName')[0];
	var picContainers = root.getElementsByClassName('playerPic');
	this.photos = [
		new TeamPicsPlayerPhoto(picContainers[0]),
		new TeamPicsPlayerPhoto(picContainers[1]),
		new TeamPicsPlayerPhoto(picContainers[2]),
		new TeamPicsPlayerPhoto(picContainers[3]),
		new TeamPicsPlayerPhoto(picContainers[4])
	];

	var self = this;
	this.conn = new Connection('currentMatch', {
		teams: function(data) {
			self.selectTeam(data[color] || '');
		}
	});

	this.selectTeam('');
}
TeamPictures.teamPlayerData = {};
TeamPictures.prototype.getPlayerData = function(teamName) {
	// TODO: Sort the players based on which cab they are on if they have such settings.
	return TeamPictures.teamPlayerData[teamName] || [];
}
TeamPictures.prototype.selectTeam = function(teamName) {
	this.teamName.innerText = teamName;
	var playerData = this.getPlayerData(teamName);
	for (var i = 0; i < 5; ++i) {
		this.photos[i].update(playerData[i] || {});
	}
}
TeamPictures.prototype.update = function() {
	this.selectTeam(this.teamName.innerText);
}
{{- end}}
{{define "JS_init" -}}
TeamPicsPlayerPhoto.defaultUri = "{{with .DefaultPlayerPhoto}}{{.}}{{else}}data:image/svg+xml,%3csvg xmlns='http://www.w3.org/2000/svg'/%3e{{end}}";
var blue = new TeamPictures(document.getElementById('teamPics_blue'), 'blue');
var gold = new TeamPictures(document.getElementById('teamPics_gold'), 'gold');
new Connection('tournamentData', {
	teams: function(data) {
		TeamPictures.teamPlayerData = data || {};
		blue.update();
		gold.update();
	}
});
{{- end}}

{{define "CSS" -}}
.teamPicsName { font-size: 2em; }
.playerPic {
	display: inline-flex;
	flex-direction: column;
	padding: 1em;
	padding-top: 0;
}
.playerPic img {
	width: 144px;
	height: 144px;
}
{{- end}}

{{define "Head" -}}
	<title>kq-live status</title>
	<script async>{{template "JS"}}
	window.addEventListener("load", function() {
		{{- template "JS_init" . -}}
	});</script>
	<style>{{template "CSS"}}</style>
{{- end}}

{{define "TeamPics_OnePlayer" -}}
<div class="playerPic {{.}}"><img /><span class="playerPicName"></span></div>
{{- end}}
{{define "TeamPics_OneTeam" -}}
<div class="teamPicsName"></div>
{{- template "TeamPics_OnePlayer" "pos0"}}
{{- template "TeamPics_OnePlayer" "pos1"}}
{{- template "TeamPics_OnePlayer" "pos2"}}
{{- template "TeamPics_OnePlayer" "pos3"}}
{{- template "TeamPics_OnePlayer" "pos4"}}
{{- end}}
{{define "Body" -}}
{{$leftTeam := or (and .GoldOnLeft "gold") "blue" -}}
{{$rightTeam := or (and .GoldOnLeft "blue") "gold" -}}
<div id="teamPics_{{$leftTeam}}" class="teamPics left {{$leftTeam}}">
	{{- template "TeamPics_OneTeam" -}}
</div>
<div id="teamPics_{{$rightTeam}}" class="teamPics right {{$rightTeam}}">
	{{- template "TeamPics_OneTeam" -}}
</div>
{{- end}}
