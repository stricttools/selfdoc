// Heading anchor copy toast
(function() {
  document.querySelectorAll('.heading-link').forEach(function(link) {
    link.addEventListener('click', function(e) {
      e.preventDefault();
      var url = window.location.origin + window.location.pathname + link.getAttribute('href');
      history.replaceState(null, '', link.getAttribute('href'));
      navigator.clipboard.writeText(url).then(function() {
        var toast = document.createElement('span');
        toast.className = 'copy-toast';
        toast.textContent = 'Link copied!';
        link.parentElement.style.position = 'relative';
        link.parentElement.appendChild(toast);
        setTimeout(function() { toast.remove(); }, 2000);
      });
    });
  });
})();
