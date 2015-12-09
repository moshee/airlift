(function() {
	'use strict';

	function bindHistoryItem(item) {
		var a = item.querySelector('a.delete-upload');
		a.addEventListener('click', function() {
			item.style.opacity = '0.5';
			var path = '/-/delete/' + item.dataset.id;

			json('POST', path, null, true, function(code, resp) {
				if (code == 204) {
					item.style.opacity = '0.0';
					item.addEventListener('transitionend', function(e) {
						reloadSection(window.location.pathname, '#history', setupHistory);
					}, false);
				} else {
					item.style.opacity = '';
					errorMessage(resp);
				}
			});
		}, false);
	}

	function setupHistory() {
		var items = $$('.history-item');
		Array.prototype.forEach.call(items, bindHistoryItem);
	}

	window.addEventListener('DOMContentLoaded', setupHistory, true);
})();
