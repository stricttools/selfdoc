// Copy button on code blocks
//
// The framework's copy button is a .copy-btn holding an icon, and the slot
// it goes in is .tm-code-actions -- the sheet reserves the slot and does no
// copying, so the click is the consumer's.  .copied is the framework's
// momentary state class after a successful copy.
(function() {
  var COPY = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="8" y="8" width="12" height="12"/><path d="M4 16V4h12"/></svg>';
  var DONE = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M4 12.5l5 5L20 6"/></svg>';
  document.querySelectorAll('.tm-code').forEach(function(figure) {
    var code = figure.querySelector('pre code');
    var slot = figure.querySelector('.tm-code-actions');
    if (!code || !slot) return;
    var btn = document.createElement('button');
    btn.className = 'copy-btn';
    btn.type = 'button';
    btn.setAttribute('aria-label', 'Copy code');
    btn.innerHTML = COPY;
    btn.addEventListener('click', function() {
      navigator.clipboard.writeText(code.textContent).then(function() {
        btn.classList.add('copied');
        btn.innerHTML = DONE;
        setTimeout(function() {
          btn.classList.remove('copied');
          btn.innerHTML = COPY;
        }, 2000);
      });
    });
    slot.appendChild(btn);
  });
})();
