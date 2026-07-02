/* app.js — AC 充电桩仿真台 v0.3
 * 实时:WebSocket /ws/telemetry(telemetry/event/state) + /api/state 轮询兜底
 * 图表:纯 SVG 双通道(功率/电流),/api/chargers/{id}/history 回填
 */
const API = '';
const HISTORY_WINDOW_SEC = 600;

const $ = (sel, root) => (root || document).querySelector(sel);
const grid = $('#charger-grid');
const evBody = $('#events-body');

const cards = {};      // id -> {el, refs, series: [{t, p, c}], lastTs}
let evFilter = 'all';
let globalPowerCache = {};

/* ───────── 工具 ───────── */
async function api(path, method, body) {
  const opt = { method: method || 'GET', headers: { 'Content-Type': 'application/json' } };
  if (body !== undefined) opt.body = JSON.stringify(body);
  const r = await fetch(API + path, opt);
  const j = await r.json().catch(() => ({}));
  if (!r.ok || j.success === false) throw new Error(j.message || ('HTTP ' + r.status));
  return j;
}
function toast(msg, isErr) {
  let t = $('#toast');
  if (!t) { t = document.createElement('div'); t.id = 'toast'; document.body.appendChild(t); }
  t.textContent = msg;
  t.className = 'show' + (isErr ? ' err' : '');
  clearTimeout(t._h);
  t._h = setTimeout(() => (t.className = ''), 2600);
}
function fmtRuntime(sec) {
  if (sec == null) return '-';
  const h = Math.floor(sec / 3600), m = Math.floor((sec % 3600) / 60), s = sec % 60;
  return (h ? h + 'h' : '') + m + 'm' + (h ? '' : s + 's');
}
function fmtTime(ts) {
  const d = new Date(ts);
  return d.toLocaleTimeString('zh-CN', { hour12: false });
}

const CONN_TXT = { connected: '已连接', connecting: '连接中', disconnected: '未连接', error: '连接错误' };
const ST_TXT = {
  Available: '空闲', Preparing: '准备中', Charging: '充电中', Finishing: '结束中',
  SuspendedEV: '车端暂停', SuspendedEVSE: '桩端暂停', Faulted: '故障', Unavailable: '不可用',
};

/* ───────── 卡片 ───────── */
function makeCard(id, snap) {
  const tpl = $('#card-tpl');
  const el = tpl.content.firstElementChild.cloneNode(true);
  const refs = {};
  el.querySelectorAll('[data-el]').forEach(n => (refs[n.dataset.el] = n));
  refs.cid.textContent = id;

  const maxA = (snap && snap.max_current_a) || 32;
  refs['limit-slider'].max = maxA; refs['limit-num'].max = maxA;
  refs['limit-slider'].value = maxA; refs['limit-num'].value = maxA;
  refs['limit-slider'].oninput = () => (refs['limit-num'].value = refs['limit-slider'].value);
  refs['limit-num'].oninput = () => (refs['limit-slider'].value = refs['limit-num'].value);

  el.querySelectorAll('[data-act]').forEach(btn => {
    btn.onclick = async () => {
      const act = btn.dataset.act;
      btn.disabled = true;
      try {
        if (act === 'conn') {
          const c = cards[id].lastSnap;
          const connected = c && (c.ocpp_connection_state === 'connected' || c.ocpp_connection_state === 'connecting');
          await api(`/api/chargers/${id}/${connected ? 'disconnect' : 'connect'}`, 'POST', {});
          toast(id + (connected ? ' 已断开' : ' 连接中…'));
        } else if (act === 'start') {
          await api(`/api/chargers/${id}/start`, 'POST', {});
          toast(id + ' 启动指令已发');
        } else if (act === 'stop') {
          await api(`/api/chargers/${id}/stop`, 'POST', {});
          toast(id + ' 停止指令已发');
        } else if (act === 'limit') {
          const v = parseFloat(refs['limit-num'].value);
          await api(`/api/chargers/${id}/target-current`, 'POST', { current_a: v });
          toast(`${id} 限流 → ${v}A`);
        } else if (act === 'setsoc') {
          const v = parseFloat(refs['soc-num-input'].value);
          await api(`/api/chargers/${id}/soc`, 'POST', { soc: v });
          toast(`${id} SOC → ${v}%`);
        }
      } catch (e) { toast(id + ': ' + e.message, true); }
      btn.disabled = false;
    };
  });
  refs.profile.onchange = async () => {
    try { await api(`/api/chargers/${id}/profile`, 'POST', { profile: refs.profile.value }); toast(id + ' 曲线 → ' + refs.profile.options[refs.profile.selectedIndex].text); }
    catch (e) { toast(e.message, true); }
  };
  refs.fault.onchange = async () => {
    try { await api(`/api/chargers/${id}/fault`, 'POST', { code: refs.fault.value }); toast(id + ' 故障 → ' + refs.fault.value); }
    catch (e) { toast(e.message, true); }
  };

  grid.appendChild(el);
  cards[id] = { el, refs, series: [], lastSnap: null };
  // 历史回填
  api(`/api/chargers/${id}/history`).then(j => {
    const pts = (j.data || []).map(p => ({ t: +new Date(p.timestamp), p: p.power_kw, c: p.current_a }));
    cards[id].series = pts.concat(cards[id].series);
    drawChart(id);
  }).catch(() => {});
  return cards[id];
}

function updateCard(id, snap) {
  const card = cards[id] || makeCard(id, snap);
  const r = card.refs;
  card.lastSnap = snap;

  const conn = snap.ocpp_connection_state || 'disconnected';
  const st = snap.charger_status || '-';

  r.ring.className = 'conn-ring ' + conn;
  r['conn-pill'].textContent = CONN_TXT[conn] || conn;
  r['conn-pill'].className = 'pill conn-' + conn;
  r['status-pill'].textContent = (ST_TXT[st] || st);
  r['status-pill'].className = 'pill status st-' + st;

  const faulted = snap.fault_code && snap.fault_code !== 'NoError';
  r['fault-pill'].classList.toggle('hidden', !faulted);
  if (faulted) r['fault-pill'].textContent = '⚠ ' + snap.fault_code;

  card.el.classList.toggle('is-charging', st === 'Charging');
  card.el.classList.toggle('is-faulted', faulted || st === 'Faulted');

  r.tx.textContent = snap.transaction_id ? 'txID ' + snap.transaction_id : '';
  r.power.textContent = (snap.power_kw || 0).toFixed(2);
  r.current.textContent = (snap.actual_current_a || 0).toFixed(1);
  r.target.textContent = (snap.target_current_a || 0).toFixed(0);
  r.energy.textContent = (snap.energy_kwh || 0).toFixed(3);
  r.voltage.textContent = (snap.voltage_v || 0).toFixed(0);
  r.phase.textContent = (snap.phase_count === 3 ? '三相' : '单相') + (snap.phase_assignment ? ' ' + snap.phase_assignment : '');

  const soc = snap.soc || 0;
  r.soc.textContent = soc.toFixed(1) + '%';
  r['soc-fill'].style.width = Math.min(100, soc) + '%';
  r['soc-fill'].classList.toggle('charging', st === 'Charging');

  // 电池信息 + 目标线 + 预计时间(SOC 由充电能量物理驱动)
  const cap = snap.battery_capacity_kwh || 0, tgt = snap.target_soc || 0;
  if (tgt > 0) r['soc-target'].style.left = Math.min(100, tgt) + '%';
  r['battery-info'].textContent = cap ? `电池 ${cap}kWh · 目标 ${tgt}% · 效率92%` : '';
  if (st === 'Charging' && (snap.power_kw || 0) > 0.05 && tgt > soc) {
    const hours = ((tgt - soc) / 100 * cap) / ((snap.power_kw) * 0.92);
    const mins = Math.round(hours * 60);
    r.eta.textContent = '⏱ 预计 ' + (mins >= 90 ? (hours.toFixed(1) + ' 小时') : (mins + ' 分钟')) + '充至目标';
  } else if (st === 'Charging' && soc >= tgt) {
    r.eta.textContent = '已达目标,即将自动停充';
  } else {
    r.eta.textContent = '';
  }

  // 连接按钮文案
  const connBtn = card.el.querySelector('[data-act="conn"]');
  connBtn.textContent = (conn === 'connected' || conn === 'connecting') ? '断开' : '连接';

  // 序列
  const t = snap.timestamp ? +new Date(snap.timestamp) : Date.now();
  const s = card.series;
  if (!s.length || t > s[s.length - 1].t) {
    s.push({ t, p: snap.power_kw || 0, c: snap.actual_current_a || 0 });
    const cutoff = Date.now() - HISTORY_WINDOW_SEC * 1000;
    while (s.length && s[0].t < cutoff) s.shift();
    drawChart(id);
  }
  globalPowerCache[id] = snap.power_kw || 0;
  renderGlobalPower();
}

/* ───────── SVG 曲线 ───────── */
function drawChart(id) {
  const card = cards[id];
  const svg = card.refs.chart;
  const s = card.series;
  const W = 560, H = 120, PAD = 6;
  if (s.length < 2) { svg.innerHTML = ''; return; }

  const t0 = s[0].t, t1 = s[s.length - 1].t, span = Math.max(t1 - t0, 1);
  const pMax = Math.max(1, ...s.map(d => d.p)) * 1.15;
  const cMax = Math.max(1, ...s.map(d => d.c)) * 1.15;
  const x = t => PAD + ((t - t0) / span) * (W - 2 * PAD);
  const yP = v => H - PAD - (v / pMax) * (H - 2 * PAD);
  const yC = v => H - PAD - (v / cMax) * (H - 2 * PAD);
  const path = (fy, key) => s.map((d, i) => (i ? 'L' : 'M') + x(d.t).toFixed(1) + ',' + fy(d[key]).toFixed(1)).join(' ');

  const pPath = path(yP, 'p');
  const areaP = pPath + ` L${x(t1).toFixed(1)},${H - PAD} L${x(t0).toFixed(1)},${H - PAD} Z`;

  svg.innerHTML = `
    <defs><linearGradient id="gp-${id}" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0%" stop-color="#4f8cff" stop-opacity="0.35"/>
      <stop offset="100%" stop-color="#4f8cff" stop-opacity="0.02"/>
    </linearGradient></defs>
    <line x1="${PAD}" y1="${H - PAD}" x2="${W - PAD}" y2="${H - PAD}" stroke="#1c2850" stroke-width="1"/>
    <path d="${areaP}" fill="url(#gp-${id})"/>
    <path d="${pPath}" fill="none" stroke="#4f8cff" stroke-width="2" stroke-linejoin="round"/>
    <path d="${path(yC, 'c')}" fill="none" stroke="#35c9dd" stroke-width="1.6" stroke-dasharray="1,0" opacity="0.9"/>
    <text x="${PAD + 4}" y="14" fill="#5a6488" font-size="10" font-family="monospace">${pMax.toFixed(1)}kW / ${cMax.toFixed(0)}A</text>`;
}

/* ───────── 全局状态 ───────── */
function renderGlobal(g) {
  if (!g) return;
  $('#g-endpoint').textContent = g.ocpp_endpoint || '-';
  $('#g-endpoint').title = g.ocpp_endpoint || '';
  $('#foot-endpoint').textContent = g.ocpp_endpoint || '';
  $('#g-connected').textContent = `${g.connected_count}/${g.configured_count}`;
  $('#g-charging').textContent = g.charging_count;
  $('#g-runtime').textContent = fmtRuntime(g.run_time_sec);
}
function renderGlobalPower() {
  const total = Object.values(globalPowerCache).reduce((a, b) => a + b, 0);
  $('#g-power').textContent = total.toFixed(2) + ' kW';
}

/* ───────── 事件流 ───────── */
const seenEvents = new Set();
function evKey(e) { return e.timestamp + '|' + e.charger_id + '|' + e.type + '|' + e.message; }
function addEvent(e, backfill) {
  const key = evKey(e);
  if (seenEvents.has(key)) return;
  seenEvents.add(key);
  if (seenEvents.size > 3000) { const it = seenEvents.values().next(); seenEvents.delete(it.value); }

  ensureFilterChip(e.charger_id);
  const row = document.createElement('div');
  row.className = 'ev';
  row.dataset.cid = e.charger_id || '';
  row.innerHTML = `<span class="ev-time">${fmtTime(e.timestamp)}</span>` +
    `<span class="ev-id">${e.charger_id || '-'}</span>` +
    `<span class="ev-type t-${e.type}">${e.type}</span>` +
    `<span class="ev-msg"></span>`;
  row.lastElementChild.textContent = e.message;
  if (evFilter !== 'all' && e.charger_id !== evFilter) row.style.display = 'none';
  evBody.appendChild(row);
  while (evBody.children.length > 500) evBody.firstChild.remove();
  if (!backfill && $('#ev-autoscroll').checked) evBody.scrollTop = evBody.scrollHeight;
}
function ensureFilterChip(cid) {
  if (!cid) return;
  const holder = $('#event-filters');
  if (holder.querySelector(`[data-filter="${cid}"]`)) return;
  const b = document.createElement('button');
  b.className = 'chip'; b.dataset.filter = cid; b.textContent = cid;
  holder.appendChild(b);
}
$('#event-filters').onclick = e => {
  const chip = e.target.closest('.chip'); if (!chip) return;
  evFilter = chip.dataset.filter;
  document.querySelectorAll('#event-filters .chip').forEach(c => c.classList.toggle('active', c === chip));
  evBody.querySelectorAll('.ev').forEach(r => (r.style.display = evFilter === 'all' || r.dataset.cid === evFilter ? '' : 'none'));
};
$('#btn-clear-events').onclick = () => { evBody.innerHTML = ''; };

/* ───────── 全局操作 ───────── */
$('#btn-all-start').onclick = async () => { try { await api('/api/chargers/all/start', 'POST', {}); toast('全部启动指令已发'); } catch (e) { toast(e.message, true); } };
$('#btn-all-stop').onclick = async () => { try { await api('/api/chargers/all/stop', 'POST', {}); toast('全部停止指令已发'); } catch (e) { toast(e.message, true); } };

/* ───────── 数据链路:WS + 轮询兜底 ───────── */
let ws = null, wsOK = false;
function connectWS() {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  try { ws = new WebSocket(`${proto}://${location.host}/ws/telemetry`); } catch { return; }
  ws.onopen = () => { wsOK = true; $('#ws-dot').classList.add('on'); };
  ws.onclose = ws.onerror = () => {
    wsOK = false; $('#ws-dot').classList.remove('on');
    setTimeout(connectWS, 3000);
  };
  ws.onmessage = m => {
    let b; try { b = JSON.parse(m.data); } catch { return; }
    if (b.type === 'telemetry' && b.charger_id) updateCard(b.charger_id, b.telemetry);
    else if (b.type === 'event' && b.event) addEvent(b.event);
    else if (b.type === 'state' && b.state) renderGlobal(b.state);
  };
}

async function poll() {
  try {
    const j = await api('/api/state');
    const d = j.data || {};
    renderGlobal(d.global);
    const chargers = d.chargers || {};
    // 稳定排序渲染
    Object.keys(chargers).sort().forEach(id => updateCard(id, chargers[id]));
    (d.events || []).slice().reverse().forEach(e => addEvent(e, true));
  } catch (e) { /* 静默,下轮再试 */ }
}

/* 启动:先全量拉一次,再开 WS;轮询兜底(WS 通时低频校准全局计数) */
poll().then(connectWS);
setInterval(() => { if (!wsOK) poll(); else api('/api/state').then(j => renderGlobal((j.data || {}).global)).catch(() => {}); }, wsFallbackInterval());
function wsFallbackInterval() { return 3000; }
