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
	timeout = window.setTimeout(hideMessage, 5000);
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

var dropZone, dropZoneText, picker, box, bar;

function uploadFile(fileList) {
	if (fileList == null || fileList.length == 0) {
		return;
	}

	disable();

	var file = fileList[0];
	var x    = new XMLHttpRequest();

	bar.style.width = '0%';
	box.classList.remove('active');
	dropZone.classList.add('active');

	x.upload.addEventListener('progress', function(e) {
		if (e.lengthComputable) {
			bar.style.width = (e.loaded * 100 / e.total) + '%';
		}
	}, false);

	x.upload.addEventListener('load', function() {
		bar.style.width = '100%';
	}, false);

	console.trace('attach cancel');
	dropZone.removeEventListener('click', clickPicker);

	var cancel = function() {
		x.abort();
		dropZone.removeEventListener(cancel);
		finish();
	}
	dropZone.addEventListener('click', cancel, false);

	x.addEventListener('load', function(e) {
		if (this.status !== 201) {
			var err = JSON.parse(this.responseText);
			showMessage($('#upload'), err.Err, 'bad');
		} else {
			var resp = JSON.parse(this.responseText);
			box.classList.add('active');
			box.value = window.location.protocol + '//' + resp.URL;
			box.select();
			box.focus();
			box.setSelectionRange(0, box.value.length);
		}
		dropZone.removeEventListener('click', cancel);
		finish();
	}, false);

	dropZoneText.dataset.oldText = dropZoneText.innerText;
	dropZoneText.innerText = 'Cancel';

	x.open('POST', '/upload/web', true);
	x.setRequestHeader('X-Airlift-Filename', encodeURIComponent(file.name));
	x.send(file);
}

function dropZoneEnter(e) {
	e.preventDefault();
	e.stopPropagation();
	dropZone.classList.add('active');
}

function dropZoneLeave(e) {
	e.preventDefault();
	e.stopPropagation();
	dropZone.classList.remove('active');
}

function dropped(e) {
	console.log(e);
	e.stopPropagation();
	e.preventDefault();
	uploadFile(e.dataTransfer.files);
}

function finish() {
	dropZone.classList.remove('active');
	dropZoneText.innerText = dropZoneText.dataset.oldText;
	bar.style.width = '0%';
	enable();
}

function enable() {
	console.trace('attach click');
	dropZone.addEventListener('click', clickPicker, false);
	dropZoneText.addEventListener('dragenter', dropZoneEnter, false);
	dropZoneText.addEventListener('dragover', dropZoneEnter, false);
	dropZoneText.addEventListener('dragleave', dropZoneLeave, false);
	dropZoneText.addEventListener('drop', dropped, false);
}

function disable() {
	dropZoneText.removeEventListener('dragenter');
	dropZoneText.removeEventListener('dragover');
	dropZoneText.removeEventListener('dragleave');
	dropZoneText.removeEventListener('drop');
}

function clickPicker() {
	picker.click();
}

function setupUploader() {
	dropZone = $('#drop-zone');
	dropZoneText = $('#drop-zone-text');
	picker = $('#picker');
	box = $('#uploaded-url');
	bar = dropZone.querySelector('.progress-bar');

	picker.addEventListener('change', function(e) {
		uploadFile(this.files);
	}, false);

	enable();
}
