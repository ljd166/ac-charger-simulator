/* app.js — AC Charger Simulator Web Console */
const API = '';
let ws = null;
let chargers = {};
let selectedId = null;
let historyData = {}; // id -> { timestamps: [], currents: [], powers: [] }
const maxHistoryPoints = 300;

async function init() {
  await loadState();
  connectWebSocket();
  setupControls();
  setInterval(drawCharts, 1000);
  setInterval(loadState, 5000);
}

async function loadState() {
  try {
    const r = await fetch(API + '/api/state');
    const data = await r.json();
    if (!data.success) return;
    updateGlobal(data.data.global);
    updateChargers(data.data.chargers);
    updateEvents(data.data.events);
  } catch (e) {
    console.error('loadState error', e);
  }
}

function updateGlobal(g) {
  document.getElementById('global-endpoint').textContent = 'Endpoint: ' + (g.ocpp_endpoint || '-');
  document.getElementById('global-runtime').textContent = 'Runtime: ' + g.run_time_sec + 's';
  document.getElementById('global-configured').textContent = 'Configured: ' + g.configured_count;
  document.getElementById('global-connected').textContent = 'Connected: ' + g.connected_count;
  document.getElementById('global-charging').textContent = 'Charging: ' + g.charging_count;
}

function updateChargers(list) {
  const container = document.getElementById('charger-cards');
  let html = '';
  for (const id in list) {
    const c = list[id];
    chargers[id] = c;
    if (!historyData[id]) historyData[id] = { timestamps: [], currents: [], powers: [] };
    historyData[id].timestamps.push(new Date());
    historyData[id].currents.push(c.actual_current_a || 0);
    historyData[id].powers.push(c.power_kw || 0);
    if (historyData[id].timestamps.length > maxHistoryPoints) {
      historyData[id].timestamps.shift();
      historyData[id].currents.shift();
      historyData[id].powers.shift();
    }
    const active = selectedId === id ? 'active' : '';
    html += `<div class="charger-card ${active}" onclick="selectCharger('${id}')">
      <div class="card-id">${id}</div>
      <div class="card-state">${c.ocpp_connection_state} / ${c.charger_status}</div>
      <div class="card-metrics">
        <span>I=${c.actual_current_a?.toFixed(1)}A</span>
        <span>P=${c.power_kw?.toFixed(1)}kW</span>
        <span>SOC=${c.soc?.toFixed(0)}%</span>
      </div>
    </div>`;
  }
  container.innerHTML = html;
}

function updateEvents(events) {
  const container = document.getElementById('log-container');
  let html = '';
  for (const ev of events || []) {
    const time = new Date(ev.timestamp).toLocaleTimeString();
    html += `<div class="log-line">[${time}] ${ev.charger_id || 'SYS'} | ${ev.type}: ${ev.message}</div>`;
  }
  container.innerHTML = html;
  container.scrollTop = container.scrollHeight;
}

function selectCharger(id) {
  selectedId = id;
  document.getElementById('selected-charger-id').textContent = id;
  document.getElementById('control-for').textContent = 'Controlling: ' + id;
  updateChargers(chargers); // re-render cards to show active
}

function connectWebSocket() {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const url = proto + '//' + location.host + '/ws/telemetry';
  ws = new WebSocket(url);
  ws.onmessage = (e) => {
    const msg = JSON.parse(e.data);
    if (msg.type === 'telemetry') {
      const c = msg.telemetry;
      chargers[c.charger_id] = c;
      if (!historyData[c.charger_id]) historyData[c.charger_id] = { timestamps: [], currents: [], powers: [] };
      historyData[c.charger_id].timestamps.push(new Date(c.timestamp));
      historyData[c.charger_id].currents.push(c.actual_current_a || 0);
      historyData[c.charger_id].powers.push(c.power_kw || 0);
      if (historyData[c.charger_id].timestamps.length > maxHistoryPoints) {
        historyData[c.charger_id].timestamps.shift();
        historyData[c.charger_id].currents.shift();
        historyData[c.charger_id].powers.shift();
      }
    } else if (msg.type === 'event') {
      const container = document.getElementById('log-container');
      const ev = msg.event;
      const time = new Date(ev.timestamp).toLocaleTimeString();
      const line = document.createElement('div');
      line.className = 'log-line';
      line.textContent = `[${time}] ${ev.charger_id || 'SYS'} | ${ev.type}: ${ev.message}`;
      container.appendChild(line);
      container.scrollTop = container.scrollHeight;
    } else if (msg.type === 'state') {
      updateGlobal(msg.state);
    }
  };
  ws.onclose = () => {
    setTimeout(connectWebSocket, 3000);
  };
}

function setupControls() {
  document.getElementById('btn-set-endpoint').onclick = async () => {
    const ep = prompt('Enter new OCPP endpoint:', 'ws://127.0.0.1:9000');
    if (!ep) return;
    await post('/api/config/ocpp-endpoint', { endpoint: ep });
    await loadState();
  };
  document.getElementById('btn-start-all').onclick = async () => {
    await post('/api/chargers/all/start', {});
  };
  document.getElementById('btn-stop-all').onclick = async () => {
    await post('/api/chargers/all/stop', {});
  };
  document.getElementById('btn-connect').onclick = async () => {
    if (!selectedId) return alert('Select a charger first');
    await post(`/api/chargers/${selectedId}/connect`, {});
  };
  document.getElementById('btn-disconnect').onclick = async () => {
    if (!selectedId) return alert('Select a charger first');
    await post(`/api/chargers/${selectedId}/disconnect`, {});
  };
  document.getElementById('btn-start').onclick = async () => {
    if (!selectedId) return alert('Select a charger first');
    await post(`/api/chargers/${selectedId}/start`, {});
  };
  document.getElementById('btn-stop').onclick = async () => {
    if (!selectedId) return alert('Select a charger first');
    await post(`/api/chargers/${selectedId}/stop`, {});
  };
  document.getElementById('btn-set-current').onclick = async () => {
    if (!selectedId) return alert('Select a charger first');
    const val = parseFloat(document.getElementById('input-current').value);
    await post(`/api/chargers/${selectedId}/target-current`, { current_a: val });
  };
  document.getElementById('btn-fault').onclick = async () => {
    if (!selectedId) return alert('Select a charger first');
    await post(`/api/chargers/${selectedId}/fault`, { code: 'EarthFailure' });
  };
  document.getElementById('btn-clear-fault').onclick = async () => {
    if (!selectedId) return alert('Select a charger first');
    await post(`/api/chargers/${selectedId}/fault`, { code: '' });
  };
  document.getElementById('btn-profile').onclick = async () => {
    if (!selectedId) return alert('Select a charger first');
    const profile = prompt('Enter profile name:', 'default');
    if (!profile) return;
    await post(`/api/chargers/${selectedId}/profile`, { profile });
  };
}

async function post(path, body) {
  try {
    const r = await fetch(API + path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    });
    const data = await r.json();
    if (!data.success) alert('Error: ' + data.message);
  } catch (e) {
    alert('Request failed: ' + e);
  }
}

function drawCharts() {
  if (!selectedId || !historyData[selectedId]) return;
  const hd = historyData[selectedId];
  drawChart('current-chart', 'Current (A)', hd.currents, '#3498db', 0, 40);
  drawChart('power-chart', 'Power (kW)', hd.powers, '#e74c3c', 0, 12);
}

function drawChart(canvasId, label, data, color, minY, maxY) {
  const canvas = document.getElementById(canvasId);
  const ctx = canvas.getContext('2d');
  const w = canvas.width;
  const h = canvas.height;
  ctx.clearRect(0, 0, w, h);

  // Grid
  ctx.strokeStyle = '#333';
  ctx.lineWidth = 1;
  ctx.beginPath();
  for (let i = 0; i <= 4; i++) {
    const y = h - (i / 4) * h;
    ctx.moveTo(0, y);
    ctx.lineTo(w, y);
  }
  ctx.stroke();

  if (data.length < 2) return;

  // Line
  ctx.strokeStyle = color;
  ctx.lineWidth = 2;
  ctx.beginPath();
  const range = maxY - minY;
  for (let i = 0; i < data.length; i++) {
    const x = (i / (data.length - 1)) * w;
    const y = h - ((data[i] - minY) / range) * h;
    if (i === 0) ctx.moveTo(x, y);
    else ctx.lineTo(x, y);
  }
  ctx.stroke();

  // Label
  ctx.fillStyle = '#aaa';
  ctx.font = '12px sans-serif';
  ctx.fillText(label, 8, 14);
  if (data.length > 0) {
    const last = data[data.length - 1];
    ctx.fillText(last.toFixed(2), w - 60, 14);
  }
}

init();
