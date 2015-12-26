(function() {
	'use strict';

	var dropZone, dropZoneText, picker, urlList, bar;

	function paste(e) {
		var item;
		var c = chain();

		for (var i = 0; i < e.clipboardData.items.length; i++) {
			(function(item) {
				c.then(function(pass, fail, items) {
					switch (item.kind) {
					case 'file':
						var blob = item.getAsFile();
						blob.name = 'Paste ' + new Date().toISOString() + '.png';
						items.push(blob);
						pass(items);
						break;

					case 'string':
						item.getAsString(function(s) {
							var blob = new Blob([s]);
							blob.name = 'Paste ' + new Date().toISOString() + '.txt';
							items.push(blob);
							pass(items);
						});
						break;
					}
				});
			})(e.clipboardData.items[i]);
		}

		c.then(function(pass, fail, items) {
			uploadFiles(items);
		}).pass([]);
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
		var svg, err, x;

		for (var i = 0; i < fileList.length; i++) {
			totalSize += fileList[i].size;
		}

		if (fileList.length > 1) {
			svg = dropZone.querySelector('svg');
			if (svg == null) {
				svg = makesvg('svg');
				dropZone.appendChild(svg);
			}
			svg.sacrificeChildren();

			var i, acc, pos;

			for (i = acc = 0; i < fileList.length; i++) {
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
				c.then(function(pass, fail, result, totalLoaded) {
					json('POST', '/upload/web', file, function(code, resp) {
						switch (code) {
						case 201:
							result.push(window.location.protocol + '//' + resp.URL);
							pass(result, totalLoaded);
							break;
						case 403:
							redirectLogin();
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
		}).catch(errorMessage).pass([], 0);
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

	window.addEventListener('DOMContentLoaded', function() {
		dropZone     = $('#drop-zone');
		dropZoneText = $('#drop-zone-text');
		picker       = $('#picker');
		urlList      = $('#uploaded-urls');
		bar          = dropZone.querySelector('.progress-bar');

		picker.addEventListener('change', function(e) {
			uploadFiles(this.files);
		}, false);

		window.addEventListener('paste', paste, false);

		enable();
	}, false);
})();
