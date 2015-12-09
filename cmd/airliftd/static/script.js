/*** Common ***/

var messageTimeout;

function $(sel, root) {
	return (root || document).querySelector(sel);
}

function $$(sel, root) {
	return (root || document).querySelectorAll(sel);
}

Node.prototype.sacrificeChildren = function() {
	while (this.hasChildNodes()) this.removeChild(this.firstChild);
};

function chain(f) {
    return {
        i: 0,
        funcs: f != null ? [f] : [],
        err: null,
        catcher: null,
        then: function(g) {
            this.funcs = this.funcs || [];
            this.funcs.push(g);
            return this;
        },
        pass: function() {
            if (this.i < this.funcs.length) {
                var args = [this.pass.bind(this), this.fail.bind(this)];
                if (arguments != null) {
                    args = args.concat(Array.prototype.slice.call(arguments));
                }
                this.funcs[this.i++].apply(this, args);
            }
            return this;
        },
        fail: function(err) {
            if (this.catcher != null) {
                this.catcher(err);
            }
            return this;
        },
        catch: function(g) {
            this.catcher = g;
            return this;
        }
    };
}

function makesvg(elem) {
	return document.createElementNS("http://www.w3.org/2000/svg", elem);
}

function showMessage(msg, classname) {
	if (messageTimeout != null) {
		window.clearTimeout(messageTimeout);
	}

	var box = $('#message-box');
	box.innerText = msg;
	box.classList.add(classname);
	box.classList.add('active')
	messageTimeout = window.setTimeout(hideMessage, 5000);
}

function hideMessage() {
	$('#message-box').classList.remove('active');
}

function errorMessage(resp) {
	var err = resp.Err || resp;
	if (err != null) {
		showMessage('Error: ' + err, 'bad');
	} else {
		console.error('errorMessage: malformed error object');
		console.log(resp);
		showMessage('An unknown error occurred (status ' + code + ')', 'bad');
	}
}

// method string
// url    string
// data   Object - post form data or null
// async  Boolean
// cb     function(code int, resp Object) - callback
// mutate function(x XMLHttpRequest, afteropen Boolean) [opt] -
//  callback to mutate xhr before request
function json(method, url, data, async, cb, mutate) {
	var x = new XMLHttpRequest();
	var h = function(x) {
		var resp = {};
		if (x.response != '') {
			try {
				resp = JSON.parse(x.response);
			} catch (err) {
				console.error(err);
				showMessage('Something\'s wrong with the server (status ' + code + ')', 'bad');
				return false;
			}
		}
		return cb(x.status, resp);
	};
	if (async) {
		x.addEventListener('load', function(e) { h(e.target); }, false);
	}
	if (mutate != null) {
		mutate(x, false);
	}
	x.open(method, url, async);
	if (mutate != null) {
		mutate(x, true);
	}
	x.send(data);
	if (!async) {
		return h(x);
	}
}

/*** Uploader ***/

var dropZone, dropZoneText, picker, urlList, bar;

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

	var c = chain();

	for (var i = 0; i < fileList.length; i++) {
		(function(file) {
			c.then(function(pass, fail, result) {
				json('POST', '/upload/web', file, true, function(code, resp) {
					switch (code) {
					case 201:
						result.push(window.location.protocol + '//' + resp.URL);
						setTimeout(next, 1, i+1, result, totalLoaded);
						pass(result);
						break;
					case 403:
						window.location = '/-/login';
						break;
					default:
						fail(resp);
						break;
					}
				}, function(x, afteropen) {
					if (!afteropen) {
						x.upload.addEventListener('progress', function(e) {
							if (e.lengthComputable) {
								bar.style.width = ((totalLoaded + e.loaded)*100 / totalSize) + '%';
							}
						}, false);

						x.upload.addEventListener('load', function() {
							totalLoaded += file.size;
							bar.style.width = totalLoaded*100 / totalSize + '%';
						}, false);
					} else {
						x.setRequestHeader('X-Airlift-Filename', encodeURIComponent(file.name));
					}
				});
			});
		})(fileList[i]);
	}

	c.then(function(pass, fail, result) {
		finish();
		setURLList(result);
		dropZone.removeEventListener('click', cancel);
		dropZone.addEventListener('click', clickPicker);
		if (svg != null) {
			svg.sacrificeChildren();
		}
	}).catch(errorMessage).pass([]);
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

/*** Config ***/

var oldMaxSize, oldMaxAge, sampleID, sampleExt, idSize, addExt;

function reloadSection(endpoint, target) {
	var x = new XMLHttpRequest();
	x.addEventListener('load', function(e) {
		var section    = $(target);
		var newSection = $(target, e.target.response);
		section.parentNode.replaceChild(newSection, section);
	}, false);
	x.open('GET',endpoint, true);
	x.responseType = 'document';
	x.setRequestHeader('X-Ajax-Partial', 1);
	x.send();
}

function reloadConfigValues() {
	reloadSection('/-/config', '#section-config');
}

function reloadOverview() {
	reloadSection('/-/config/overview', '#section-overview');
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

function purgeDone(code, resp) {
	if (code == 204) {
		reloadOverview();
	} else {
		errorMessage(resp);
	}
}

function purgeAll() {
	var str = 'Really delete all of your uploads?\n\n' +
		'Once they\'re gone, the\'re really gone.';
	if (!window.confirm(str)) {
		return;
	}

	json('POST', '/purge/all', null, true, purgeDone);
}

function purgeThumbs() {
	json('POST', '/purge/thumbs', null, true, purgeDone);
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
			if (this.checked) {
				this.hidee.removeAttribute('disabled');
			} else {
				this.hidee.setAttribute('disabled', 'disabled');
			}
		}, false);
	}

	$('#submit').addEventListener('click', function() {
		for (var i = 0, button; button = buttons[i]; i++) {
			button.setAttribute('disabled', true);
		}
		var maxSize = parseInt($('#max-size').value);
		var maxAge  = parseInt($('#max-age').value);
		var delta   = 0;
		var f = function(url, val, pass, fail) {
			var fd = new FormData();
			fd.append('N', val);

			json('POST', url, fd, true, function(code, resp) {
				if (code === 200) {
					if (resp.N > delta) delta = resp.N;
					pass();
				} else {
					fail(resp);
				}
			});
		};

		chain(function(pass, fail) {
			if (maxSize > 0 && (oldMaxSize == 0 || maxSize < oldMaxSize)) {
				f('/-/config/size', maxSize, pass, fail);
			} else {
				pass();
			}
		}).then(function(pass, fail) {
			if (maxAge > 0 && (oldMaxAge == 0 || maxAge < oldMaxAge)) {
				f('/-/config/age', maxAge, pass, fail);
			} else {
				pass();
			}
		}).then(function(pass, fail) {
			if (delta > 0) {
				if (!confirm('Changes made to age or size limits mean that ' + delta + ' old file(s) will be pruned. Continue?')) {
					return;
				}
			}

			oldMaxAge  = maxAge;
			oldMaxSize = maxSize;

			var host   = $('#host');
			host.value = host.value.replace(/\w+:\/\//, '');
			var fd     = new FormData($('#config'));

			json('POST', '/-/config', fd, true, function(code, resp) {
				$('#password').value = '';

				for (var i = 0, button; button = buttons[i]; i++) {
					button.removeAttribute('disabled');
				}

				if (code === 204) {
					$('#newpass').value = '';
					reloadConfigValues();
					reloadOverview();
					showMessage('Configuration updated.', 'good');
					pass();
				} else {
					fail(resp);
				}
			});
		}).catch(errorMessage).pass();
	}, false);
}
