(function() {
	'use strict';

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

	window.addEventListener('DOMContentLoaded', function() {
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
	}, false);
})();
