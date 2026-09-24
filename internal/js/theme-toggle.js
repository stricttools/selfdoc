// Theme toggle
(function(){
  var btn = document.querySelector('.theme-toggle');
  var states = ['system', 'light', 'dark'];
  function getState() {
    var s = localStorage.getItem('selfdoc-theme');
    return (s === 'light' || s === 'dark') ? s : 'system';
  }
  function apply(state) {
    if (state === 'light' || state === 'dark') {
      document.documentElement.setAttribute('data-theme', state);
      localStorage.setItem('selfdoc-theme', state);
    } else {
      document.documentElement.removeAttribute('data-theme');
      localStorage.removeItem('selfdoc-theme');
    }
    btn.setAttribute('data-state', state);
    var labels = {system: 'Theme: system. Click for light mode', light: 'Theme: light. Click for dark mode', dark: 'Theme: dark. Click for system theme'};
    btn.setAttribute('aria-label', labels[state]);
  }
  apply(getState());
  btn.addEventListener('click', function() {
    var cur = getState();
    var next = states[(states.indexOf(cur) + 1) % states.length];
    apply(next);
  });
})();
