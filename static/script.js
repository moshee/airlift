function $(sel) { return document.querySelector(sel); }
function $$(sel) { return document.querySelectorAll(sel); }
var timeout;

function makesvg(elem) { return document.createElementNS("http://www.w3.org/2000/svg", elem); }

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

var dropZone, dropZoneText, picker, urlList, bar;

function uploadSingle(file) {
	disable();

	var x = new XMLHttpRequest();

	bar.style.width = '0%';
	urlList.classList.remove('active');
	dropZone.classList.add('active');

	x.upload.addEventListener('progress', function(e) {
		if (e.lengthComputable) {
			bar.style.width = (e.loaded * 100 / e.total) + '%';
		}
	}, false);

	x.upload.addEventListener('load', function() {
		bar.style.width = '100%';
	}, false);

	dropZone.removeEventListener('click', clickPicker);

	var cancel = function() {
		x.abort();
		dropZone.removeEventListener(cancel);
		finish();
	}
	dropZone.addEventListener('click', cancel, false);

	x.addEventListener('load', function(e) {
		var resp = JSON.parse(this.responseText);
		if (this.status !== 201) {
			showMessage($('#upload'), resp.Err, 'bad');
		} else {
			setURLList([window.location.protocol + '//' + resp.URL]);
			/*
			box.classList.add('active');
			box.value = 
			box.select();
			box.focus();
			box.setSelectionRange(0, box.value.length);
		   */
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

function uploadMultiple(fileList) {
	var totalSize = 0;
	for (var i = 0; i < fileList.length; i++) {
		totalSize += fileList[i].size;
	}

	var svg = dropZone.querySelector('svg');
	if (svg == null) {
		svg = makesvg('svg');
		dropZone.appendChild(svg);
	}
	while (svg.hasChildNodes()) svg.removeChild(svg.firstChild)

	for (var i = acc = 0, pos; i < fileList.length; i++) {
		acc += fileList[i].size;
		pos = acc/totalSize * dropZone.offsetWidth;
		var line = makesvg('line');
		line.setAttribute('x1', pos);
		line.setAttribute('x2', pos);
		line.setAttribute('y1', 0);
		line.setAttribute('y2', dropZone.offsetHeight - 8);
		svg.appendChild(line);
	}

	bar.style.width = '0%';
	urlList.classList.remove('active');
	dropZone.classList.add('active');

	var err = null;
	var x = null;

	var cancel = function() {
		if (x != null) {
			x.abort();
			dropZone.removeEventListener(cancel);
			finish();
		}
		if (svg != null) {
			while (svg.hasChildNodes()) svg.removeChild(svg.firstChild);
		}
	};
	dropZone.removeEventListener('click', clickPicker);
	dropZone.addEventListener('click', cancel, false);

	dropZoneText.dataset.oldText = dropZoneText.innerText;
	dropZoneText.innerText = 'Cancel';

	var next = function(i, result, totalLoaded) {
		if (i < fileList.length) {
			var file = fileList[i];
			x = new XMLHttpRequest();

			x.upload.addEventListener('progress', function(e) {
				if (e.lengthComputable) {
					bar.style.width = ((totalLoaded + e.loaded)*100 / totalSize) + '%';
				}
			}, false);

			x.upload.addEventListener('load', function() {
				totalLoaded += file.size;
				bar.style.width = totalLoaded*100 / totalSize + '%';
			}, false);

			x.addEventListener('load', function(e) {
				if (this.status !== 201) {
					var err = JSON.parse(this.responseText);
					showMessage($('#upload'), err.Err, 'bad');
				} else {
					var resp = JSON.parse(this.responseText);
					result.push(window.location.protocol + '//' + resp.URL);
					setTimeout(next, 1, i+1, result, totalLoaded);
				}
			}, false);

			x.open('POST', '/upload/web', true);
			x.setRequestHeader('X-Airlift-Filename', encodeURIComponent(file.name));
			x.send(file);
		} else {
			finish();
			setURLList(result);
			/*
			box.classList.add('active');
			box.value = result.join(' ');
			box.select();
			box.focus();
			box.setSelectionRange(0, box.value.length);
		   */
			dropZone.removeEventListener('click', cancel);
			dropZone.addEventListener('click', clickPicker);
			while (svg.hasChildNodes()) svg.removeChild(svg.firstChild);
		}
	};

	next(0, [], 0);
}

function setURLList(urls) {
	var ul = urlList.querySelector('ul');
	while (ul.hasChildNodes()) ul.removeChild(ul.firstChild);
	for (var i = 0, url, li, a; url = urls[i]; i++) {
		li = document.createElement('li');
		a = document.createElement('a');
		a.href = url;
		a.innerText = url;
		li.appendChild(a);
		ul.appendChild(li);
	}
	urlList.classList.add('active');
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
	e.stopPropagation();
	e.preventDefault();
	uploadFiles(e.dataTransfer.files);
}

function uploadFiles(fileList) {
	if (fileList == null) {
		return;
	}

	switch (fileList.length) {
	case 0:
		break;
	case 1:
		uploadSingle(fileList[0]);
		break;
	default:
		uploadMultiple(fileList);
		break;
	}
}

function finish() {
	dropZone.classList.remove('active');
	dropZoneText.innerText = dropZoneText.dataset.oldText;
	bar.style.width = '0%';
	enable();
}

function enable() {
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
	urlList = $('#uploaded-urls');
	bar = dropZone.querySelector('.progress-bar');

	picker.addEventListener('change', function(e) {
		uploadFiles(this.files);
	}, false);

	enable();
}
