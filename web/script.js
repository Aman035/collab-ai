// collab landing page
// 1. Loop a fake collab session through the hero TUI (peers join, statuses
//    tick, log entries flow, files arrive, then reset).
// 2. Wire copy-to-clipboard on the install code blocks.

(() => {

  // ───────────── 1. TUI animation ─────────────

  const peerList  = document.getElementById('peer-list');
  const peerCount = document.getElementById('peer-count');
  const logList   = document.getElementById('log-list');
  const logCount  = document.getElementById('log-count');
  const fileList  = document.getElementById('file-list');
  const fileCount = document.getElementById('file-count');
  const aliceStatus = document.getElementById('status-alice');

  // The script that plays in a loop.
  // Each step: { at: ms-since-start, do: fn(state) }
  const script = [
    { at:   600, do: setStatus('alice', 'reading the cache eviction code') },
    { at:  1800, do: addPeer({ name: 'bob', label: 'joined just now' }) },
    { at:  3000, do: setStatus('bob', 'pulling the spec from PLAN.md') },
    { at:  4400, do: addLog({ time: '14:32', who: 'bob', text: 'opened cache.go and the test', kind: 'post' }) },
    { at:  5600, do: setStatus('alice', 'looking at line 42') },
    { at:  6800, do: addLog({ time: '14:32', who: 'alice', text: 'i think there\'s an off-by-one in the eviction loop', kind: 'post', self: true }) },
    { at:  8200, do: addFile({ name: 'cache.go', size: '2.4KB', src: 'bob', age: '20s ago' }) },
    { at:  9500, do: setStatus('alice', 'writing widget.go') },
    { at: 10600, do: addLog({ time: '14:33', who: 'alice', text: 'review widget.go for me?', kind: 'question', target: 'bob', self: true }) },
    { at: 11900, do: setStatus('bob', 'reviewing widget.go') },
    { at: 13400, do: addLog({ time: '14:33', who: 'bob', text: 'lgtm, ship it', kind: 'answer' }) },
    { at: 14600, do: addFile({ name: 'widget.go', size: '1.1KB', src: 'alice', age: '6s ago' }) },
    { at: 16200, do: setStatus('alice', 'merging') },
    { at: 17400, do: setStatus('bob', 'idle') },
    { at: 19000, do: reset() },
  ];

  let state = freshState();
  let scheduled = [];
  let raf = null;

  function freshState() {
    return {
      peers:    new Map([['alice', { name: 'alice', self: true, status: '' }]]),
      log:      [],
      files:    [],
    };
  }

  function setStatus(handle, text) {
    return () => {
      const p = state.peers.get(handle);
      if (!p) return;
      p.status = text;
      renderPeers();
    };
  }

  function addPeer(p) {
    return () => {
      state.peers.set(p.name, { name: p.name, self: false, status: '' });
      renderPeers();
    };
  }

  function addLog(e) {
    return () => {
      state.log.push(e);
      renderLog();
    };
  }

  function addFile(f) {
    return () => {
      state.files.push(f);
      renderFiles();
    };
  }

  function reset() {
    return () => {
      state = freshState();
      renderPeers();
      renderLog();
      renderFiles();
    };
  }

  // Renderers: small, intentional. Re-render the whole sub-tree each time
  // (the trees are tiny). Animations come from CSS keyframes triggered by
  // the new elements being inserted.

  function renderPeers() {
    peerCount.textContent = state.peers.size;
    peerList.innerHTML = '';
    for (const p of state.peers.values()) {
      const li = document.createElement('li');
      li.className = 'peer' + (p.self ? ' self' : '');
      li.innerHTML = `
        <span class="peer-dot ${p.self ? 'self' : ''}">●</span>
        <span class="peer-name ${p.self ? 'self' : ''}">${p.name}</span>
        ${p.self ? '<span class="dim">· you</span>' : '<span class="dim">· just joined</span>'}
        <div class="peer-status">${p.status ? escapeHTML(p.status) : ''}</div>
      `;
      peerList.appendChild(li);
    }
  }

  function renderLog() {
    logCount.textContent = state.log.length;
    logList.innerHTML = '';
    for (const e of state.log) {
      const li = document.createElement('li');
      li.className = 'entry';
      let tag = '';
      if (e.kind === 'question') tag = `<span class="tag-q">?→${e.target}</span>`;
      if (e.kind === 'answer')   tag = `<span class="tag-a">←answer</span>`;
      li.innerHTML = `
        <span class="time">${e.time}</span>
        <span class="who ${e.self ? 'who-self' : ''}">${e.who}</span>
        ${tag}
        <span class="dim">:</span>
        <span class="text">${escapeHTML(e.text)}</span>
      `;
      logList.appendChild(li);
    }
  }

  function renderFiles() {
    fileCount.textContent = state.files.length;
    fileList.innerHTML = '';
    if (state.files.length === 0) {
      const li = document.createElement('li');
      li.className = 'empty dim';
      li.textContent = '(empty)';
      fileList.appendChild(li);
      return;
    }
    for (const f of state.files) {
      const li = document.createElement('li');
      li.innerHTML = `
        <span class="name">${escapeHTML(f.name)}</span>
        <span class="size">${f.size}</span>
        <span class="src">from ${f.src} · ${f.age}</span>
      `;
      fileList.appendChild(li);
    }
  }

  function escapeHTML(s) {
    return String(s).replace(/[&<>"']/g, ch => (
      { '&':'&amp;', '<':'&lt;', '>':'&gt;', '"':'&quot;', "'":'&#39;' }[ch]
    ));
  }

  // Animation loop. Runs forever, restarting the script by reset() at the end.
  let loopStart = performance.now();
  let nextStep  = 0;
  function tick(now) {
    const elapsed = now - loopStart;
    while (nextStep < script.length && script[nextStep].at <= elapsed) {
      script[nextStep].do();
      nextStep++;
    }
    if (nextStep >= script.length) {
      // give the reset a moment to render before restarting
      loopStart = now + 200;
      nextStep = 0;
    }
    raf = requestAnimationFrame(tick);
  }

  // Pause animation when the page tab is hidden, to save cycles.
  function start() {
    loopStart = performance.now();
    nextStep = 0;
    state = freshState();
    renderPeers(); renderLog(); renderFiles();
    raf = requestAnimationFrame(tick);
  }
  function stop() {
    if (raf) cancelAnimationFrame(raf);
    raf = null;
  }
  document.addEventListener('visibilitychange', () => {
    if (document.hidden) stop();
    else if (!raf) start();
  });

  // Initial render + start
  renderPeers();
  start();


  // ───────────── 2. Clipboard copy on install blocks ─────────────

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
      showToast('copied to clipboard');
      setTimeout(() => {
        btn.dataset.state = '';
        if (label) label.textContent = original;
      }, 1400);
    });
  });

})();
