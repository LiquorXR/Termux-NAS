// api.js — fetch 封装:超时、Abort、JSON 解析、错误归一、401 统一。
export async function api(path, opts = {}) {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), opts.timeout || 12000);
  const headers = new Headers(opts.headers || {});
  // 合并外部 signal 与内部超时 controller：任一触发均中止请求；
  // 若直接用外部 signal 覆盖，超时保护会失效
  const extSignal = opts.signal;
  let signal = controller.signal;
  let onExtAbort = null;
  if (extSignal) {
    if (typeof AbortSignal.any === 'function') {
      signal = AbortSignal.any([extSignal, controller.signal]);
    } else if (extSignal.aborted) {
      controller.abort();
    } else {
      onExtAbort = () => controller.abort();
      extSignal.addEventListener('abort', onExtAbort, { once: true });
    }
  }
  // 确保同源携带 cookie（会话）
  const fetchOpts = { ...opts, signal, credentials: 'same-origin', headers };
  // 如果是 JSON 且未声明 content-type，自动补（FormData 除外）
  if (fetchOpts.body && typeof fetchOpts.body === 'string' && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json');
  }
  let r;
  try {
    r = await fetch(path, fetchOpts);
  } catch (e) {
    if (e.name === 'AbortError') {
      // 外部主动取消（搜索防抖/轮询切换）→ 抛原 AbortError 供调用方静默；否则为超时触发
      if (extSignal && extSignal.aborted) throw e;
      throw new Error('请求超时，请检查网络');
    }
    throw new Error('网络错误');
  } finally {
    clearTimeout(timeout);
    if (onExtAbort && extSignal) extSignal.removeEventListener('abort', onExtAbort);
  }
  let data = null;
  const ct = r.headers.get('content-type') || '';
  if (ct.includes('application/json')) {
    try { data = await r.json(); } catch (e) { /* 非 JSON */ }
  } else {
    // 尝试 JSON，失败则留空
    try { data = await r.json(); } catch (e) { data = null; }
  }
  if (r.status === 401) {
    // 会话失效 → 回到登录态（SPA 由 /api/auth/status 决定视图）
    // 避免在已是 /login 时循环
    if (!location.pathname.includes('/login')) {
      try { window.dispatchEvent(new CustomEvent('nas:unauthorized')); } catch (e) {}
      window.location.href = '/login';
    }
    throw new Error('未登录');
  }
  if (!r.ok) {
    const msg = (data && (data.error || data.message)) || ('请求失败(' + r.status + ')');
    throw new Error(msg);
  }
  return data;
}

export function humanSize(n) {
  if (n === null || n === undefined || n === '') return '—';
  const v = Number(n);
  if (!Number.isFinite(v) || v < 0) return '—';
  if (v < 1024) return v + ' B';
  const u = ['KB', 'MB', 'GB', 'TB'];
  let i = -1;
  let x = v;
  do { x /= 1024; i++; } while (x >= 1024 && i < u.length - 1);
  return x.toFixed(1) + ' ' + u[i];
}

export function fmtTime(s) {
  if (!s || String(s).indexOf('0001') === 0) return '—';
  const d = new Date(s);
  if (isNaN(d.getTime())) return '—';
  const p = (x) => String(x).padStart(2, '0');
  return d.getFullYear() + '-' + p(d.getMonth() + 1) + '-' + p(d.getDate()) +
    ' ' + p(d.getHours()) + ':' + p(d.getMinutes());
}

export function fmtUptime(sec) {
  if (sec === null || sec === undefined || sec === '') return '—';
  const v = Number(sec);
  if (!Number.isFinite(v)) return '—';
  const h = Math.floor(v / 3600);
  const m = Math.floor((v % 3600) / 60);
  const s = Math.floor(v % 60);
  if (h > 0) return h + 'h' + m + 'm';
  return m + 'm' + s + 's';
}
