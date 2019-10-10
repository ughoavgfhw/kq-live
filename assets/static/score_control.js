function ScoreController(form, conn) {
	this.form = form;
	this.conn = conn;
	var inputs = form.getElementsByTagName('input');
	this.seriesLength = inputs.seriesLength;
	this.blueTeamOther = inputs.blueTeamOther;
	this.goldTeamOther = inputs.goldTeamOther;
	this.blueScore = inputs.blueScore;
	this.goldScore = inputs.goldScore;
	var selects = form.getElementsByTagName('select');
	this.goldTeamSelect = selects.goldTeam;
	this.blueTeamSelect = selects.blueTeam;

	var self = this;

	this.blueTeamSelect.addEventListener('change', function() {
		self.blueTeamOther.value = '';
	});
	this.goldTeamSelect.addEventListener('change', function() {
		self.goldTeamOther.value = '';
	});

	form.addEventListener('submit', function(e) {
		e.preventDefault();
		self.sendUpdate();
	});
	form.addEventListener('reset', function(e) { self.sendReset(); });

	var buttons = form.getElementsByClassName('setSeriesLengthButton');
	for (var b of buttons) {
		b.addEventListener('click', function(e) {
			self.seriesLength.value = e.target.getAttribute('seriesLength');
		});
	}
	buttons = form.getElementsByClassName('clearScoreButton');
	for (var b of buttons) {
		b.addEventListener('click', function(e) {
			self[e.target.getAttribute('side') + 'Score'].value = 0;
		});
	}
	buttons = form.getElementsByClassName('incrementScoreButton');
	for (var b of buttons) {
		b.addEventListener('click', function(e) {
			var score = self[e.target.getAttribute('side') + 'Score'];
			score.value = 1 + (parseInt(score.value, 10) || 0);
		});
	}
	buttons = form.getElementsByClassName('finishMatchButton');
	for (var b of buttons) {
		b.addEventListener('click', function() {
			self.finishMatch();
		});
	}

	this.updateTeamList(null);

	this.hasMatchSettings = form.getAttribute('matchsettings') || false;
	this.matchTeams = form.getAttribute('matchteams');
	this.matchScores = form.getAttribute('matchscores');
	if (this.hasMatchSettings) {
		conn.setHandler('matchSettings', function(data) {
			if (data.victoryRule === null) return;
			if (data.victoryRule.rule === 'BestOfN') {
				self.seriesLength.value = data.victoryRule.length;
			} else {
				console.warn('Ignoring unknown victory rule',
							 data.victoryRule);
			}
		});
	}
	if (this.matchTeams !== null) {
		conn.setHandler(this.matchTeams + 'Teams', function(data) {
			self.setTeam_(
				data.blue, self.blueTeamSelect, self.blueTeamOther);
			self.setTeam_(
				data.gold, self.goldTeamSelect, self.goldTeamOther);
		});
	}
	if (this.matchScores !== null) {
		conn.setHandler(this.matchScores + 'Scores', function(data) {
			self.blueScore.value = data.blue;
			self.goldScore.value = data.gold;
		});
	}
}

ScoreController.prototype.setTeam_ = function(team, select, other) {
	for (var opt of select.options) {
		if (opt.value === team) {
			select.value = team;
			other.value = '';
			return;
		}
	}
	select.value = '';
	other.value = team;
}

ScoreController.prototype.sendUpdate = function() {
	if (this.hasMatchSettings) {
		var data = { victoryRule: null };
		if (this.seriesLength.value !== '') {
			data.victoryRule = {
				rule: 'BestOfN',
				length: parseInt(this.seriesLength.value, 10) || 0
			};
		}
		this.conn.send('matchSettings', data);
	}
	if (this.matchTeams !== null) {
		var data = {
			blue: this.blueTeamSelect.value || this.blueTeamOther.value,
			gold: this.goldTeamSelect.value || this.goldTeamOther.value
		};
		this.conn.send(this.matchTeams + 'Teams', data);
	}
	if (this.matchScores !== null) {
		var data = {
			blue: parseInt(this.blueScore.value, 10) || 0,
			gold: parseInt(this.goldScore.value, 10) || 0
		};
		this.conn.send(this.matchScores + 'Scores', data);
	}
}
ScoreController.prototype.sendReset = function() {
	var parts = [];
	if (this.hasMatchSettings) parts.push('matchSettings');
	if (this.matchTeams !== null) parts.push(this.matchTeams + 'Teams');
	if (this.matchScores !== null) parts.push(this.matchScores + 'Scores');
	if (parts.length > 0) {
		this.conn.send('reset', parts);
	}
}
ScoreController.prototype.finishMatch = function() {
	if (this.matchTeams !== null) {
		var data = {
			gold: this.blueTeamSelect.value || this.blueTeamOther.value,
			blue: this.goldTeamSelect.value || this.goldTeamOther.value
		};
		this.conn.send(this.matchTeams + 'Teams', data);
	}
	if (this.matchScores !== null) {
		var data = {
			gold: parseInt(this.blueScore.value, 10) || 0,
			blue: parseInt(this.goldScore.value, 10) || 0
		};
		this.conn.send(this.matchScores + 'Scores', data);
	}
}

ScoreController.prototype.updateTeamList = function(teams) {
	if (teams == null) teams = [];
	this.updateTeamSelect_(teams, this.blueTeamSelect, this.blueTeamOther);
	this.updateTeamSelect_(teams, this.goldTeamSelect, this.goldTeamOther);
}
ScoreController.prototype.updateTeamSelect_ = function(teams, select,
													   other) {
	var curr = select.value || other.value;
	// Replace existing options with the new teams list when possible. The
	// first is "Other..." and should not be changed.
	var t = 0;
	var prev = select.firstElementChild;
	var found = false;
	for (var opt = prev.nextElementSibling;
		 t < teams.length && opt != null;
		 ++t, prev = opt, opt = opt.nextElementSibling) {
		opt.value = teams[t];
		opt.innerText = teams[t];
		if (teams[t] == curr) found = true;
	}
	// Remove any extra options.
	while (prev.nextElementSibling != null) {
		select.removeChild(select.lastElementChild);
	}
	// Add additional options as needed.
	for (; t < teams.length; ++t) {
		var opt = document.createElement('option');
		opt.value = teams[t];
		opt.innerText = teams[t];
		select.appendChild(opt);
		if (teams[t] == curr) found = true;
	}

	if (found) {
		select.value = curr;
		other.value = '';
	} else {
		select.value = '';  // Select "Other...".
		other.value = curr;
	}
	select.style.display = teams.length > 0 ? 'initial' : 'none';
}

window.addEventListener("load", function() {
	var currentMatchController;
	// This is capturing the controller variable before it is filled, which in
	// theory could allow the callback to run before it is filled. However, we
	// know the callback will only be run from network events, and since JS is
	// single-threaded no network events will be processed until after the
	// controllers are initialized and this returns.
	var conn = new Connection("control", {
		teamList: function(data) {
			currentMatchController.updateTeamList(data);
		}
	});
	currentMatchController =
		new ScoreController(document.getElementById('currentMatchForm'), conn);
});
