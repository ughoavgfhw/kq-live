// A connection client, which handles certain types of data messages.
//
// The set of possible data messages is divided into sections. The server will
// only send messages for requested sections; a section is requested whenever
// a client is created for it. A section may have multiple associated data
// types, identified by a tag. The client specifies handlers by using the tag
// as a key in handler_map. The handlers receive any data object sent in the
// message.
//
// The data within a message is often an update, containing only partial state.
// When a client first connects, a full snapshot will be sent by the server,
// either as a single snapshot operation or as a series of updates from a
// default state.
//
// A single data message may have multiple parts for the same section for batch
// processing. If a client has expensive processing that could be batched, it
// can specify a special batch handler using an empty string key. This handler
// is called for any data message for the client's section, after all parts are
// passed to handlers of every client. It does not receive any data.
//
// Communication with the server occurs over a websocket, using a JSON-like
// payload. The payload is a valid JSON object, but it is required to have the
// first key be "type"; JSON does not specify any key order. This required
// ordering allows partial parsing, where the payload is only ever parsed for
// known message types and can be parsed directly into a known data structure.
function Connection(section, handler_map) {
	if (Connection.sock === null) {
		Connection.initSocket();
	}
	this.section = section;
	this.handlers = handler_map;
	Connection.addClient(this);
}
Connection.debug = false;
Connection.sock = null;
Connection.clients = [];
Connection.reconnectInfo = { last: null, count: 0 };
Connection.initSocket = function() {
	var server = 'ws://' + location.host + '/ws';
	console.log('connecting to ' + server + '...');
	var conn = new WebSocket(server);
	conn.onopen = Connection.handleOpen;
	conn.onerror = function(event) {
		console.log('websocket error; closing', event);
		if (Connection.sock == conn) Connection.sock = null;
		conn.close();
	};
	conn.onclose = function(event) {
		console.log('websocket closed', event.code, event.reason);
		if (Connection.sock == conn) Connection.sock = null;
		Connection.waitAndReconnect();
	};
	conn.onmessage = Connection.handleMessage;
	Connection.sock = conn;
};
Connection.addClient = function(client) {
	Connection.clients.push(client);
	if (Connection.sock !== null &&
		Connection.sock.readyState == WebSocket.OPEN) {
		Connection.send('client_start', {
			sections: [client.section]
		});
	}
};
Connection.handleOpen = function(event) {
	console.log('websocket connected to ' + event.target.url);
	Connection.reconnectInfo.last = new Date();
	var sections = {};
	for (var i = 0; i < Connection.clients.length; ++i) {
		sections[Connection.clients[i].section] = null;
	}
	Connection.send('client_start', {
		sections: Object.keys(sections)
	});
};
Connection.waitAndReconnect = function() {
	if (Connection.reconnectInfo.last != null &&
		new Date() - Connection.reconnectInfo.last > 5000) {
		Connection.reconnectInfo.last = null;
		Connection.reconnectInfo.count = 0;
	}
	var previousAttempts = Connection.reconnectInfo.count;
	Connection.reconnectInfo.count += 1;
	var delayMs = 100 * Math.exp(previousAttempts) * (Math.random() / 5 + 0.9);
	console.log('attempting reconnect in ' + (delayMs / 1000) + ' seconds');
	setTimeout(Connection.initSocket, delayMs);
};
Connection.send = function(type, data) {
	if (Connection.debug) console.log('websocket send', type, data);
	Connection.sock.send(
		'{"type":"' + type + '","data":' + JSON.stringify(data) + '}');
};
Connection.handleMessage = function(event) {
	if (Connection.debug) console.log('websocket receive', event.data);
	var msg;
	try {
		msg = JSON.parse(event.data);
	} catch(e) {
		console.log('websocket received invalid payload', e, event);
		return;
	}
	if (msg.type === 'data' && msg.data != null) {
		Connection.handleDataMessage(event, msg.data);
	} else if (msg.type === 'error') {
		console.log("websocket received error", msg.data);
	} else if ('string' !== typeof msg.type) {
		console.log("websocket received unexpected message format", event);
	}
};
Connection.handleDataMessage = function(event, data) {
	/* data: struct {
	 *   section: string
	 *   parts: []struct{
	 *     tag: string
	 *     data?: any
	 *   }
	 * }
	 */
	for (var i = 0; i < Connection.clients.length; ++i) {
		var client = Connection.clients[i];
		if (client.section !== data.section) continue;
		for (var p = 0; p < data.parts.length; ++p) {
			var part = data.parts[p];
			var handler = client.handlers[part.tag];
			if (handler) handler(part.data);
		}
	}
	for (var i = 0; i < Connection.clients.length; ++i) {
		var client = Connection.clients[i];
		if (client.section !== data.section) continue;
		if (client.handlers['']) client.handlers['']();
	}
};
