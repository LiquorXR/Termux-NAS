// pages/files.js — 文件管理页(JS 渲染,替代原 Go 模板 partial)。
// 桌面:表格列表;移动(≤640px):卡片列表,均带 SVG 图标。
import { api, humanSize, fmtTime } from '../api.js';
import { toast, confirmDialog, promptDialog, setBtnLoading } from '../ui.js';

let currentPath = '/';
let searchMode = false;

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
  let html = '<a data-crumb="/">根目录</a>';
  let cur = '';
  parts.forEach((p, i) => {
    cur += '/' + p;
    const last = i === parts.length - 1;
    html += '<span class="csep">/</span>';
    html += last
      ? '<span class="ccur">' + esc(p) + '</span>'
      : '<a data-crumb="' + esc(cur) + '">' + esc(p) + '</a>';
  });
  return html;
}

// ---------- 桌面:表格行 ----------
function tableRows(list) {
  return list.map((e) => {
    const p = esc(e.path);
    const nm = esc(e.name);
    let ops = '';
    if (!e.is_dir) {
      ops += '<a class="btn sm" href="/api/files/download?path=' + encodeURIComponent(e.path) + '">' + icon('i-download') + '下载</a> ';
    }
    ops += '<button class="btn sm" data-act="rename" data-path="' + p + '" data-name="' + nm + '">' + icon('i-edit') + '重命名</button> ';
    ops += '<button class="btn sm" data-act="share" data-path="' + p + '">' + icon('i-share') + '分享</button> ';
    ops += '<button class="btn sm danger" data-act="del" data-path="' + p + '" data-name="' + nm + '">' + icon('i-trash') + '删除</button>';
    return '<tr>' +
      '<td>' + (e.is_dir
        ? '<a class="link" data-act="open" data-path="' + p + '"><span class="fico">' + icon(iconFor(e)) + '</span>' + nm + '</a>'
        : '<span class="fico">' + icon(iconFor(e)) + '</span>' + nm) + '</td>' +
      '<td class="col-optional">' + (e.is_dir ? '—' : humanSize(e.size)) + '</td>' +
      '<td class="col-optional">' + fmtTime(e.mod_time) + '</td>' +
      '<td style="white-space:nowrap">' + ops + '</td>' +
      '</tr>';
  }).join('');
}

// ---------- 移动:卡片列表 ----------
function cardRows(list) {
  return list.map((e) => {
    const p = esc(e.path);
    const nm = esc(e.name);
    const meta = e.is_dir ? '文件夹' : (humanSize(e.size) + ' · ' + fmtTime(e.mod_time));
    const body = e.is_dir
      ? '<div class="fcard-main" data-act="open" data-path="' + p + '">'
      : '<div class="fcard-main"><a class="fcard-main" href="/api/files/download?path=' + encodeURIComponent(e.path) + '">';
    const bodyClose = e.is_dir ? '</div>' : '</a>';
    return '<div class="fcard" data-dir="' + (e.is_dir ? 1 : 0) + '">' +
      '<div class="fcard-icon">' + icon(iconFor(e)) + '</div>' +
      body +
        '<div class="fcard-name">' + nm + '</div>' +
        '<div class="fcard-meta">' + meta + '</div>' +
      bodyClose +
      '<div class="fcard-ops">' +
        '<button type="button" class="icon-btn" title="分享" data-act="share" data-path="' + p + '">' + icon('i-share') + '</button>' +
        '<button type="button" class="icon-btn" title="重命名" data-act="rename" data-path="' + p + '" data-name="' + nm + '">' + icon('i-edit') + '</button>' +
        '<button type="button" class="icon-btn danger" title="删除" data-act="del" data-path="' + p + '" data-name="' + nm + '">' + icon('i-trash') + '</button>' +
      '</div>' +
      '</div>';
  }).join('');
}

function render(list) {
  const rows = document.getElementById('f-rows');
  const count = document.getElementById('f-count');
  count.textContent = searchMode
    ? '搜索结果: ' + list.length + ' 条'
    : '共 ' + list.length + ' 项';
  const mobile = isMobile();
  if (!list.length) {
    const empty = '<div class="fempty">' + (searchMode ? '无匹配结果' : '目录为空') + '</div>';
    rows.innerHTML = mobile
      ? empty
      : '<tr><td colspan="4" class="fempty">' + (searchMode ? '无匹配结果' : '目录为空') + '</td></tr>';
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
  crumbBox.innerHTML = '<a data-clear-search="1">' + icon('i-back') + ' 返回文件列表</a>';
  document.getElementById('f-q').value = query;
  render(list);
}

async function loadList(path) {
  currentPath = path;
  searchMode = false;
  try {
    const data = await api('/api/files/list?path=' + encodeURIComponent(path));
    document.getElementById('f-crumbs').innerHTML = crumbsHTML(data.path || '/');
    render(data.entries || []);
  } catch (e) {
    toast(e.message, 'err');
  }
}

async function doSearch(q) {
  searchMode = true;
  try {
    const data = await api('/api/files/search?q=' + encodeURIComponent(q));
    renderSearch(q, data.results || []);
  } catch (e) {
    toast(e.message, 'err');
  }
}

// ---------- 操作 ----------
async function actMkdir() {
  const name = await promptDialog('新建文件夹', '', '', '文件夹名称');
  if (!name) return;
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
  const ok = await confirmDialog('删除文件', '确定删除 ' + name + ' ?此操作不可恢复。');
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
    if (navigator.clipboard && navigator.clipboard.writeText) {
      await navigator.clipboard.writeText(full);
      toast('分享链接已复制', 'ok');
    } else {
      await navigator.clipboard.writeText(full).catch(() => {});
      toast(full, 'ok');
    }
  } catch (e) { toast(e.message, 'err'); }
}

async function uploadFiles(files) {
  if (!files || !files.length) return;
  const fd = new FormData();
  fd.append('path', currentPath);
  [...files].forEach((f) => fd.append('files', f));
  const btn = document.getElementById('f-upload-label');
  setBtnLoading(btn, true);
  try {
    const data = await api('/api/files/upload', { method: 'POST', body: fd });
    toast('已上传 ' + (data.uploaded || files.length) + ' 个文件', 'ok');
    loadList(currentPath);
  } catch (e) { toast(e.message, 'err'); }
  finally { setBtnLoading(btn, false); }
}

// ---------- 页面挂载 ----------
export function renderFilesPage() {
  const content = document.getElementById('content');
  content.innerHTML =
    '<div class="card" id="files-page">' +
      '<div class="fbar">' +
        '<h2>文件管理</h2>' +
        '<div class="fsearch"><input id="f-q" placeholder="搜索文件名..." autocomplete="off"></div>' +
      '</div>' +
      '<div class="fcrumbs" id="f-crumbs"><span class="ccur">根目录</span></div>' +
      '<div class="ftools">' +
        '<button class="btn sm" id="f-mkdir">' + icon('i-folder-plus') + '新建文件夹</button>' +
        '<label class="btn sm" id="f-upload-label">' + icon('i-upload') + '上传' +
          '<input type="file" id="f-file" multiple hidden></label>' +
        '<span class="fcount" id="f-count"></span>' +
      '</div>' +
      '<div class="ftable-wrap"><table class="ftable">' +
        '<thead><tr><th>名称</th><th class="w120 col-optional">大小</th>' +
        '<th class="w160 col-optional">修改时间</th><th class="w220">操作</th></tr></thead>' +
        '<tbody id="f-rows"><tr><td colspan="4" class="fempty">加载中…</td></tr></tbody>' +
      '</table></div>' +
    '</div>';

  document.getElementById('f-rows').innerHTML = isMobile()
    ? '<div class="fempty">加载中…</div>'
    : '<tr><td colspan="4" class="fempty">加载中…</td></tr>';
  loadList(currentPath);

  // 事件绑定
  document.getElementById('f-q').addEventListener('input', (e) => {
    clearTimeout(window.__fqTimer);
    const q = e.target.value.trim();
    if (!q) { loadList(currentPath); return; }
    window.__fqTimer = setTimeout(() => doSearch(q), 400);
  });
  document.getElementById('f-mkdir').addEventListener('click', actMkdir);
  document.getElementById('f-file').addEventListener('change', (e) => {
    uploadFiles(e.target.files);
    e.target.value = '';
  });
  document.getElementById('f-crumbs').addEventListener('click', (e) => {
    const el = e.target.closest('[data-crumb],[data-clear-search]');
    if (!el) return;
    if (el.dataset.clearSearch !== undefined) { loadList(currentPath); return; }
    loadList(el.dataset.crumb);
  });
  document.getElementById('f-rows').addEventListener('click', (e) => {
    const el = e.target.closest('[data-act]');
    if (!el) return;
    switch (el.dataset.act) {
      case 'open': loadList(el.dataset.path); break;
      case 'rename': actRename(el.dataset.path); break;
      case 'share': actShare(el.dataset.path); break;
      case 'del': actDelete(el.dataset.path, el.dataset.name); break;
    }
  });
}