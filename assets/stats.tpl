{{define "JS" -}}
  function extractPlayerCells(row, headers, players, stat) {
    var c = row.firstElementChild;
    for (var i = 0; i < headers; ++i) {
      c = c.nextElementSibling;
    }
    var map = [9, 7, 1, 5, 3, 8, 6, 0, 4, 2];
    for (var i = 0; i < 10; ++i) {
      players[map[i]][stat] = c;
      c = c.nextElementSibling;
    }
    return row.nextElementSibling;
  }
  function formatTime(secs) {
    secs /= 1e9;
    if (secs >= 60) {
      var mins = Math.floor(secs / 60);
      secs -= mins * 60;
      return '' + mins + 'm' + secs.toFixed(3) + 's';
    }
    return '' + secs.toFixed(3) + 's';
  }
  function format(val, key) {
    if (!val) return '';
    if (key.endsWith('Time')) {
      return formatTime(val);
    } else {
      return '' + val;
    }
  }
  function Stats(root) {
    var row = root.firstElementChild;
	this.mapCell = row.firstElementChild.nextElementSibling;
	row = row.nextElementSibling;
	this.timeCell = row.firstElementChild.nextElementSibling;
	row = row.nextElementSibling;
	this.stateCell = row.firstElementChild.nextElementSibling;
	row = row.nextElementSibling;

    this.statCells = [{}, {}, {}, {}, {}, {}, {}, {}, {}, {}];
    row = extractPlayerCells(row, 2, this.statCells, 'BerriesRun');
    row = extractPlayerCells(row, 1, this.statCells, 'BerriesKicked');
    row = extractPlayerCells(row, 1, this.statCells, 'BerriesKickedOpp');
    row = extractPlayerCells(row, 2, this.statCells, 'SnailTime');
    row = extractPlayerCells(row, 1, this.statCells, 'SnailDist');
    row = extractPlayerCells(row, 1, this.statCells, 'EatKills');
    row = extractPlayerCells(row, 1, this.statCells, 'EatDeaths');
    row = extractPlayerCells(row, 1, this.statCells, 'EatRescues');
    row = extractPlayerCells(row, 1, this.statCells, 'EatRescued');
    row = extractPlayerCells(row, 2, this.statCells, 'WarriorTime');
    row = extractPlayerCells(row, 1, this.statCells, 'MaxWarriorTime');
    row = extractPlayerCells(row, 1, this.statCells, 'LastWarriorTime');
    row = extractPlayerCells(row, 2, this.statCells, 'Kills');
    row = extractPlayerCells(row, 1, this.statCells, 'QueenKills');
    row = extractPlayerCells(row, 1, this.statCells, 'WarriorKills');
    row = extractPlayerCells(row, 1, this.statCells, 'DroneKills');
    row = extractPlayerCells(row, 1, this.statCells, 'SnailKills');
    row = extractPlayerCells(row, 2, this.statCells, 'Deaths');
    row = extractPlayerCells(row, 1, this.statCells, 'WarriorDeaths');
    row = extractPlayerCells(row, 1, this.statCells, 'DroneDeaths');
    row = extractPlayerCells(row, 1, this.statCells, 'SnailDeaths');
    row = extractPlayerCells(row, 2, this.statCells, 'Assists');
    row = extractPlayerCells(row, 1, this.statCells, 'DroneAssists');

    var self = this;
    this.conn = new Connection("prediction", {
      reset: function(data) { self.reset(); },
      stats: function(data) { self.update(data); }
    });

    this.reset();
  }
  Stats.prototype.reset = function() {
    this.mapCell.innerText = '';
    this.timeCell.innerText = '0s';
    this.stateCell.innerText = 'starting...';
    for (var i = 0; i < 10; ++i) {
      for (var k of Object.keys(this.statCells[i])) {
        this.statCells[i][k].innerText = format(0, k);
      }
    }
  };
  Stats.prototype.update = function(data) {
    this.mapCell.innerText = data.map || '';
    this.timeCell.innerText = formatTime(data.duration || 0);
    if ('winner' in data) {
      this.stateCell.innerText = data.winner + ' wins by ' + data.winType;
    } else {
      this.stateCell.innerText = 'playing...';
    }
    for (var i = 0; i < data.stats.length; ++i) {
      for (var k of Object.keys(data.stats[i])) {
        var cell = this.statCells[i][k];
        if (cell) cell.innerText = format(data.stats[i][k], k);
      }
    }
  };

  function Log(root) {
    this.log = root;
    var self = this;
    this.conn = new Connection("prediction", {
      valentineEvent: function(data) {
        for (var event of data.events) {
          self.addLog(data.time, event.awardee, event.event);
        }
      }
    });
  }
  Log.prototype.addLog = function(time, awardee, event) {
    var li = document.createElement('li');
    li.innerText = [time, awardee, event].join(': ');
    this.log.insertBefore(li, this.log.firstChild);
  }

  window.addEventListener("load", function() {
    new Stats(document.getElementById('stats'));
    window.log = new Log(document.getElementById('log'));
  });
{{- end}}

{{define "Head" -}}
  <title>kq-live stats</title>
  <script async>{{template "JS" .}}</script>
  <style>
    table { border-collapse: collapse; }
    tr, td { border: solid 1px black; }
    thead td { border: 0; }
    thead th { min-width: 75pt; }
    tr.a { background-color: #ddeeff; }
    tr.b { background-color: #ffeebb; }
  </style>
{{- end}}

{{define "Body" -}}
<table>
  <thead>
    <tr><th colspan="2"></th><th>Blue Checks</th><th>Blue Skulls</th><th>Blue Queen</th><th>Blue Abs</th><th>Blue Stripes</th><td><div style="width:20pt"></div></td><th>Gold Checks</th><th>Gold Skulls</th><th>Gold Queen</th><th>Gold Abs</th><th>Gold Stripes</th></tr>
  </thead>
  <tbody id="stats">
    <tr><th>Map</th><td colspan="6"></td><td rowspan="26"></td><td colspan="5"></td></tr>
    <tr><th>Time</th><td colspan="6"></td><td colspan="5"></td></tr>
    <tr><th>State</th><td colspan="6"></td><td colspan="5"></td></tr>
    <tr class="a"><th rowspan="3">Berries</th><th>Run</th><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td></tr>
	<tr class="a"><th>Kicked (Own team)</th><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td></tr>
	<tr class="a"><th>Kicked (Opponent)</th><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td></tr>
    <tr class="b"><th rowspan="6">Snail</th><th>Time</th><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td></tr>
    <tr class="b"><th>Distance</th><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td></tr>
    <tr class="b"><th>Snacks Eaten</th><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td></tr>
    <tr class="b"><th>Times Eaten</th><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td></tr>
    <tr class="b"><th>Snacks Rescued</th><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td></tr>
    <tr class="b"><th>Times Rescued</th><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td></tr>
    <tr class="a"><th rowspan="3">Warrior</th><th>Time</th><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td></tr>
    <tr class="a"><th>Max Time</th><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td></tr>
    <tr class="a"><th>Previous Time</th><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td></tr>
    <tr class="b"><th rowspan="5">Kills</th><th>&nbsp;</th><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td></tr>
    <tr class="b"><th>Queen</th><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td></tr>
    <tr class="b"><th>Warrior</th><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td></tr>
    <tr class="b"><th>Drone</th><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td></tr>
    <tr class="b"><th>Snail Rider</th><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td></tr>
    <tr class="a"><th rowspan="4">Deaths</th><th>&nbsp;</th><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td></tr>
    <tr class="a"><th>Warrior</th><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td></tr>
    <tr class="a"><th>Drone</th><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td></tr>
    <tr class="a"><th>Snail Rider</th><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td></tr>
    <tr class="b"><th rowspan="2">Assists</th><th>&nbsp;</th><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td></tr>
    <tr class="b"><th>Drone</th><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td></tr>
  </tbody>
</table>
<ul id="log" style="max-height: 9em; overflow: scroll"></ul>
{{- end}}
