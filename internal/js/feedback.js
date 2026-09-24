// Feedback widget
(function() {
  var widget = document.querySelector('.feedback');
  if (!widget) return;
  var key = 'selfdoc-feedback-' + location.pathname;
  if (localStorage.getItem(key)) {
    widget.innerHTML = '<span>Thanks for your feedback!</span>';
    return;
  }
  var webhook = widget.getAttribute('data-webhook');
  var gaId = widget.getAttribute('data-ga');
  function sendFeedback(vote, comment) {
    if (webhook) {
      var payload = {
        page: location.pathname,
        vote: vote,
        timestamp: new Date().toISOString(),
        user_agent: navigator.userAgent
      };
      if (comment) payload.comment = comment;
      fetch(webhook, {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(payload)
      }).catch(function() {});
    }
    if (gaId && typeof gtag === 'function') {
      gtag('event', 'selfdoc_feedback', {
        page_path: location.pathname,
        vote: vote
      });
    }
    var data = {vote: vote};
    if (comment) data.comment = comment;
    localStorage.setItem(key, JSON.stringify(data));
    widget.innerHTML = '<span>Thanks for your feedback!</span>';
  }
  widget.querySelectorAll('button').forEach(function(btn) {
    btn.addEventListener('click', function() {
      var vote = btn.className.indexOf('yes') !== -1 ? 'yes' : 'no';
      if (vote === 'yes') {
        sendFeedback('yes');
        return;
      }
      widget.innerHTML = '<input type="text" placeholder="What were you looking for?" class="feedback-input"><button class="feedback-submit">Send</button>';
      var input = widget.querySelector('.feedback-input');
      var submitBtn = widget.querySelector('.feedback-submit');
      var timer = setTimeout(function() { sendFeedback('no', input.value); }, 10000);
      submitBtn.addEventListener('click', function() {
        clearTimeout(timer);
        sendFeedback('no', input.value);
      });
    });
  });
})();
