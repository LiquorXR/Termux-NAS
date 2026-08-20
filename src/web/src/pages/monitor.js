// pages/monitor.js — 系统监控看板(JS 渲染,3s 轮询,环形进度 + 阈值变色)。
import { api, humanSize, fmtUptime } from '../api.js';

let timer = null;

function levelColor(pct) {
  if (pct >= 90) return 'var(--danger)';
  if (pct >= 80) return 'var(--warn)';
  return 'var(--accent)';
}

function ringCell(label, pct, detail) {
  const c = levelColor(pct);
  const html = '<div class="mcell"><span>' + label + '</span><div class="ringrow">' +
    '<div class="ring" style="--pct:' + pct.toFixed(1) + ';--c:' + c + '">' +
    '<b>' + pct.toFixed(0) + '%</b></div>' +
    '<div>' + (detail ? '<i>' + detail + '</i>' : '') + '</div></div></div>';
  return html;
}

function render(s) {
  const grid = document.getElementById('m-grid');
  if (!grid) return;
  let html = '';
  html += ringCell('CPU', s.cpu_percent);
  html += ringCell('内存', s.mem_percent,
    humanSize(s.mem_used_bytes) + ' / ' + humanSize(s.mem_total_bytes));
  html += ringCell('磁盘', s.disk_percent,
    '可用 ' + humanSize(s.disk_free_bytes) + ' / ' + humanSize(s.disk_total_bytes));
  html += '<div class="mcell"><span>运行时长</span><b class="val">' +
    fmtUptime(s.uptime_seconds) + '</b><i>daemon 已运行</i></div>';
  if (s.battery) {
    html += '<div class="mcell"><span>电量</span><div class="ringrow">' +
      '<div class="ring" style="--pct:' + s.battery.percentage + ';--c:var(--ok)">' +
      '<b>' + s.battery.percentage + '%</b></div>' +
      '<div><i>' + s.battery.status + '</i>' +
      (s.battery.temperature > 0
        ? '<i> ' + s.battery.temperature.toFixed(1) + '℃</i>' : '') +
      '</div></div></div>';
  }
  if (s.net) {
    html += '<div class="mcell"><span>网络流量</span>' +
      '<b class="val">↓ ' + humanSize(s.net.rx_bytes) + '</b>' +
      '<i>↑ ' + humanSize(s.net.tx_bytes) + '</i></div>';
  }
  grid.innerHTML = html;
  const sub = document.getElementById('m-sub');
  if (sub) {
    sub.textContent = s.platform + ' · ' + s.hostname + ' · ' + s.timestamp;
  }
}

export function renderMonitorPage() {
  const content = document.getElementById('content');
  content.innerHTML =
    '<div class="card" id="monitor-page">' +
      '<h2>系统监控</h2>' +
      '<div class="mgrid" id="m-grid">' +
        '<div class="mcell"><span>加载中…</span></div>' +
      '</div>' +
      '<p class="msub" id="m-sub"></p>' +
    '</div>';

  if (timer) clearInterval(timer);
  timer = setInterval(async () => {
    // 页面已被切换覆盖时停止轮询
    if (!document.getElementById('monitor-page')) {
      clearInterval(timer);
      timer = null;
      return;
    }
    try {
      render(await api('/api/monitor/summary'));
    } catch (e) { /* 静默,下轮重试 */ }
  }, 3000);
}
