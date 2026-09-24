// Scrollspy for the table of contents
//
// The entries are the framework's anchor spelling, so the state they carry
// is aria-current="page" -- the static counterpart of the .active class a
// router would toggle, and what a.docs-toc-item[aria-current] paints.  The
// scrolling element is #tm-content, so positions are measured against its
// box rather than against the viewport.
(function() {
  var tocLinks = document.querySelectorAll('.docs-toc-item');
  var scroller = document.getElementById('tm-content');
  if (!tocLinks.length || !scroller) return;
  var headings = [];
  tocLinks.forEach(function(link) {
    var id = link.getAttribute('href').substring(1);
    var el = document.getElementById(id);
    if (el) headings.push({ el: el, link: link });
  });
  var ticking = false;
  function update() {
    var top = scroller.getBoundingClientRect().top;
    var active = null;
    for (var i = 0; i < headings.length; i++) {
      if (headings[i].el.getBoundingClientRect().top - top <= 100) {
        active = headings[i];
      }
    }
    tocLinks.forEach(function(a) { a.removeAttribute('aria-current'); });
    if (active) {
      active.link.setAttribute('aria-current', 'page');
      active.link.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    }
    ticking = false;
  }
  scroller.addEventListener('scroll', function() {
    if (!ticking) {
      requestAnimationFrame(update);
      ticking = true;
    }
  }, { passive: true });
  update();
})();
