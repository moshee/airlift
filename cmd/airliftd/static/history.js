(function() {
	'use strict';

	function bindHistoryItem(item) {
		var a = item.querySelector('a.delete-upload');
		a.addEventListener('click', function() {
			item.style.opacity = '0.5';
			var path = '/-/delete/' + item.dataset.id;

			json('POST', path, null, function(code, resp) {
				switch (code) {
				case 204:
					item.style.opacity = '0.0';
					item.addEventListener('transitionend', function(e) {
						reloadSection(window.location.pathname, '#history', setupHistory);
					}, false);
					break;
				case 403:
					redirectLogin();
					break;
				default:
					item.style.opacity = '';
					errorMessage(resp);
					break;
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
