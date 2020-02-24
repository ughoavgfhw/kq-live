{{define "JS" -}}
	function FamineTracker(root) {
		this.text = root.firstElementChild.nextElementSibling;
		if (window.location.hash == '#layouttest') {
			this.text.innerText = '0:00';
		}

		var self = this;
		this.conn = new Connection('famineTracker', {
			update: function(data) {
				if (data.inFamine) {
					var m = Math.floor(data.famineLeftSeconds / 60);
					var s = Math.floor(data.famineLeftSeconds % 60);
					if (s < 10) s = '0' + s;
					self.text.innerText = m + ':' + s;
				} else {
					self.text.innerText = data.berriesLeft;
				}
			}
		});
	}
{{- end}}
{{define "JS_init" -}}
new FamineTracker(document.getElementById('famineTracker'));
{{- end}}

{{define "CSS" -}}
#famineTracker span { vertical-align: top; font-size: 60px; }
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
<div id="famineTracker">
	 <img src="{{assetUri "/single_berry.png"}}" />
	 <span></span>
</div>
{{- end}}
