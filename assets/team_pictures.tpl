import './TeamPictures.css';

{{define "JS" -}}
function TeamPicsPlayerPhoto(root) {
	this.name = root.getElementsByClassName('playerPicName')[0];
	this.img = root.getElementsByTagName('img')[0];
	this.img.src = TeamPicsPlayerPhoto.defaultUri;
}
TeamPicsPlayerPhoto.prototype.update = function(player) {
	this.name.innerText = player.name || '';
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
.teamPics {
	width: 1920px;
	height: 540px;
	margin: 0;
}
.teamPicsName {
	font-size: 54px;
	line-height: 54px;
	height: 54px;
	font-family: "Vermin Vibes 1989"
}
.teamPics.left .teamPicsName {
	position: relative;
	top: 450px;
	text-align: right;
	padding-right: 975px;
}
.teamPics.right .teamPicsName {
	text-align: left;
	padding-left: 975px;
}
.playerPic {
	display: inline-block;
	width: 364px;
	height: 453px;
	margin: 3px 10px;
	vertical-align: top;
}
.teamPics.left .playerPic {
	position: relative;
	top: -54px;
}
.playerPic img {
	border: solid 4px;
	width: 356px;
	height: 356px;
	border-radius: 8px;
	object-fit: contain;
	background-color: rgba(0, 0, 0, 0.5);
}
.blue .playerPic img { border-color: rgb(50, 180, 255); }
.gold .playerPic img { border-color: rgb(255, 180, 0); }
.playerPicName {
	display: block;
	font-size: 30px;
	text-align: center;
}
#teamPicsVsText {
	position: absolute;
	top: 495px;
	left: 750px;
	width: 420px;
	text-align: center;
	font-size: 54px;
	line-height: 54px;
	font-family: "Vermin Vibes 1989"
}
{{- end}}

{{define "Head" -}}
	<title>kq-live team pictures</title>
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
<div id="teamPicsVsText">vs</div>
<div id="teamPics_{{$rightTeam}}" class="teamPics right {{$rightTeam}}">
	{{- template "TeamPics_OneTeam" -}}
</div>
{{- end}}
