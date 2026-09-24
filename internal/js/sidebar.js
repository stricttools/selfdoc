// Mobile sidebar drawer
//
// The framework's shell paints the drawer -- #tm-sidebar slides in when
// #tm-app carries .sidebar-open, over a backdrop -- but ships no toggle a
// server-emitted page can use: its own wiring lives inside mountShell(),
// which builds the frame from nothing and would throw this page's shell
// away. So the click, the focus trap and the Escape key are selfdoc's, and
// the class they toggle is the framework's.
(function() {
  var toggle = document.querySelector('.tm-hamburger');
  var sidebar = document.getElementById('tm-sidebar');
  var app = document.getElementById('tm-app');
  if (!toggle || !sidebar || !app) return;
  function openSidebar() {
    app.classList.add('sidebar-open');
    toggle.setAttribute('aria-expanded', 'true');
    var focusable = sidebar.querySelectorAll('a, button, input, [tabindex]');
    if (focusable.length) {
      focusable[0].focus();
      sidebar.addEventListener('keydown', trapFocus);
    }
  }
  function closeSidebar() {
    app.classList.remove('sidebar-open');
    toggle.setAttribute('aria-expanded', 'false');
    sidebar.removeEventListener('keydown', trapFocus);
    toggle.focus();
  }
  function trapFocus(e) {
    if (e.key !== 'Tab') return;
    var focusable = sidebar.querySelectorAll('a, button, input, [tabindex]');
    if (!focusable.length) return;
    var first = focusable[0];
    var last = focusable[focusable.length - 1];
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first.focus();
    }
  }
  toggle.addEventListener('click', function() {
    if (app.classList.contains('sidebar-open')) {
      closeSidebar();
    } else {
      openSidebar();
    }
  });
  document.addEventListener('keydown', function(e) {
    if (e.key === 'Escape' && app.classList.contains('sidebar-open')) {
      closeSidebar();
    }
  });
  document.addEventListener('click', function(e) {
    if (app.classList.contains('sidebar-open') &&
        !sidebar.contains(e.target) && !toggle.contains(e.target)) {
      closeSidebar();
    }
  });
  sidebar.querySelectorAll('a').forEach(function(link) {
    link.addEventListener('click', function() {
      closeSidebar();
    });
  });
})();
