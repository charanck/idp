// Dark mode toggle. The initial theme (from localStorage, or the OS
// preference on first visit) is already applied by the blocking inline
// script in base.templ's <head> before this file loads - this only wires up
// the toggle button and persists subsequent changes.
(function () {
	function setTheme(dark) {
		document.documentElement.classList.toggle("dark", dark);
		try {
			localStorage.setItem("cp-theme", dark ? "dark" : "light");
		} catch (e) {}
	}

	document.addEventListener("DOMContentLoaded", function () {
		var btn = document.getElementById("theme-toggle");
		if (!btn) return;
		btn.addEventListener("click", function () {
			setTheme(!document.documentElement.classList.contains("dark"));
		});
	});
})();
