// pages/files.js — 文件管理页(JS 渲染) · v3 Nocturne · 44px触摸 · 键盘可达 · 拖拽上传 · 搜索防抖+Abort
import { api, humanSize, fmtTime } from '../api.js';
import { toast, confirmDialog, promptDialog, setBtnLoading } from '../ui.js';

let currentPath = '/';
let searchMode = false;
let listAbort = null;
let searchAbort = null;
let searchTimer = null;
let loadId = 0;
let progressTimer = null;

const ICONS = {
  dir: 'i-folder', img: 'i-image', video: 'i-video', audio: 'i-audio',
  arch: 'i-archive', doc: 'i-doc', file: 'i-file'
};

function iconFor(e) {
  if (e.is_dir) return ICONS.dir;
  const m = (e.name || '').toLowerCase().match(/\.([a-z0-9]+)$/);
  if (!m) return ICONS.file;
  switch (m[1]) {
    case 'jpg': case 'jpeg': case 'png': case 'gif': case 'webp': case 'bmp': case 'svg':
      return ICONS.img;
    case 'mp4': case 'mkv': case 'avi': case 'mov': case 'webm': case 'flv':
      return ICONS.video;
    case 'mp3': case 'flac': case 'wav': case 'aac': case 'ogg': case 'm4a':
      return ICONS.audio;
    case 'zip': case 'tar': case 'gz': case 'tgz': case 'bz2': case 'xz': case '7z': case 'rar':
      return ICONS.arch;
    case 'txt': case 'md': case 'pdf': case 'doc': case 'docx': case 'xls': case 'xlsx':
      return ICONS.doc;
    default:
      return ICONS.file;
  }
}

function icon(id) {
  return '<svg class="fic" aria-hidden="true"><use href="#' + id + '"/></svg>';
}

function esc(s) {
  return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

function isMobile() {
  return window.matchMedia('(max-width: 640px)').matches;
}

function crumbsHTML(path) {
  const parts = path === '/' ? [] : path.replace(/^\//, '').split('/');
  let html = '<nav aria-label="面包屑"><button type="button" class="fcrumb' + (path === '/' ? ' active" aria-current="page"' : '"') + ' data-crumb="/">根目录</button>';
  let cur = '';
  parts.forEach((p, i) => {
    cur += '/' + p;
    const last = i === parts.length - 1;
    html += '<span class="csep" aria-hidden="true">/</span>';
    if (last) {
      html += '<span class="fcrumb active" aria-current="page">' + esc(p) + '</span>';
    } else {
      html += '<button type="button" class="fcrumb" data-crumb="' + esc(cur) + '">' + esc(p) + '</button>';
    }
  });
  html += '</nav>';
  return html;
}

// ---------- 桌面:表格行 ----------
function tableRows(list) {
  return list.map((e) => {
    const p = esc(e.path);
    const nm = esc(e.name);
    let ops = '';
    if (!e.is_dir) {
      ops += '<a class="btn sm" href="/api/files/download?path=' + encodeURIComponent(e.path) + '" aria-label="下载 ' + nm + '">' + icon('i-download') + '下载</a> ';
    }
    ops += '<button type="button" class="btn sm" data-act="rename" data-path="' + p + '" data-name="' + nm + '" aria-label="重命名 ' + nm + '">' + icon('i-edit') + '重命名</button> ';
    ops += '<button type="button" class="btn sm" data-act="share" data-path="' + p + '" aria-label="分享 ' + nm + '">' + icon('i-share') + '分享</button> ';
    ops += '<button type="button" class="btn sm danger" data-act="del" data-path="' + p + '" data-name="' + nm + '" aria-label="删除 ' + nm + '">' + icon('i-trash') + '删除</button>';
    const nameCell = e.is_dir
      ? '<button type="button" class="link" data-act="open" data-path="' + p + '" aria-label="打开文件夹 ' + nm + '"><span class="fico">' + icon(iconFor(e)) + '</span>' + nm + '</button>'
      : '<span class="fico">' + icon(iconFor(e)) + '</span>' + nm;
    return '<tr>' +
      '<td>' + nameCell + '</td>' +
      '<td class="col-optional num">' + (e.is_dir ? '—' : humanSize(e.size)) + '</td>' +
      '<td class="col-optional num">' + fmtTime(e.mod_time) + '</td>' +
      '<td style="white-space:nowrap"><div style="display:flex;gap:6px;flex-wrap:wrap">' + ops + '</div></td>' +
      '</tr>';
  }).join('');
}

// ---------- 移动:卡片列表 ----------
function cardRows(list) {
  return list.map((e) => {
    const p = esc(e.path);
    const nm = esc(e.name);
    const meta = e.is_dir ? '文件夹' : (humanSize(e.size) + ' · ' + fmtTime(e.mod_time));
    const isDir = !!e.is_dir;
    const main = isDir
      ? '<button type="button" class="fcard-main" data-act="open" data-path="' + p + '" aria-label="打开 ' + nm + '">'
      : '<a class="fcard-main" href="/api/files/download?path=' + encodeURIComponent(e.path) + '" aria-label="下载 ' + nm + '">';
    const mainClose = isDir ? '</button>' : '</a>';
    return '<div class="fcard" data-dir="' + (isDir ? 1 : 0) + '">' +
      '<div class="fcard-icon" aria-hidden="true">' + icon(iconFor(e)) + '</div>' +
      main +
        '<div class="fcard-name">' + nm + '</div>' +
        '<div class="fcard-meta">' + meta + '</div>' +
      mainClose +
      '<div class="fcard-ops">' +
        '<button type="button" class="icon-btn" aria-label="分享 ' + nm + '" data-act="share" data-path="' + p + '">' + icon('i-share') + '</button>' +
        '<button type="button" class="icon-btn" aria-label="重命名 ' + nm + '" data-act="rename" data-path="' + p + '" data-name="' + nm + '">' + icon('i-edit') + '</button>' +
        '<button type="button" class="icon-btn danger" aria-label="删除 ' + nm + '" data-act="del" data-path="' + p + '" data-name="' + nm + '">' + icon('i-trash') + '</button>' +
      '</div>' +
      '</div>';
  }).join('');
}

function render(list) {
  const rows = document.getElementById('f-rows');
  const count = document.getElementById('f-count');
  if (!rows || !count) return;
  count.textContent = searchMode
    ? '搜索结果: ' + list.length + ' 条'
    : '共 ' + list.length + ' 项';
  const mobile = isMobile();
  if (!list.length) {
    const empty = '<div class="empty-state">' + (searchMode ? '无匹配结果' : '目录为空') + '</div>';
    if (mobile) {
      rows.className = 'fcardlist';
      rows.innerHTML = empty;
    } else {
      rows.className = '';
      rows.innerHTML = '<tr><td colspan="4" class="fempty">' + (searchMode ? '无匹配结果' : '目录为空') + '</td></tr>';
    }
    return;
  }
  if (mobile) {
    rows.className = 'fcardlist';
    rows.innerHTML = cardRows(list);
  } else {
    rows.className = '';
    rows.innerHTML = tableRows(list);
  }
}

function renderSearch(query, list) {
  const crumbBox = document.getElementById('f-crumbs');
  if (crumbBox) crumbBox.innerHTML = '<button type="button" class="fcrumb-clear" data-clear-search="1">' + icon('i-back') + ' 返回文件列表</button> <span style="color:var(--muted);font-size:13px">搜索 “' + esc(query) + '”</span>';
  const qInput = document.getElementById('f-q');
  if (qInput) qInput.value = query;
  render(list);
}

function setLoading(isLoading) {
  const rows = document.getElementById('f-rows');
  const count = document.getElementById('f-count');
  if (!rows) return;
  if (isLoading) {
    rows.setAttribute('aria-busy', 'true');
    if (count) count.textContent = '加载中…';
    const skel = isMobile()
      ? '<div class="fcard"><div class="fcard-icon skel" style="width:44px;height:44px"></div><div class="fcard-main"><div class="skel" style="height:14px;width:60%"></div><div class="skel" style="height:12px;width:40%;margin-top:6px"></div></div></div>'.repeat(3)
      : '<tr><td colspan="4"><div class="skel" style="height:44px"></div></td></tr>';
    rows.innerHTML = isMobile() ? skel : skel;
    rows.className = isMobile() ? 'fcardlist' : '';
  } else {
    rows.removeAttribute('aria-busy');
  }
}

async function loadList(path) {
  // 取消搜索请求与防抖定时器（列表/搜索 Abort 分离，互不覆盖）
  searchMode = false;
  if (searchAbort) { try { searchAbort.abort(); } catch (e) {} searchAbort = null; }
  if (searchTimer) { clearTimeout(searchTimer); searchTimer = null; }
  const id = ++loadId;
  currentPath = path;
  if (listAbort) { try { listAbort.abort(); } catch (e) {} }
  listAbort = new AbortController();
  setLoading(true);
  try {
    const data = await api('/api/files/list?path=' + encodeURIComponent(path), { signal: listAbort.signal });
    if (id !== loadId) return; // 已有更新请求，丢弃过期响应
    const crumbBox = document.getElementById('f-crumbs');
    if (crumbBox) crumbBox.innerHTML = crumbsHTML(data.path || '/');
    render(data.entries || []);
  } catch (e) {
    if (e.name === 'AbortError' || String(e.message).includes('超时')) return;
    toast(e.message, 'err');
    if (id !== loadId) return;
    const rows = document.getElementById('f-rows');
    if (rows) rows.innerHTML = isMobile() ? '<div class="empty-state">加载失败：' + esc(e.message) + '</div>' : '<tr><td colspan="4" class="fempty">加载失败：' + esc(e.message) + '</td></tr>';
  } finally {
    if (id === loadId) setLoading(false);
  }
}

async function doSearch(q) {
  if (searchAbort) { try { searchAbort.abort(); } catch (e) {} }
  searchAbort = new AbortController();
  searchMode = true;
  const id = ++loadId;
  setLoading(true);
  try {
    const data = await api('/api/files/search?q=' + encodeURIComponent(q), { signal: searchAbort.signal });
    if (id !== loadId) return; // 已有更新请求，丢弃过期响应
    renderSearch(q, data.results || []);
  } catch (e) {
    if (e.name === 'AbortError' || String(e.message).includes('超时')) return;
    toast(e.message, 'err');
  } finally {
    if (id === loadId) setLoading(false);
  }
}

// ---------- 操作 ----------
async function actMkdir() {
  const name = await promptDialog('新建文件夹', '在 ' + currentPath + ' 下创建', '', '文件夹名称');
  if (!name) return;
  if (/[\/\\]/.test(name)) { toast('名称不能包含 / 或 \\', 'err'); return; }
  try {
    await api('/api/files/mkdir', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: currentPath, name })
    });
    toast('已创建 ' + name, 'ok');
    loadList(currentPath);
  } catch (e) { toast(e.message, 'err'); }
}

async function actRename(path) {
  const cur = path.split('/').pop();
  const name = await promptDialog('重命名', path, cur, '新名称');
  if (!name || name === cur) return;
  if (/[\/\\]/.test(name)) { toast('名称不能包含 / 或 \\', 'err'); return; }
  try {
    await api('/api/files/rename', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path, new_name: name })
    });
    toast('已重命名', 'ok');
    loadList(currentPath);
  } catch (e) { toast(e.message, 'err'); }
}

async function actDelete(path, name) {
  const ok = await confirmDialog('删除', '确定删除 ' + name + ' ？此操作不可恢复。');
  if (!ok) return;
  try {
    await api('/api/files/delete', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path })
    });
    toast('已删除', 'ok');
    loadList(currentPath);
  } catch (e) { toast(e.message, 'err'); }
}

async function actShare(path) {
  try {
    const data = await api('/api/files/share', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path })
    });
    const full = location.origin + data.url;
    let copied = false;
    if (navigator.clipboard && navigator.clipboard.writeText) {
      try { await navigator.clipboard.writeText(full); copied = true; } catch (e) {}
    }
    if (copied) toast('分享链接已复制', 'ok');
    else {
      // 降级：弹窗显示
      await promptDialog('分享链接', '链接已生成（24 小时有效）：', full, full);
      try { await navigator.clipboard.writeText(full); } catch (e) {}
    }
  } catch (e) { toast(e.message, 'err'); }
}

async function uploadFiles(files) {
  if (!files || !files.length) return;
  const list = [...files];
  // 简单大小校验（单文件 256MB 后端限制，前端先提示）
  const tooBig = list.find(f => f.size > 256 * 1024 * 1024);
  if (tooBig) { toast('文件过大: ' + tooBig.name + ' 超过 256 MB', 'err'); return; }
  const fd = new FormData();
  fd.append('path', currentPath);
  list.forEach((f) => fd.append('files', f));
  const btn = document.getElementById('f-upload-label');
  const progress = document.getElementById('f-progress');
  const progressBar = document.getElementById('f-progress-bar');
  if (progress) { progress.hidden = false; progress.setAttribute('aria-hidden', 'false'); }
  if (progressBar) progressBar.style.width = '12%';
  setBtnLoading(btn, true);
  // 使用 XHR 以支持进度
  const xhrUpload = () => new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open('POST', '/api/files/upload', true);
    xhr.withCredentials = true;
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && progressBar) {
        const pct = Math.round((e.loaded / e.total) * 100);
        progressBar.style.width = pct + '%';
        progressBar.setAttribute('aria-valuenow', String(pct));
      }
    };
    xhr.onload = () => {
      const ct = xhr.getResponseHeader('content-type') || '';
      let data = null;
      try { data = JSON.parse(xhr.responseText); } catch (e) {}
      if (xhr.status === 401) { location.href = '/login'; reject(new Error('未登录')); return; }
      if (xhr.status < 200 || xhr.status >= 300) {
        reject(new Error((data && data.error) || ('上传失败(' + xhr.status + ')')));
        return;
      }
      resolve(data);
    };
    xhr.onerror = () => reject(new Error('网络错误'));
    xhr.onabort = () => reject(new Error('已取消'));
    xhr.send(fd);
  });
  try {
    const data = await xhrUpload();
    toast('已上传 ' + (data.uploaded || list.length) + ' 个文件', 'ok');
    loadList(currentPath);
  } catch (e) {
    const msg = String(e.message);
    if (msg !== '已取消' && msg !== '未登录') toast(msg, 'err');
  } finally {
    setBtnLoading(btn, false);
    if (progress) {
      // 连续上传时清除旧定时器，避免新进度条被上一次的隐藏逻辑关掉
      clearTimeout(progressTimer);
      progressTimer = setTimeout(() => { progress.hidden = true; if (progressBar) progressBar.style.width = '0%'; }, 600);
    }
  }
}

// ---------- 页面挂载 ----------
export function renderFilesPage() {
  const content = document.getElementById('content');
  content.innerHTML =
    '<div class="card" id="files-page" role="region" aria-label="文件管理">' +
      '<div class="fbar">' +
        '<h2>文件</h2>' +
        '<div class="fcount" id="f-count" aria-live="polite">—</div>' +
        '<label class="fsearch" for="f-q"><span class="sr-only">搜索文件名</span>' + icon('i-search') + '<input id="f-q" type="search" placeholder="搜索文件名…" autocomplete="off" aria-label="搜索文件名" enterkeyhint="search"></label>' +
      '</div>' +
      '<div class="fcrumbs" id="f-crumbs" role="navigation" aria-label="面包屑"><span class="fcrumb active" aria-current="page">根目录</span></div>' +
      '<div class="ftools">' +
        '<button type="button" class="btn sm primary" id="f-mkdir" aria-label="新建文件夹">' + icon('i-folder-plus') + '新建文件夹</button>' +
        '<label class="btn sm" id="f-upload-label" tabindex="0" role="button" aria-label="上传文件">' + icon('i-upload') + '上传' +
          '<input type="file" id="f-file" multiple hidden></label>' +
        '<button type="button" class="btn sm ghost" id="f-sort" aria-label="排序">排序 · 名称</button>' +
      '</div>' +
      '<div id="f-dropzone" class="dropzone" hidden>释放以添加至 <span class="mono" id="f-drop-path">/</span></div>' +
      '<div id="f-progress" class="progress" hidden aria-hidden="true" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow="0"><i id="f-progress-bar" style="width:0%"></i></div>' +
      '<div class="ftable-wrap" style="margin-top:12px"><table class="ftable" aria-label="文件列表">' +
        '<thead><tr><th>名称</th><th class="w120 col-optional">大小</th>' +
        '<th class="w160 col-optional">修改时间</th><th class="w220">操作</th></tr></thead>' +
        '<tbody id="f-rows" aria-live="polite"><tr><td colspan="4" class="fempty">加载中…</td></tr></tbody>' +
      '</table></div>' +
    '</div>';

  document.getElementById('f-rows').innerHTML = isMobile()
    ? '<div class="fempty">加载中…</div>'
    : '<tr><td colspan="4" class="fempty">加载中…</td></tr>';
  loadList(currentPath);

  // 搜索：防抖 + Abort
  const qInput = document.getElementById('f-q');
  qInput.addEventListener('input', (e) => {
    const q = e.target.value.trim();
    if (searchTimer) clearTimeout(searchTimer);
    if (searchAbort) { try { searchAbort.abort(); } catch (ex) {} }
    if (!q) { loadList(currentPath); return; }
    searchTimer = setTimeout(() => doSearch(q), 380);
  });
  qInput.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') { e.target.value = ''; loadList(currentPath); }
  });

  document.getElementById('f-mkdir').addEventListener('click', actMkdir);
  const fileInput = document.getElementById('f-file');
  const uploadLabel = document.getElementById('f-upload-label');
  // label 键盘可达
  uploadLabel.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); fileInput.click(); }
  });
  uploadLabel.addEventListener('click', (e) => {
    // 让 label 点击触发 file picker（原生已支持），但阻止冒泡到 f-rows
    e.stopPropagation();
  });
  fileInput.addEventListener('change', (e) => {
    uploadFiles(e.target.files);
    e.target.value = '';
  });

  // 面包屑
  document.getElementById('f-crumbs').addEventListener('click', (e) => {
    const el = e.target.closest('[data-crumb],[data-clear-search]');
    if (!el) return;
    if (el.dataset.clearSearch !== undefined) { loadList(currentPath); return; }
    loadList(el.dataset.crumb);
  });
  document.getElementById('f-crumbs').addEventListener('keydown', (e) => {
    const el = e.target.closest('[data-crumb]');
    if (!el) return;
    if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); loadList(el.dataset.crumb); }
  });

  // 拖拽上传
  const page = document.getElementById('files-page');
  const dropzone = document.getElementById('f-dropzone');
  const dropPath = document.getElementById('f-drop-path');
  ['dragenter', 'dragover'].forEach(ev => {
    page.addEventListener(ev, (e) => {
      e.preventDefault();
      if (dropzone) { dropzone.hidden = false; dropPath.textContent = currentPath; page.classList.add('drag'); }
    });
  });
  ['dragleave', 'drop'].forEach(ev => {
    page.addEventListener(ev, (e) => {
      if (ev === 'drop') {
        e.preventDefault();
        const files = e.dataTransfer ? e.dataTransfer.files : null;
        if (files && files.length) uploadFiles(files);
      }
      if (dropzone) dropzone.hidden = true;
      page.classList.remove('drag');
    });
  });

  // 操作委托（click + 键盘）
  const rows = document.getElementById('f-rows');
  function handleAct(el) {
    if (!el) return;
    switch (el.dataset.act) {
      case 'open': loadList(el.dataset.path); break;
      case 'rename': actRename(el.dataset.path); break;
      case 'share': actShare(el.dataset.path); break;
      case 'del': actDelete(el.dataset.path, el.dataset.name); break;
    }
  }
  rows.addEventListener('click', (e) => {
    const el = e.target.closest('[data-act]');
    handleAct(el);
  });
  rows.addEventListener('keydown', (e) => {
    const el = e.target.closest('[data-act]');
    if (!el) return;
    if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); handleAct(el); }
  });
}
