function $(sel) { return document.querySelector(sel); }
function $$(sel) { return document.querySelectorAll(sel); }
var timeout;

function setupUploader() {
	dropZone = $('#drop-zone');
	dropZoneText = $('#drop-zone-text');
	picker = $('#picker');
	urlList = $('#uploaded-urls');
	bar = dropZone.querySelector('.progress-bar');

	picker.addEventListener('change', function(e) {
		uploadFiles(this.files);
	}, false);

	window.addEventListener('paste', paste, false);

	enable();
}

function paste(e) {
	var items = [];
	var i = 0;
	var item;

	var next = function() {
		item = e.clipboardData.items[i++];
		if (item == null) {
			uploadFiles(items);
			return;
		}

		switch (item.kind) {
		case 'file':
			var blob = item.getAsFile();
			blob.name = 'Paste ' + new Date().toISOString() + '.png';
			items.push(blob);
			break;
		case 'string':
			item.getAsString(function(s) {
				var blob = new Blob([s]);
				blob.name = 'Paste ' + new Date().toISOString() + '.txt';
				items.push(blob);
				next();
			});
			return;
		}
		setTimeout(next, 1);
	}

	next();
}

Node.prototype.sacrificeChildren = function() {
	while (this.hasChildNodes()) this.removeChild(this.firstChild);
};

function makesvg(elem) {
	return document.createElementNS("http://www.w3.org/2000/svg", elem);
}

function showMessage(msg, classname) {
	if (timeout != null) window.clearTimeout(timeout);

	var box = $('#message-box');
	box.innerText = msg;
	box.classList.add(classname);
	box.classList.add('active')
	timeout = window.setTimeout(hideMessage, 5000);
}

function hideMessage() {
	$('#message-box').classList.remove('active');
}

function cb(e) {
	if (e.target.status == 204) {
		document.location.reload(true);
	} else {
		var resp = JSON.parse(e.target.responseText);
		if (resp != null && resp.Err != null) {
			showMessage('Error: ' + resp.Err, 'bad');
		} else {
			showMessage('An unknown error occurred (status ' + e.target.status + ')', 'bad');
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

function setURLList(urls) {
	var ul = urlList.querySelector('ul');
	ul.sacrificeChildren();
	for (var i = 0, url, li, a; url = urls[i]; i++) {
		li = document.createElement('li');
		a = document.createElement('a');
		a.href = a.innerText = a.textContent = url;
		li.appendChild(a);
		ul.appendChild(li);
	}
	urlList.classList.add('active');
}

function dropZoneEnter(e) {
	var dt = e.dataTransfer;
	if (dt != null && Array.prototype.indexOf.call(dt.types, 'Files') >= 0) {
		e.preventDefault();
		e.stopPropagation();
		dropZone.classList.add('active');
	}
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
	if (fileList == null || fileList.length == 0) {
		finish();
		return;
	}

	var totalSize = 0;
	for (var i = 0; i < fileList.length; i++) {
		totalSize += fileList[i].size;
	}

	var svg = null;

	if (fileList.length > 1) {
		svg = dropZone.querySelector('svg');
		if (svg == null) {
			svg = makesvg('svg');
			dropZone.appendChild(svg);
		}
		svg.sacrificeChildren();

		for (var i = acc = 0, pos; i < fileList.length; i++) {
			acc += fileList[i].size;
			pos = acc/totalSize * svg.offsetWidth;
			var line = makesvg('line');
			line.setAttribute('x1', pos);
			line.setAttribute('x2', pos);
			line.setAttribute('y1', 0);
			line.setAttribute('y2', dropZone.offsetHeight - 8);
			svg.appendChild(line);
		}
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
			svg.sacrificeChildren();
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
				try {
					switch (this.status) {
					case 201:
						var resp = JSON.parse(this.responseText);
						result.push(window.location.protocol + '//' + resp.URL);
						setTimeout(next, 1, i+1, result, totalLoaded);
						break;
					case 403:
						window.location = '/-/login';
						break;
					default:
						var err = JSON.parse(this.responseText);
						showMessage(err.Err, 'bad');
						break;
					}
				} catch (e) {
					showMessage('Server Error: ' + this.statusText, 'bad');
					console.error('error parsing response: ' + e);
				}
			}, false);

			x.open('POST', '/upload/web', true);
			x.setRequestHeader('X-Airlift-Filename', encodeURIComponent(file.name));
			x.send(file);
		} else {
			finish();
			setURLList(result);
			dropZone.removeEventListener('click', cancel);
			dropZone.addEventListener('click', clickPicker);
			if (svg != null) {
				svg.sacrificeChildren();
			}
		}
	};

	next(0, [], 0);
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

var oldMaxSize, oldMaxAge, sampleID, sampleExt, idSize, addExt;

function reloadOverview() {
	var x = new XMLHttpRequest();
	x.open('GET', '/-/config/overview', true);
	x.addEventListener('load', function(e) {
		if (e.target.status === 200) {
			$('#section-overview').innerHTML = e.target.response;
		}
	});
	x.send();
}

function updateSample() {
	var a = new Array(parseInt(idSize.value));
	for (var i = 0; i < a.length; i++) {
		a[i] = 'X';
	}
	sampleID.textContent = sampleID.innerText = a.join('');

	if (addExt.checked) {
		sampleExt.classList.add('show');
	} else {
		sampleExt.classList.remove('show');
	}
}

function configPage() {
	var buttons = $$('button'), host = $('#host');
	oldMaxSize  = parseInt($('#max-size').value);
	oldMaxAge   = parseInt($('#max-age').value);
	sampleID    = $('#sample-id');
	sampleExt   = $('#sample-ext');
	idSize      = $('#id-size');
	addExt      = $('#append-ext');

	if (host.value === '') {
		host.value = window.location.host;
	}

	updateSample();
	// IE and webkit seem to have different change and input impls
	idSize.addEventListener('change', updateSample, false);
	idSize.addEventListener('input', updateSample, false);
	addExt.addEventListener('change', updateSample, false);

	var boxes = $$('.check-enable');
	for (var i = 0, b; b = boxes[i]; i++) {
		var hider = b.querySelector('.hider');
		hider.hidee = b.querySelector('.hidee input');
		hider.addEventListener('click', function() {
			this.hidee.disabled = !this.checked;
		}, false);
	}

	$('#submit').addEventListener('click', function() {
		for (var i = 0, button; button = buttons[i]; i++) button.setAttribute('disabled', true);
		var maxSize = parseInt($('#max-size').value);
		var maxAge  = parseInt($('#max-age').value);
		var delta   = 0;

		var f = function(url, val) {
			var fd = new FormData();
			fd.append('N', val);
			var x = new XMLHttpRequest();
			x.open('POST', url, false);
			x.send(fd);

			if (x.status == 200) {
				var n = JSON.parse(x.response).N;
				if (n > delta) delta = n;
				return true;
			} else {
				var err = JSON.parse(x.response);
				showMessage('Server error: ' + err.Err + ' (' + x.status + ')', 'bad');
				return false;
			}
		}

		if (maxSize > 0 && (oldMaxSize == 0 || maxSize < oldMaxSize)) {
			if (!f('/-/config/size', maxSize)) return;
		}
		if (maxAge > 0 && (oldMaxAge == 0 || maxAge < oldMaxAge)) {
			if (!f('/-/config/age', maxAge)) return;
		}
		if (delta > 0) {
			if (!confirm('Changes made to age or size limits mean that ' + delta + ' old file(s) will be pruned. Continue?')) {
				return;
			}
		}

		oldMaxAge = maxAge;
		oldMaxSize = maxSize;

		var host   = $('#host');
		host.value = host.value.replace(/\w+:\/\//, '');
			var fd     = new FormData($('#config'));
		var x      = new XMLHttpRequest();

		x.addEventListener('load', function(e) {
			$('#password').value = '';
			for (var i = 0, button; button = buttons[i]; i++) button.removeAttribute('disabled');
			if (e.target.status === 204) {
				showMessage('Configuration updated.', 'good');
				$('#newpass').value = '';
				reloadOverview();
			} else {
				var resp = JSON.parse(x.responseText);
				if (resp != null && resp.Err != null) {
					showMessage('Error: ' + resp.Err, 'bad');
				} else {
					showMessage('An unknown error occurred (status ' + e.target.status + ')', 'bad');
				}
			}
		}, false);

		x.open('POST', '/-/config', true);
		x.send(fd);
	}, false);
}
