// Re-enable smooth scroll after initial load
requestAnimationFrame(function() {
  requestAnimationFrame(function() {
    document.documentElement.style.scrollBehavior = '';
  });
});
