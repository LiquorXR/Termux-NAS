// pages/monitor.js — 系统监控看板(首帧立即 + 3s 轮询 + visibility 暂停 + 无堆积) · v3
import { api, humanSize, fmtUptime } from '../api.js';
import { toast } from '../ui.js';

let timer = null;
let abortCtrl = null;
let hasToasted = false;

function levelColor(pct) {
  if (pct >= 90) return 'var(--danger)';
  if (pct >= 80) return 'var(--warn)';
  return 'var(--accent)';
}

function ringCell(label, pct, detail, ariaLabel) {
  const c = levelColor(pct);
  // 钳位后统一用于文本与 --pct，避免 NaN 进入 CSS 变量导致渐变失效
  const n = Math.max(0, Math.min(100, Number(pct) || 0));
  const v = n.toFixed(0);
  return '<div class="mcell" role="group" aria-label="' + (ariaLabel || label) + ' ' + v + '%">' +
    '<span>' + label + '</span>' +
    '<div class="ringrow">' +
      '<div class="ring" style="--pct:' + n.toFixed(1) + ';--c:' + c + '" role="progressbar" aria-valuenow="' + v + '" aria-valuemin="0" aria-valuemax="100" aria-label="' + label + '"><b>' + v + '%</b></div>' +
      '<div>' + (detail ? '<i>' + detail + '</i>' : '') + '</div>' +
    '</div>' +
  '</div>';
}

function render(s) {
  const grid = document.getElementById('m-grid');
  if (!grid) return;
  let html = '';
  // CPU + 详情
  const cpuDetail = (s.cpu_count ? s.cpu_count + ' 核' : '') + (s.load_avg ? ' · 负载 ' + s.load_avg : '');
  html += ringCell('CPU', s.cpu_percent, cpuDetail, 'CPU使用率');
  html += ringCell('内存', s.mem_percent,
    humanSize(s.mem_used_bytes) + ' / ' + humanSize(s.mem_total_bytes),
    '内存使用率');
  html += ringCell('磁盘', s.disk_percent,
    '可用 ' + humanSize(s.disk_free_bytes) + ' / ' + humanSize(s.disk_total_bytes),
    '磁盘使用率');
  html += '<div class="mcell"><span>运行时长</span><b class="val">' +
    fmtUptime(s.uptime_seconds) + '</b><i>daemon 已运行 · PID ' + (s.pid || '—') + '</i></div>';
  if (s.battery && typeof s.battery.percentage === 'number') {
    const pct = s.battery.percentage;
    html += '<div class="mcell" role="group" aria-label="电量 ' + pct + '%"><span>电量</span><div class="ringrow">' +
      '<div class="ring" style="--pct:' + pct + ';--c:var(--ok)" role="progressbar" aria-valuenow="' + pct + '" aria-valuemin="0" aria-valuemax="100" aria-label="电量"><b>' + pct + '%</b></div>' +
      '<div><i>' + (s.battery.status || '') + '</i>' +
      (s.battery.temperature > 0
        ? '<i> ' + s.battery.temperature.toFixed(1) + '℃</i>' : '') +
      '</div></div></div>';
  }
  if (s.net) {
    html += '<div class="mcell"><span>网络流量</span>' +
      '<b class="val">↓ ' + humanSize(s.net.rx_bytes) + '</b>' +
      '<i>↑ ' + humanSize(s.net.tx_bytes) + ' · ' + (s.net.iface || '') + '</i></div>';
  }
  grid.innerHTML = html;
  const sub = document.getElementById('m-sub');
  if (sub) {
    const ts = s.timestamp ? new Date(s.timestamp).toLocaleString() : '';
    sub.textContent = [s.platform, s.hostname, ts].filter(Boolean).join(' · ');
    sub.title = sub.textContent;
  }
}

async function tick() {
  if (document.hidden) return;
  if (!document.getElementById('monitor-page')) { stop(); return; }
  if (abortCtrl) { try { abortCtrl.abort(); } catch (e) {} }
  abortCtrl = new AbortController();
  try {
    const data = await api('/api/monitor/summary', { signal: abortCtrl.signal });
    render(data);
    hasToasted = false;
  } catch (e) {
    if (e.name === 'AbortError') return;
    // 网络抖动：仅首次失败提示一次，成功后重置，避免持续失败时每 3s 弹一次
    if (hasToasted) return;
    hasToasted = true;
    toast('监控数据加载失败：' + e.message, 'err');
  }
}

function stop() {
  if (timer) { clearInterval(timer); timer = null; }
  if (abortCtrl) { try { abortCtrl.abort(); } catch (e) {} abortCtrl = null; }
  document.removeEventListener('visibilitychange', onVis);
}

function onVis() {
  if (!document.hidden) tick();
}

export function renderMonitorPage() {
  const content = document.getElementById('content');
  content.innerHTML =
    '<div class="card" id="monitor-page" role="region" aria-label="系统监控">' +
      '<div style="display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:6px">' +
        '<h2 style="margin:0">系统监控</h2><span class="badge ok" aria-live="polite" id="m-live">实时</span>' +
      '</div>' +
      '<div class="mgrid" id="m-grid" aria-live="polite" aria-busy="true">' +
        '<div class="mcell"><span>加载中…</span><div class="skel" style="height:58px;border-radius:12px"></div></div>' +
        '<div class="mcell"><span>加载中…</span><div class="skel" style="height:58px;border-radius:12px"></div></div>' +
        '<div class="mcell"><span>加载中…</span><div class="skel" style="height:58px;border-radius:12px"></div></div>' +
        '<div class="mcell"><span>加载中…</span><div class="skel" style="height:58px;border-radius:12px"></div></div>' +
      '</div>' +
      '<p class="msub" id="m-sub" aria-live="polite"></p>' +
    '</div>';

  stop();
  // 首帧立即
  tick().finally(() => {
    const g = document.getElementById('m-grid');
    if (g) g.removeAttribute('aria-busy');
  });
  // 轮询：避免堆积，使用 setInterval 但 tick 内 abort 前序
  timer = setInterval(tick, 3000);
  document.addEventListener('visibilitychange', onVis);
}
