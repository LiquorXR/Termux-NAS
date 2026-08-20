// ui.js — 全局 UI 原语:toast / 确认对话框 / 输入对话框 / 按钮 loading。
// 依赖 index.html 中的 #toasts、#dialog-mask 结构。

export function toast(msg, type) {
  const box = document.getElementById('toasts');
  if (!box) return;
  const el = document.createElement('div');
  el.className = 'toast' + (type ? ' ' + type : '');
  el.textContent = msg;
  box.appendChild(el);
  setTimeout(() => {
    el.style.opacity = '0';
    el.style.transition = 'opacity .3s';
    setTimeout(() => el.remove(), 320);
  }, 3200);
}

let dialogResolve = null;

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
  const title = document.getElementById('dialog-title');
  const text = document.getElementById('dialog-text');
  const input = document.getElementById('dialog-input');
  const ok = document.getElementById('dialog-ok');
  const cancel = document.getElementById('dialog-cancel');

  title.textContent = opt.title || '';
  text.textContent = opt.text || '';
  text.style.display = opt.text ? '' : 'none';
  input.hidden = !opt.input;
  if (opt.input) {
    input.value = opt.value || '';
    input.placeholder = opt.placeholder || '';
  }
  ok.textContent = opt.okText || '确定';
  ok.classList.toggle('danger', !!opt.danger);
  mask.hidden = false;
  if (opt.input) input.focus();

  return new Promise((resolve) => {
    dialogResolve = resolve;
    const done = (val) => {
      cleanup();
      resolve(val);
    };
    function cleanup() {
      mask.hidden = true;
      ok.onclick = null;
      cancel.onclick = null;
      input.onkeydown = null;
      mask.onclick = null;
      dialogResolve = null;
    }
    ok.onclick = () => done(opt.input ? input.value.trim() : true);
    cancel.onclick = () => done(opt.input ? null : false);
    input.onkeydown = (e) => {
      if (e.key === 'Enter') done(opt.input ? input.value.trim() : true);
      if (e.key === 'Escape') done(opt.input ? null : false);
    };
    mask.onclick = (e) => { if (e.target === mask) done(opt.input ? null : false); };
  });
}

// setBtnLoading(btn, busy) — 统一按钮 loading 态
export function setBtnLoading(btn, busy) {
  if (!btn) return;
  if (busy) {
    if (btn.dataset.old === undefined) btn.dataset.old = btn.innerHTML;
    btn.disabled = true;
    btn.innerHTML = '…';
  } else {
    btn.disabled = false;
    if (btn.dataset.old !== undefined) {
      btn.innerHTML = btn.dataset.old;
      delete btn.dataset.old;
    }
  }
}
