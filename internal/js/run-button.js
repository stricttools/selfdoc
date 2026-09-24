// Embedded live code playground
(function() {
  document.querySelectorAll('.tm-code[data-run="true"]').forEach(function(block) {
    var label = block.querySelector('.tm-code-label');
    if (!label) return;
    var lang = label.textContent.trim().toLowerCase();
    var code = block.querySelector('code');
    if (!code) return;
    var url = null;
    if (lang === 'go') {
      url = 'https://go.dev/play/p/?body=' + encodeURIComponent(code.textContent);
    } else if (lang === 'python') {
      url = 'https://www.online-python.com/';
    }
    if (url) {
      var btn = document.createElement('a');
      btn.className = 'run-btn';
      btn.href = url;
      btn.target = '_blank';
      btn.rel = 'noopener';
      btn.textContent = 'Run';
      block.querySelector('pre').appendChild(btn);
    }
  });
})();
