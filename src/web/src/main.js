// main.js — SPA 入口:登录态三态路由 + 主界面导航 + 主题切换。v3 Nocturne
import htmx from 'htmx.org';
import './styles/app.css';
import { api, humanSize, fmtTime, fmtUptime } from './api.js';
import { toast, confirmDialog, promptDialog, setBtnLoading } from './ui.js';
import { renderFilesPage } from './pages/files.js';
import { renderMonitorPage } from './pages/monitor.js';

// htmx.org 为 CJS 包,Vite 导入不挂全局;partials 内联脚本与 htmx.ajax 依赖全局对象
window.htmx = htmx;

// 暴露公共原语给 partials 内联脚本(htmx 交换执行)
window.toast = toast;
window.confirmDialog = confirmDialog;
window.promptDialog = promptDialog;
window.setBtnLoading = setBtnLoading;
window.humanSize = humanSize;
window.fmtTime = fmtTime;
window.fmtUptime = fmtUptime;

const authView = document.getElementById('auth-view');
const appView = document.getElementById('app-view');
const authForm = document.getElementById('auth-form');
const authSub = document.getElementById('auth-sub');
const authErr = document.getElementById('auth-err');
const authSwitch = document.getElementById('auth-switch');
const authBtn = document.getElementById('auth-btn');
const pw2Wrap = document.getElementById('auth-pw2-wrap');
const contentEl = document.getElementById('content');

// ---------- 主题切换(双主题:深色/浅色/跟随系统) ----------
const THEME_META = { dark: '#05060a', light: '#f8fafc' };
const themeEls = {
  icon: document.getElementById('theme-icon'),
  iconM: document.getElementById('theme-icon-m'),
  label: document.getElementById('theme-label')
};

function effectiveTheme(t) {
  if (t === 'dark' || t === 'light') return t;
  return window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
}

function applyTheme(t) {
  const eff = effectiveTheme(t);
  document.documentElement.setAttribute('data-theme', eff);
  const moon = eff === 'dark';
  const name = eff === 'dark' ? '深色' : '浅色';
  if (themeEls.icon) themeEls.icon.setAttribute('href', moon ? '#i-moon' : '#i-sun');
  if (themeEls.iconM) themeEls.iconM.setAttribute('href', moon ? '#i-moon' : '#i-sun');
  if (themeEls.label) themeEls.label.textContent = name;
  document.querySelectorAll('meta[name="theme-color"]').forEach((m) => {
    m.content = THEME_META[eff];
  });
  // 同步 aria-pressed
  const btn = document.getElementById('btn-theme');
  const btnM = document.getElementById('btn-theme-m');
  if (btn) btn.setAttribute('aria-pressed', String(moon));
  if (btnM) { btnM.setAttribute('aria-pressed', String(moon)); btnM.setAttribute('aria-label', moon ? '切换到浅色' : '切换到深色'); }
}

// 供设置页调用:window.setTheme('system'|'dark'|'light')
window.setTheme = function (t) {
  try { localStorage.setItem('nas-theme', t); } catch (e) { /* 忽略 */ }
  applyTheme(t);
  document.querySelectorAll('.seg [data-theme-opt]').forEach((b) => {
    const on = b.dataset.themeOpt === t;
    b.classList.toggle('active', on);
    b.setAttribute('aria-checked', String(on));
  });
};

// 设置页读取当前存储值
window.getTheme = function () {
  try { return localStorage.getItem('nas-theme') || 'system'; } catch (e) { return 'system'; }
};

// 跟随系统时监听系统偏好变化
const media = window.matchMedia('(prefers-color-scheme: light)');
media.addEventListener('change', () => {
  if ((window.getTheme() || 'system') === 'system') applyTheme('system');
});

// 顶栏快速切换: 深色 ↔ 浅色（保留 system 时切为显式）
function toggleTheme() {
  const cur = window.getTheme();
  const next = effectiveTheme(cur) === 'dark' ? 'light' : 'dark';
  window.setTheme(next);
}
const btnTheme = document.getElementById('btn-theme');
const btnThemeM = document.getElementById('btn-theme-m');
if (btnTheme) btnTheme.addEventListener('click', toggleTheme);
if (btnThemeM) btnThemeM.addEventListener('click', toggleTheme);

// ---------- 认证视图 ----------
let authMode = 'login';

function showAuth(mode) {
  authMode = mode;
  authView.hidden = false;
  appView.hidden = true;
  if (mode === 'setup') {
    authSub.textContent = '首次使用,请创建管理员账号';
    authBtn.textContent = '创建并进入';
    if (authSwitch) authSwitch.hidden = true;
    pw2Wrap.hidden = false;
    document.getElementById('auth-pass').autocomplete = 'new-password';
  } else {
    authSub.textContent = '登录以继续';
    authBtn.textContent = '登录';
    if (authSwitch) authSwitch.hidden = false;
    pw2Wrap.hidden = true;
    document.getElementById('auth-pass').autocomplete = 'current-password';
  }
  authErr.textContent = '';
  authErr.removeAttribute('aria-invalid');
  // 清除输入错误态
  document.querySelectorAll('#auth-form input').forEach(el => el.removeAttribute('aria-invalid'));
  // 聚焦用户名
  setTimeout(() => {
    const u = document.getElementById('auth-user');
    if (u) u.focus();
  }, 30);
}

function setFieldError(id, msg) {
  const el = document.getElementById(id);
  if (el) {
    el.setAttribute('aria-invalid', 'true');
    el.focus();
  }
  authErr.textContent = msg;
}

authForm.addEventListener('submit', async (e) => {
  e.preventDefault();
  const usernameEl = document.getElementById('auth-user');
  const passwordEl = document.getElementById('auth-pass');
  const username = usernameEl.value.trim();
  const password = passwordEl.value;
  // 清错误
  [usernameEl, passwordEl].forEach(el => el.removeAttribute('aria-invalid'));
  authErr.textContent = '';
  if (!username) { setFieldError('auth-user', '请填写用户名'); return; }
  if (!password) { setFieldError('auth-pass', '请填写密码'); return; }
  if (authMode === 'setup') {
    const pw2 = document.getElementById('auth-pass2').value;
    if (password !== pw2) {
      setFieldError('auth-pass2', '两次输入的密码不一致');
      return;
    }
    if (password.length < 8) {
      setFieldError('auth-pass', '密码至少 8 位');
      return;
    }
  }
  authBtn.disabled = true;
  authBtn.setAttribute('aria-busy', 'true');
  const old = authBtn.textContent;
  authBtn.textContent = '…';
  try {
    await api(authMode === 'setup' ? '/api/auth/setup' : '/api/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password })
    });
    toast(authMode === 'setup' ? '创建成功' : '登录成功', 'ok');
    location.href = '/';
  } catch (err) {
    authErr.textContent = err.message;
    // 读屏已通过 aria-live 播报
  } finally {
    authBtn.disabled = false;
    authBtn.removeAttribute('aria-busy');
    authBtn.textContent = old;
  }
});

if (authSwitch) {
  authSwitch.addEventListener('click', (e) => {
    e.preventDefault();
    showAuth('setup');
  });
}

// ---------- 主界面导航 ----------
const jsPages = { files: renderFilesPage, monitor: renderMonitorPage };
const partialPages = ['plugins', 'market', 'backup', 'settings'];
let currentPage = 'files';
let navAbort = null;

function setNavActive(name) {
  document.querySelectorAll('[data-nav]').forEach((b) => {
    const on = b.dataset.nav === name;
    b.classList.toggle('active', on);
    if (on) b.setAttribute('aria-current', 'page'); else b.removeAttribute('aria-current');
  });
}

window.navTo = function (name) {
  if (!name) return;
  // 取消前序 htmx 请求（若支持）
  if (navAbort) { try { navAbort.abort(); } catch (e) {} navAbort = null; }
  currentPage = name;
  setNavActive(name);
  closeSheet();
  // 推历史（不触发 popstate）
  try {
    const url = name === 'files' ? '/' : '/' + name;
    if (location.pathname !== url) history.pushState({ page: name }, '', url);
  } catch (e) {}
  // 标记忙
  if (contentEl) {
    contentEl.setAttribute('aria-busy', 'true');
    contentEl.setAttribute('aria-live', 'polite');
  }
  if (jsPages[name]) {
    try { jsPages[name](); } finally { if (contentEl) { contentEl.removeAttribute('aria-busy'); contentEl.focus(); } }
  } else if (partialPages.includes(name)) {
    // 使用 AbortController 包装 htmx 请求超时
    navAbort = new AbortController();
    const sig = navAbort.signal;
    // htmx.ajax 无 signal，改用 fetch + 交换
    (async () => {
      try {
        const r = await fetch('/partials/' + name + '.html', { credentials: 'same-origin', signal: sig });
        if (!r.ok) throw new Error('加载失败(' + r.status + ')');
        const html = await r.text();
        if (contentEl) {
          contentEl.innerHTML = html;
          // 执行内联脚本（htmx.ajax 原会执行，这里手动）
          contentEl.querySelectorAll('script').forEach(old => {
            const s = document.createElement('script');
            if (old.src) s.src = old.src; else s.textContent = old.textContent;
            old.replaceWith(s);
          });
        }
      } catch (err) {
        if (err.name !== 'AbortError') {
          if (contentEl) contentEl.innerHTML = '<div class="empty-state">加载失败：' + String(err.message || err) + '</div>';
          toast(String(err.message || '加载失败'), 'err');
        }
      } finally {
        if (contentEl) { contentEl.removeAttribute('aria-busy'); contentEl.focus(); }
        // 仅当仍是本次导航的 controller 才清空，避免旧请求的 finally 覆盖新导航
        if (navAbort && navAbort.signal === sig) navAbort = null;
      }
    })();
  }
};

document.querySelectorAll('[data-nav]').forEach((b) => {
  b.addEventListener('click', () => navTo(b.dataset.nav));
  b.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); navTo(b.dataset.nav); }
  });
});

// 历史回退
window.addEventListener('popstate', (e) => {
  const p = (e.state && e.state.page) || location.pathname.replace(/^\//, '') || 'files';
  const name = (jsPages[p] || partialPages.includes(p)) ? p : 'files';
  // 不再 pushState
  currentPage = name;
  setNavActive(name);
  closeSheet();
  if (jsPages[name]) jsPages[name](); else if (partialPages.includes(name)) window.navTo(name);
});

// “更多”抽屉(移动端)
const sheetMask = document.getElementById('sheet-mask');
const btnMore = document.getElementById('btn-more');
function openSheet() {
  if (!sheetMask) return;
  sheetMask.hidden = false;
  requestAnimationFrame(() => sheetMask.classList.add('open'));
  document.documentElement.classList.add('sheet-open');
  if (btnMore) { btnMore.setAttribute('aria-expanded', 'true'); }
  // 聚焦首个 sheet 项
  const first = sheetMask.querySelector('.sheet-item');
  if (first) first.focus();
}
function closeSheet() {
  if (!sheetMask) return;
  sheetMask.classList.remove('open');
  document.documentElement.classList.remove('sheet-open');
  if (btnMore) btnMore.setAttribute('aria-expanded', 'false');
  // 延后隐藏以保留动画
  setTimeout(() => { if (!sheetMask.classList.contains('open')) sheetMask.hidden = true; }, 220);
}
if (btnMore) {
  btnMore.addEventListener('click', () => {
    if (sheetMask.hidden || !sheetMask.classList.contains('open')) openSheet(); else closeSheet();
  });
}
if (sheetMask) {
  sheetMask.addEventListener('click', (e) => {
    if (e.target === sheetMask) closeSheet();
  });
  // Esc 关闭 + 焦点陷阱
  sheetMask.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') { e.preventDefault(); closeSheet(); if (btnMore) btnMore.focus(); }
    if (e.key === 'Tab') {
      const focusable = sheetMask.querySelectorAll('.sheet-item');
      if (focusable.length === 0) return;
      const first = focusable[0]; const last = focusable[focusable.length - 1];
      if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
    }
  });
  // 下滑手势关闭
  let startY = 0;
  const sheet = sheetMask.querySelector('.sheet');
  if (sheet) {
    sheet.addEventListener('touchstart', (e) => { startY = e.touches[0].clientY; }, { passive: true });
    sheet.addEventListener('touchmove', (e) => {
      const dy = e.touches[0].clientY - startY;
      if (dy > 60) { closeSheet(); }
    }, { passive: true });
  }
}
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape' && sheetMask && !sheetMask.hidden && sheetMask.classList.contains('open')) {
    closeSheet();
  }
});

// 退出登录(桌面侧边栏 + 移动顶栏)
async function doLogout() {
  const ok = await confirmDialog('退出登录', '确定要退出登录吗？');
  if (!ok) return;
  try {
    await fetch('/api/auth/logout', { method: 'POST', credentials: 'same-origin' });
  } catch (e) { /* 忽略 */ }
  location.href = '/login';
}
const btnLogout = document.getElementById('btn-logout');
const btnLogoutM = document.getElementById('btn-logout-m');
if (btnLogout) btnLogout.addEventListener('click', doLogout);
if (btnLogoutM) btnLogoutM.addEventListener('click', doLogout);

// ---------- 会话失效统一处理(HTMX 请求) ----------
document.body.addEventListener('htmx:responseError', function (evt) {
  if (evt.detail.xhr.status === 401) { location.href = '/login'; }
});
window.addEventListener('nas:unauthorized', () => {
  toast('会话已过期，请重新登录', 'warn');
});

// ---------- 启动 ----------
(async function boot() {
  applyTheme(window.getTheme());
  let st;
  try {
    st = await api('/api/auth/status');
  } catch (e) {
    // 后端不可达(dev 下可能未启动 nasd)
    if (authSub) authSub.textContent = '无法连接后端服务,请确认 nasd 已启动';
    showAuth('login');
    const btn = authForm ? authForm.querySelector('button[type="submit"]') : null;
    if (btn) btn.disabled = true;
    return;
  }
  if (st.authed) {
    authView.hidden = true;
    appView.hidden = false;
    const unameEl = document.getElementById('uname');
    if (unameEl) unameEl.textContent = st.username;
    const avatar = document.getElementById('uname-avatar');
    if (avatar) avatar.textContent = (st.username || 'U').slice(0, 1).toUpperCase();
    // 根据当前路径决定初始页
    const path = location.pathname.replace(/^\//, '').split('/')[0] || 'files';
    const initial = (jsPages[path] || partialPages.includes(path)) ? path : 'files';
    // 替换当前历史为初始页，避免重复 push
    try { history.replaceState({ page: initial }, '', path ? '/' + path : '/'); } catch (e) {}
    navTo(initial);
  } else {
    showAuth(st.initialized ? 'login' : 'setup');
  }
})();

// ---------- PWA(生产环境注册;dev 5173 跳过,避免离线壳缓存旧资源) ----------
if ('serviceWorker' in navigator && location.port !== '5173') {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js').catch(() => { /* 静默 */ });
  });
}
