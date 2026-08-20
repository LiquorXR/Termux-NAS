// main.js — SPA 入口:登录态三态路由 + 主界面导航 + 主题切换。
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

// ---------- 主题切换(双主题:深色/浅色/跟随系统) ----------
const THEME_META = { dark: '#0c0e13', light: '#f4f6fa' };
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
  // 同步图标(当前主题状态)与标题栏 meta
  const moon = eff === 'dark';
  const name = eff === 'dark' ? '深色' : '浅色';
  if (themeEls.icon) themeEls.icon.setAttribute('href', moon ? '#i-moon' : '#i-sun');
  if (themeEls.iconM) themeEls.iconM.setAttribute('href', moon ? '#i-moon' : '#i-sun');
  if (themeEls.label) themeEls.label.textContent = name;
  document.querySelectorAll('meta[name="theme-color"]').forEach((m) => {
    m.content = THEME_META[eff];
  });
}

// 供设置页调用:window.setTheme('system'|'dark'|'light')
window.setTheme = function (t) {
  try { localStorage.setItem('nas-theme', t); } catch (e) { /* 忽略 */ }
  applyTheme(t);
  document.querySelectorAll('.seg [data-theme-opt]').forEach((b) => {
    b.classList.toggle('active', b.dataset.themeOpt === t);
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

// 顶栏快速切换:深色 ↔ 浅色
function toggleTheme() {
  const cur = window.getTheme();
  const next = effectiveTheme(cur) === 'dark' ? 'light' : 'dark';
  window.setTheme(next);
}
document.getElementById('btn-theme').addEventListener('click', toggleTheme);
document.getElementById('btn-theme-m').addEventListener('click', toggleTheme);

// ---------- 认证视图 ----------
let authMode = 'login';

function showAuth(mode) {
  authMode = mode;
  authView.hidden = false;
  appView.hidden = true;
  if (mode === 'setup') {
    authSub.textContent = '首次使用,请创建管理员账号';
    authBtn.textContent = '创建并进入';
    authSwitch.style.display = 'none';
    pw2Wrap.hidden = false;
    document.getElementById('auth-pass').autocomplete = 'new-password';
  } else {
    authSub.textContent = '登录以继续';
    authBtn.textContent = '登录';
    authSwitch.style.display = '';
    pw2Wrap.hidden = true;
    document.getElementById('auth-pass').autocomplete = 'current-password';
  }
  authErr.textContent = '';
}

authForm.addEventListener('submit', async (e) => {
  e.preventDefault();
  const username = document.getElementById('auth-user').value.trim();
  const password = document.getElementById('auth-pass').value;
  if (!username || !password) {
    authErr.textContent = '请填写用户名和密码';
    return;
  }
  if (authMode === 'setup') {
    const pw2 = document.getElementById('auth-pass2').value;
    if (password !== pw2) {
      authErr.textContent = '两次输入的密码不一致';
      return;
    }
    if (password.length < 8) {
      authErr.textContent = '密码至少 8 位';
      return;
    }
  }
  authBtn.disabled = true;
  authBtn.textContent = '…';
  try {
    await api(authMode === 'setup' ? '/api/auth/setup' : '/api/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password })
    });
    location.href = '/';
  } catch (err) {
    authErr.textContent = err.message;
  } finally {
    authBtn.disabled = false;
    authBtn.textContent = authMode === 'setup' ? '创建并进入' : '登录';
  }
});

authSwitch.addEventListener('click', (e) => {
  e.preventDefault();
  showAuth('setup');
});

// ---------- 主界面导航 ----------
const jsPages = { files: renderFilesPage, monitor: renderMonitorPage };
const partialPages = ['plugins', 'market', 'backup', 'settings'];

window.navTo = function (name) {
  document.querySelectorAll('[data-nav]').forEach((b) => {
    b.classList.toggle('active', b.dataset.nav === name);
  });
  closeSheet();
  if (jsPages[name]) {
    jsPages[name]();
  } else if (partialPages.includes(name)) {
    htmx.ajax('GET', '/partials/' + name + '.html', { target: '#content' });
  }
};

document.querySelectorAll('[data-nav]').forEach((b) => {
  b.addEventListener('click', () => navTo(b.dataset.nav));
});

// “更多”抽屉(移动端)
const sheetMask = document.getElementById('sheet-mask');
document.getElementById('btn-more').addEventListener('click', () => {
  sheetMask.classList.toggle('open');
});
function closeSheet() {
  sheetMask.classList.remove('open');
}
sheetMask.addEventListener('click', (e) => {
  if (e.target === sheetMask) closeSheet();
});

// 退出登录(桌面侧边栏 + 移动顶栏)
async function doLogout() {
  try {
    await fetch('/api/auth/logout', { method: 'POST' });
  } catch (e) { /* 忽略 */ }
  location.href = '/login';
}
document.getElementById('btn-logout').addEventListener('click', doLogout);
document.getElementById('btn-logout-m').addEventListener('click', doLogout);

// ---------- 会话失效统一处理(HTMX 请求) ----------
document.body.addEventListener('htmx:responseError', function (evt) {
  if (evt.detail.xhr.status === 401) { location.href = '/login'; }
});

// ---------- 启动 ----------
(async function boot() {
  applyTheme(window.getTheme());
  let st;
  try {
    st = await api('/api/auth/status');
  } catch (e) {
    // 后端不可达(dev 下可能未启动 nasd)
    document.getElementById('auth-sub').textContent = '无法连接后端服务,请确认 nasd 已启动';
    showAuth('login');
    authForm.querySelector('button').disabled = true;
    return;
  }
  if (st.authed) {
    authView.hidden = true;
    appView.hidden = false;
    document.getElementById('uname').textContent = st.username;
    const avatar = document.getElementById('uname-avatar');
    if (avatar) avatar.textContent = (st.username || 'U').slice(0, 1).toUpperCase();
    navTo('files');
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