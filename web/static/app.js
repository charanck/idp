// Shared Alpine.js components + htmx wiring for the SSR pages. Alpine picks
// up new markup swapped in by htmx automatically (it watches the DOM via
// MutationObserver), so no manual re-init is needed after an hx-swap.

document.addEventListener("alpine:init", function () {
	// Toast store: any page can push a message via
	// `Alpine.store('toasts').push('success', 'Saved.')`, or a swapped-in
	// fragment can trigger one declaratively with
	// `hx-trigger="load" x-on:htmx:after-request="..."`. Handlers also emit
	// toasts server-side via the HX-Trigger response header (see web/htmx.go),
	// caught by the window listener below.
	Alpine.store("toasts", {
		items: [],
		nextID: 1,
		push: function (tag, text) {
			var id = this.nextID++;
			this.items.push({ id: id, tag: tag, text: text });
			var self = this;
			setTimeout(function () {
				self.remove(id);
			}, 5000);
		},
		remove: function (id) {
			this.items = this.items.filter(function (t) {
				return t.id !== id;
			});
		},
	});

});

// Flash messages auto-dismiss after a delay, in addition to the existing
// manual close button.
document.addEventListener("alpine:init", function () {
	Alpine.data("autoDismiss", function (delayMs) {
		return {
			visible: true,
			init: function () {
				var self = this;
				setTimeout(function () {
					self.visible = false;
				}, delayMs || 6000);
			},
		};
	});
});

// Searchable tag-style multiselect (Applications access lists on the Group
// and Service Client edit forms). Progressive enhancement over a plain
// checkbox list: the real `<input type="checkbox">` elements stay in the DOM
// as the source of truth for form submission (works with JS disabled), this
// component just adds search-filtering and a "selected as chips" summary on
// top via x-model, which Alpine binds to an array automatically for a group
// of same-name checkboxes.
document.addEventListener("alpine:init", function () {
	Alpine.data("multiselect", function (optionsElId, selectedElId) {
		return {
			query: "",
			options: [],
			selected: [],
			init: function () {
				var optionsEl = document.getElementById(optionsElId);
				var selectedEl = document.getElementById(selectedElId);
				this.options = optionsEl ? JSON.parse(optionsEl.textContent) : [];
				this.selected = selectedEl ? JSON.parse(selectedEl.textContent) : [];
			},
			label: function (id) {
				var opt = this.options.find(function (o) {
					return o.id === id;
				});
				return opt ? opt.name : id;
			},
			filtered: function () {
				var q = this.query.trim().toLowerCase();
				if (!q) return this.options;
				return this.options.filter(function (o) {
					return o.name.toLowerCase().indexOf(q) !== -1;
				});
			},
			remove: function (id) {
				this.selected = this.selected.filter(function (s) {
					return s !== id;
				});
			},
		};
	});
});

// Server-triggered toasts: handlers set the `HX-Trigger` response header to
// `{"toast": {"tag": "success", "text": "Saved."}}` (see web/htmx.go's
// TriggerToast helper) instead of relying on a full-page flash message when
// responding to an htmx request.
document.body.addEventListener("toast", function (evt) {
	if (!window.Alpine) return;
	var detail = evt.detail || {};
	Alpine.store("toasts").push(detail.tag || "info", detail.text || "");
});
