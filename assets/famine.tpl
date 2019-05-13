{{define "JS" -}}
	function FamineTracker(root) {
		this.line1 = document.createTextNode('');
		this.line2 = document.createTextNode('');
		root.appendChild(this.line1);
		root.appendChild(document.createElement('br'));
		root.appendChild(this.line2);

		if (window.location.hash == '#layouttest') {
			this.line1.textContent = '10 berries left';
			this.line2.textContent = 'Respawn incoming';
		}

		var self = this;
		this.conn = new Connection('famineTracker', {
			update: function(data) {
				if (data.inFamine) {
					self.line1.textContent = 'FAMINE';
					if (data.famineLeftSeconds <= 10) {
						self.line2.textContent = 'Respawn incoming';
					} else {
						self.line2.textContent = '';
					}
				} else if (data.berriesLeft == 1) {
					self.line1.textContent = '';
					self.line2.textContent = '1 berry left';
				} else if (data.berriesLeft <= 12) {
					self.line1.textContent = '';
					self.line2.textContent = data.berriesLeft + ' berries left';
				} else {
					self.line1.textContent = '';
					self.line2.textContent = '';
				}
			}
		});
	}
{{- end}}
{{define "JS_init" -}}
new FamineTracker(document.getElementById('famineTracker'));
{{- end}}

{{define "CSS" -}}
#famineTracker { text-align: center; }
{{- end}}

{{define "Head" -}}
	<title>kq-live famine tracker</title>
	<script async>{{template "JS"}}
	window.addEventListener("load", function() {
		{{- template "JS_init" . -}}
	});</script>
	<style>{{template "CSS"}}</style>
{{- end}}

{{define "Body" -}}
<div id="famineTracker"></div>
{{- end}}
