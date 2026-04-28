// collab-ai · landing page
// One job: copy code blocks to the clipboard with a tiny confirmation flash.

(() => {
  const toast = document.getElementById('toast');
  let toastTimer;

  function showToast(text) {
    if (!toast) return;
    toast.textContent = text;
    toast.dataset.show = '1';
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { toast.dataset.show = '0'; }, 1400);
  }

  document.querySelectorAll('.copy-btn').forEach((btn) => {
    btn.addEventListener('click', async () => {
      const text = btn.dataset.copy || '';
      if (!text) return;

      try {
        await navigator.clipboard.writeText(text);
      } catch {
        // Fallback for older / non-https contexts.
        const ta = document.createElement('textarea');
        ta.value = text;
        ta.setAttribute('readonly', '');
        ta.style.position = 'fixed';
        ta.style.opacity = '0';
        document.body.appendChild(ta);
        ta.select();
        try { document.execCommand('copy'); } catch { /* swallow */ }
        document.body.removeChild(ta);
      }

      const label = btn.querySelector('.copy-label');
      const original = label ? label.textContent : '';
      btn.dataset.state = 'copied';
      if (label) label.textContent = 'Copied';
      showToast('Copied to clipboard');

      setTimeout(() => {
        btn.dataset.state = '';
        if (label) label.textContent = original;
      }, 1400);
    });
  });
})();
