// ui.js — 全局 UI 原语:toast / 确认对话框 / 输入对话框 / 按钮 loading。v3 Nocturne
// 依赖 index.html 中的 #toasts、#dialog-mask 结构（已加 aria）。

export function toast(msg, type) {
  const box = document.getElementById('toasts');
  if (!box) return;
  const el = document.createElement('div');
  el.className = 'toast' + (type ? ' ' + type : '');
  el.setAttribute('role', type === 'err' ? 'alert' : 'status');
  el.setAttribute('aria-live', type === 'err' ? 'assertive' : 'polite');
  el.textContent = String(msg || '');
  box.appendChild(el);
  // 限制堆叠数量，避免溢出视口
  const max = 4;
  while (box.children.length > max) box.firstElementChild.remove();
  const dismiss = () => {
    el.style.opacity = '0';
    el.style.transition = 'opacity .28s var(--ease, ease)';
    setTimeout(() => el.remove(), 300);
  };
  let timer = setTimeout(dismiss, 3200);
  el.addEventListener('click', () => { clearTimeout(timer); dismiss(); });
}

let dialogResolve = null;
let dialogCleanup = null;
let lastFocus = null;

// confirmDialog(title, text) → Promise<boolean>
export function confirmDialog(title, text) {
  return showDialog({ title, text, input: false, okText: '确定', danger: true });
}

// promptDialog(title, text, value, placeholder) → Promise<string|null>
export function promptDialog(title, text, value, placeholder) {
  return showDialog({ title, text, input: true, value, placeholder, okText: '确定' });
}

function showDialog(opt) {
  const mask = document.getElementById('dialog-mask');
  const dialog = mask ? mask.querySelector('.dialog') : null;
  const title = document.getElementById('dialog-title');
  const text = document.getElementById('dialog-text');
  const input = document.getElementById('dialog-input');
  const ok = document.getElementById('dialog-ok');
  const cancel = document.getElementById('dialog-cancel');
  if (!mask || !dialog || !title || !ok || !cancel) return Promise.resolve(null);

  // 并发保护：已有对话框则先执行其 cleanup（移除 keydown/trap 监听、还原焦点）再 resolve，
  // 避免旧监听与 dialog-open 类残留
  if (!mask.hidden && dialogResolve) {
    const prevResolve = dialogResolve;
    const prevCleanup = dialogCleanup;
    try { prevCleanup(); } catch (e) {}
    try { prevResolve(opt.input ? null : false); } catch (e) {}
  }

  title.textContent = opt.title || '';
  text.textContent = opt.text || '';
  text.style.display = opt.text ? '' : 'none';
  text.hidden = !opt.text;
  input.hidden = !opt.input;
  if (opt.input) {
    input.value = opt.value || '';
    input.placeholder = opt.placeholder || '';
    input.removeAttribute('aria-hidden');
  } else {
    input.setAttribute('aria-hidden', 'true');
  }
  ok.textContent = opt.okText || '确定';
  ok.classList.toggle('danger', !!opt.danger);
  if (opt.danger) ok.setAttribute('aria-label', '危险操作 确定'); else ok.removeAttribute('aria-label');

  lastFocus = document.activeElement;
  mask.hidden = false;
  document.documentElement.classList.add('dialog-open');
  // 聚焦首个可聚焦元素
  if (opt.input) input.focus();
  else ok.focus();

  return new Promise((resolve) => {
    dialogResolve = resolve;
    dialogCleanup = cleanup;
    const done = (val) => {
      cleanup();
      resolve(val);
    };
    function cleanup() {
      mask.hidden = true;
      document.documentElement.classList.remove('dialog-open');
      ok.onclick = null;
      cancel.onclick = null;
      input.onkeydown = null;
      mask.onclick = null;
      document.removeEventListener('keydown', onKey);
      if (dialog) dialog.removeEventListener('keydown', trap);
      dialogResolve = null;
      dialogCleanup = null;
      // 还原焦点
      if (lastFocus && typeof lastFocus.focus === 'function') {
        try { lastFocus.focus(); } catch (e) {}
      }
      lastFocus = null;
    }
    function onKey(e) {
      if (e.key === 'Escape') {
        e.preventDefault();
        done(opt.input ? null : false);
      }
    }
    function trap(e) {
      if (e.key !== 'Tab') return;
      const focusable = dialog.querySelectorAll('button:not([hidden]), input:not([hidden]), [tabindex]:not([tabindex="-1"])');
      if (focusable.length === 0) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (e.shiftKey) {
        if (document.activeElement === first) { e.preventDefault(); last.focus(); }
      } else {
        if (document.activeElement === last) { e.preventDefault(); first.focus(); }
      }
    }
    ok.onclick = () => done(opt.input ? String(input.value || '').trim() : true);
    cancel.onclick = () => done(opt.input ? null : false);
    input.onkeydown = (e) => {
      if (e.key === 'Enter') {
        e.preventDefault();
        done(opt.input ? String(input.value || '').trim() : true);
      }
    };
    document.addEventListener('keydown', onKey);
    dialog.addEventListener('keydown', trap);
    mask.onclick = (e) => { if (e.target === mask) done(opt.input ? null : false); };
  });
}

// setBtnLoading(btn, busy) — 统一按钮 loading 态，保留可访问名
export function setBtnLoading(btn, busy) {
  if (!btn) return;
  if (busy) {
    if (btn.dataset.old === undefined) btn.dataset.old = btn.innerHTML;
    const label = btn.getAttribute('aria-label') || btn.textContent.trim() || '加载中';
    btn.setAttribute('aria-busy', 'true');
    btn.setAttribute('aria-label', label + ' 加载中');
    btn.disabled = true;
    btn.innerHTML = '<span aria-hidden="true">…</span><span class="sr-only">加载中</span>';
  } else {
    btn.removeAttribute('aria-busy');
    // 还原 aria-label（若之前无则移除）
    // 保留外部设置的 aria-label，不强制清空
    btn.disabled = false;
    if (btn.dataset.old !== undefined) {
      btn.innerHTML = btn.dataset.old;
      delete btn.dataset.old;
    }
    // 若按钮原本有 aria-label，恢复后仍保留；否则移除 busy 时的 label
    // 简单策略：若当前 label 以 " 加载中" 结尾则去掉
    const cur = btn.getAttribute('aria-label');
    if (cur && cur.endsWith(' 加载中')) {
      const orig = cur.slice(0, -4);
      if (orig) btn.setAttribute('aria-label', orig); else btn.removeAttribute('aria-label');
    }
  }
}
