function $(sel) { return document.querySelector(sel); }
function $$(sel) { return document.querySelectorAll(sel); }
var timeout;

function showMessage(root, msg, classname) {
	if (timeout != null) window.clearTimeout(timeout);
	var box = $('#message-box');
	if (box == null) {
		box = document.createElement('div');
		box.id = 'message-box';
	} else {
		box.parentNode.removeChild(box);
	}
	root.insertBefore(box, root.querySelector('h1').nextSibling);
	box.className = classname;
	box.innerText = msg;
	box.style.display = 'block';
}

function hideMessage() {
	$('#message-box').style.display = 'none';
}

function cb(e) {
	if (e.target.status == 204) {
		document.location.reload(true);
	} else {
		var resp = JSON.parse(e.target.responseText);
		if (resp != null && resp.Err != null) {
			showMessage($('#section-overview'), 'Error: ' + resp.Err, 'bad');
		} else {
			showMessage($('#section-overview'), 'An unknown error occurred (status ' + e.target.status + ')', 'bad');
		}
	}
}

function purgeAll() {
	if (!window.confirm('Really delete all of your uploads?')) {
		return;
	}

	var x = new XMLHttpRequest();
	x.addEventListener('load', cb, false);
	x.open('POST', '/purge/all');
	x.send();
}

function purgeThumbs() {
	var x = new XMLHttpRequest();
	x.addEventListener('load', cb, false);
	x.open('POST', '/purge/thumbs');
	x.send();
}
