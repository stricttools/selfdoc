// Code tabs: switch between language panels
(function() {
  var syncing = false;
  document.querySelectorAll('.code-tabs').forEach(function(tabGroup) {
    var buttons = tabGroup.querySelectorAll('.tab-bar .tab');
    var panels = tabGroup.querySelectorAll('.tab-panel');
    buttons.forEach(function(btn) {
      btn.addEventListener('click', function() {
        if (syncing) return;
        var lang = btn.getAttribute('data-lang');
        buttons.forEach(function(b) { b.classList.remove('active'); b.setAttribute('aria-selected', 'false'); b.setAttribute('tabindex', '-1'); });
        panels.forEach(function(p) { p.classList.remove('active'); });
        btn.classList.add('active');
        btn.setAttribute('aria-selected', 'true');
        btn.setAttribute('tabindex', '0');
        var panel = tabGroup.querySelector('.tab-panel[data-lang="' + lang + '"');
        if (panel) panel.classList.add('active');
        localStorage.setItem('selfdoc-tab-' + lang, 'true');
        syncing = true;
        document.querySelectorAll('.code-tabs').forEach(function(otherGroup) {
          if (otherGroup === tabGroup) return;
          var otherBtn = otherGroup.querySelector('.tab-bar .tab[data-lang="' + lang + '"');
          if (otherBtn) otherBtn.click();
        });
        syncing = false;
      });
    });
    buttons.forEach(function(btn) {
      var lang = btn.getAttribute('data-lang');
      if (localStorage.getItem('selfdoc-tab-' + lang)) {
        btn.click();
      }
    });
    // Initialize roving tabindex
    buttons.forEach(function(b) {
      b.setAttribute('tabindex', b.classList.contains('active') ? '0' : '-1');
    });
    // Keyboard navigation for tabs (WAI-ARIA)
    var tabBar = tabGroup.querySelector('.tab-bar');
    if (tabBar) {
      tabBar.addEventListener('keydown', function(e) {
        if (!e.target.classList.contains('tab')) return;
        var tabs = Array.prototype.slice.call(buttons);
        var idx = tabs.indexOf(e.target);
        var next = -1;
        if (e.key === 'ArrowRight') {
          next = (idx + 1) % tabs.length;
        } else if (e.key === 'ArrowLeft') {
          next = (idx - 1 + tabs.length) % tabs.length;
        } else if (e.key === 'Home') {
          next = 0;
        } else if (e.key === 'End') {
          next = tabs.length - 1;
        }
        if (next >= 0) {
          e.preventDefault();
          tabs[next].focus();
          tabs[next].click();
        }
      });
    }
  });
})();
