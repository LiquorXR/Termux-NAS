// api.js — fetch 封装:JSON 解析、错误归一、401 统一跳登录。
export async function api(path, opts = {}) {
  let r;
  try {
    r = await fetch(path, opts);
  } catch (e) {
    throw new Error('网络错误');
  }
  let data = null;
  try { data = await r.json(); } catch (e) { /* 非 JSON 响应 */ }
  if (r.status === 401) {
    // 会话失效 → 回到登录态(后端 /login 由 SPA 渲染登录视图)
    window.location.href = '/login';
    throw new Error('未登录');
  }
  if (!r.ok) {
    throw new Error((data && data.error) || ('请求失败(' + r.status + ')'));
  }
  return data;
}

export function humanSize(n) {
  if (!n && n !== 0) return '—';
  if (n < 1024) return n + ' B';
  const u = ['KB', 'MB', 'GB', 'TB'];
  let i = -1;
  do { n /= 1024; i++; } while (n >= 1024 && i < u.length - 1);
  return n.toFixed(1) + ' ' + u[i];
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
  if (!sec && sec !== 0) return '—';
  const h = Math.floor(sec / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = Math.floor(sec % 60);
  if (h > 0) return h + 'h' + m + 'm';
  return m + 'm' + s + 's';
}
