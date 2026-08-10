const $ = selector => document.querySelector(selector);
let current = null;

async function api(path, options = {}) {
  const response = await fetch(path, {
    headers: {"Content-Type": "application/json"},
    cache: "no-store",
    ...options
  });
  let body = {};
  try { body = await response.json(); } catch (_) {}
  if (!response.ok) throw new Error(body.error || `HTTP ${response.status}`);
  return body;
}

function toast(message, error = false) {
  const box = $("#toast");
  box.textContent = message;
  box.classList.toggle("error", error);
  box.classList.add("show");
  clearTimeout(toast.timer);
  toast.timer = setTimeout(() => box.classList.remove("show"), 3200);
}

function decodeBase32(value) {
  const alphabet = "abcdefghijklmnopqrstuvwxyz234567";
  let bits = 0;
  let accumulator = 0;
  const bytes = [];
  for (const raw of String(value).toLowerCase()) {
    const index = alphabet.indexOf(raw);
    if (index < 0) continue;
    accumulator = (accumulator << 5) | index;
    bits += 5;
    if (bits >= 8) {
      bits -= 8;
      bytes.push((accumulator >> bits) & 255);
      accumulator &= (1 << bits) - 1;
    }
  }
  return bytes;
}

function logicalAddresses(nodeID) {
  const source = decodeBase32(nodeID);
  while (source.length < 15) source.push(0);
  const ipv4 = `100.${64 + (source[0] & 63)}.${source[1]}.${1 + (source[2] % 254)}`;
  const bytes = [0xfd, ...source.slice(0, 15)];
  const groups = [];
  for (let i = 0; i < 16; i += 2) groups.push(((bytes[i] << 8) | bytes[i + 1]).toString(16));
  return {ipv4, ipv6: groups.join(":")};
}

function shortID(value = "") {
  return value.length > 14 ? `${value.slice(0, 14)}…` : value;
}

async function refresh() {
  const [status, peers, health, diagnostics] = await Promise.all([
    api("/api/status"),
    api("/api/peers"),
    api("/api/health"),
    api("/api/diagnostics")
  ]);
  current = {status, peers, health, diagnostics};
  render();
}

function render() {
  const {status, peers, health, diagnostics} = current;
  const cfg = status.config || {};
  const autopilot = status.autopilot || {};
  const addresses = logicalAddresses(status.node_id);
  const peerValues = Object.values(peers || {});
  const online = Object.values(health || {}).filter(item => item.online).length;
  const dataItem = diagnostics.find(item => item.name === "Каталог данных");

  $("#overlayIPv4").textContent = addresses.ipv4;
  $("#overlayIPv6").textContent = addresses.ipv6;
  $("#interfaceState").textContent = "TUN не назначен";
  $("#interfaceState").className = "warning-text";
  $("#interfaceNote").textContent = "Это логический адрес личности, не системная сетевая карта";
  $("#peerCount").textContent = peerValues.length;
  $("#onlineCount").textContent = `${online} онлайн`;
  $("#nodeName").textContent = cfg.node_name || "—";
  $("#nodeID").textContent = status.node_id || "—";
  $("#selectedExit").textContent = autopilot.selected_exit ? (peers[autopilot.selected_exit]?.name || shortID(autopilot.selected_exit)) : "не выбран";
  $("#routeValue").textContent = autopilot.route || "маршрут ещё не найден";
  $("#latencyValue").textContent = autopilot.latency_ms ? `${autopilot.latency_ms} мс` : "—";
  $("#uiURL").textContent = location.origin;
  $("#dataDir").value = dataItem?.details || "Каталог не определён";
  $("#endpoints").value = (status.endpoints || []).join("\n") || "Нет опубликованных транспортных адресов";
  $("#versionLine").textContent = `${status.product || "Canopy"} ${status.release || "5.0.0"} · ${status.protocol || "LICHEN/2"}`;

  const enabled = !!autopilot.enabled;
  $("#autopilotToggle").disabled = false;
  $("#autopilotToggle").classList.toggle("on", enabled);
  $("#powerLabel").textContent = enabled ? "Остановить сеть" : "Запустить сеть";
  $("#autopilotToggle").setAttribute("aria-pressed", enabled ? "true" : "false");

  let title = "Сеть готова";
  let text = autopilot.reason || "Canopy контролирует маршруты автоматически.";
  let badge = "ожидание";
  let stateClass = "";
  if (!enabled) {
    title = "Сеть остановлена";
    text = "Нажмите одну кнопку — Canopy сам проверит узлы и построит лучший маршрут.";
    badge = "выключено";
  } else if (autopilot.active) {
    title = "Защищённый маршрут работает";
    badge = autopilot.route || "активен";
    stateClass = "online";
  } else if (!peerValues.length) {
    title = "Узел работает — добавьте устройство";
    text = "Создайте приглашение или вставьте полученный ключ. Остальное Canopy настроит сам.";
    badge = "нет узлов";
  } else {
    title = "Поиск рабочего маршрута";
    badge = "поиск";
  }
  $("#heroTitle").textContent = title;
  $("#heroText").textContent = text;
  $("#statusText").textContent = `${cfg.node_name || "Canopy"} · ${shortID(status.node_id)}`;
  $("#statusDot").className = `dot ${stateClass}`;
  $("#routeBadge").textContent = badge;
  $("#routeBadge").classList.toggle("online", !!autopilot.active);
}

$("#autopilotToggle").addEventListener("click", async () => {
  try {
    const enabled = !current.status.autopilot?.enabled;
    await api("/api/autopilot", {method: "POST", body: JSON.stringify({enabled})});
    toast(enabled ? "Сеть запущена" : "Сеть остановлена");
    await refresh();
  } catch (error) { toast(error.message, true); }
});

$("#inviteOpen").addEventListener("click", () => $("#inviteDialog").showModal());
$("#joinOpen").addEventListener("click", () => $("#joinDialog").showModal());

$("#createInvite").addEventListener("click", async () => {
  try {
    const result = await api("/api/invites", {
      method: "POST",
      body: JSON.stringify({
        ttl_minutes: 1440,
        permissions: {
          use_exit: $("#allowExit").checked,
          access_lan: $("#allowLAN").checked,
          relay: $("#allowRelay").checked
        }
      })
    });
    $("#inviteToken").value = result.token;
    await navigator.clipboard?.writeText(result.token);
    toast("Приглашение создано и скопировано");
  } catch (error) { toast(error.message, true); }
});

$("#copyInvite").addEventListener("click", async () => {
  const token = $("#inviteToken").value;
  if (!token) return toast("Сначала создайте приглашение", true);
  await navigator.clipboard?.writeText(token);
  toast("Ключ скопирован");
});

$("#joinButton").addEventListener("click", async () => {
  try {
    const token = $("#joinToken").value.trim();
    if (!token) throw new Error("Вставьте приглашение");
    await api("/api/join", {method: "POST", body: JSON.stringify({token})});
    $("#joinDialog").close();
    $("#joinToken").value = "";
    toast("Устройство подключено. Canopy строит маршрут.");
    await refresh();
  } catch (error) { toast(error.message, true); }
});

$("#copyNodeData").addEventListener("click", async () => {
  if (!current) return;
  const status = current.status;
  const addresses = logicalAddresses(status.node_id);
  const diagnostics = current.diagnostics || [];
  const dataDir = diagnostics.find(item => item.name === "Каталог данных")?.details || "не определён";
  const text = [
    `Продукт: ${status.product} ${status.release}`,
    `Узел: ${status.config?.node_name}`,
    `ID: ${status.node_id}`,
    `Логический IPv4: ${addresses.ipv4}`,
    `Логический IPv6: ${addresses.ipv6}`,
    `TUN/Wintun: не назначен`,
    `Интерфейс: ${location.origin}`,
    `Данные: ${dataDir}`,
    `Endpoints: ${(status.endpoints || []).join(", ")}`
  ].join("\n");
  await navigator.clipboard?.writeText(text);
  toast("Данные узла скопированы");
});

$("#runDiagnostics").addEventListener("click", async () => {
  try {
    const items = await api("/api/diagnostics");
    $("#diagnostics").innerHTML = items.map(item => `<div class="diag ${item.ok ? "ok" : "bad"}"><b>${item.ok ? "✓" : "!"}</b><span><strong>${escapeHTML(item.name)}</strong><small>${escapeHTML(item.details)}</small></span></div>`).join("");
    toast(items.every(item => item.ok) ? "Все проверки пройдены" : "Найдены проблемы", !items.every(item => item.ok));
  } catch (error) { toast(error.message, true); }
});

function escapeHTML(value = "") {
  return String(value).replace(/[&<>"']/g, char => ({"&":"&amp;","<":"&lt;",">":"&gt;","\"":"&quot;","'":"&#39;"}[char]));
}

refresh().catch(error => toast(`Интерфейс не получил данные: ${error.message}`, true));
setInterval(() => refresh().catch(() => {}), 4000);
