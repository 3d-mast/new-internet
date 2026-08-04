const $ = (selector, root=document) => root.querySelector(selector);
const $$ = (selector, root=document) => [...root.querySelectorAll(selector)];
let state = {status:null, peers:{}, health:{}, found:[], tunnels:[], events:[], invites:[]};

async function api(path, options={}) {
  const response = await fetch(path, {headers:{"Content-Type":"application/json"}, ...options});
  let body = {};
  try { body = await response.json(); } catch (_) {}
  if (!response.ok) throw new Error(body.error || `Ошибка HTTP ${response.status}`);
  return body;
}
function esc(value='') { return String(value).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }
function short(id='') { return id.length > 10 ? id.slice(0,10) : id; }
function toast(message, error=false) {
  const box = $('#toast'); box.textContent = message; box.classList.toggle('error', error); box.classList.add('show');
  clearTimeout(toast.timer); toast.timer = setTimeout(() => box.classList.remove('show'), 3200);
}
function permissionTags(perms={}, cls='') {
  return [perms.use_exit && 'интернет', perms.access_lan && 'LAN', perms.relay && 'relay']
    .filter(Boolean).map(name => `<span class="tag ${cls}">${name}</span>`).join('') || '<span class="tag">нет</span>';
}
function timeAgo(value) {
  if (!value) return 'никогда';
  const seconds = Math.max(0, Math.round((Date.now()-new Date(value).getTime())/1000));
  if (seconds < 10) return 'только что'; if (seconds < 60) return `${seconds} сек назад`;
  const min = Math.round(seconds/60); if (min < 60) return `${min} мин назад`;
  return new Date(value).toLocaleString('ru-RU');
}

async function loadState() {
  const [status, peers, health, found, tunnels, events, invites] = await Promise.all([
    api('/api/status'), api('/api/peers'), api('/api/health'), api('/api/discovered'),
    api('/api/tunnels'), api('/api/events'), api('/api/invites')
  ]);
  state = {status, peers, health, found, tunnels, events, invites};
  render();
}

function render() {
  const cfg = state.status.config;
  $('#protocolBadge').textContent = state.status.protocol;
  const peerValues = Object.values(state.peers);
  const healthValues = Object.values(state.health);
  const online = healthValues.filter(item => item.online).length;
  $('#onlineCount').textContent = online;
  $('#peerCount').textContent = `из ${peerValues.length} доверенных`;
  $('#discoveredCount').textContent = state.found.length;
  $('#nearbyCount').textContent = state.found.length;
  $('#mainStatus').textContent = `${cfg.node_name} · ${short(state.status.node_id)}`;
  $('#nodeName').value = cfg.node_name;
  $('#offerExit').checked = cfg.offer_exit;
  $('#autoRelay').checked = cfg.auto_relay;
  $('#systemProxy').checked = cfg.system_proxy;
  $('#manualEndpoints').value = (cfg.manual_endpoints || []).join('\n');

  const exits = peerValues.filter(peer => peer.capabilities?.use_exit);
  const exitOptions = exits.map(peer => `<option value="${esc(peer.id)}">${esc(peer.name)} · ${short(peer.id)}</option>`).join('');
  $('#selectedExit').innerHTML = '<option value="">Выберите узел</option>' + exitOptions;
  $('#selectedExit').value = cfg.selected_exit || '';
  const tunnelPeers = peerValues.filter(peer => peer.capabilities?.access_lan);
  $('#tunnelPeer').innerHTML = '<option value="">Выберите узел</option>' + tunnelPeers.map(peer => `<option value="${esc(peer.id)}">${esc(peer.name)} · ${short(peer.id)}</option>`).join('');

  const selectedHealth = state.health[cfg.selected_exit] || {};
  $('#routeChip').textContent = cfg.selected_exit ? (selectedHealth.online ? selectedHealth.route || 'доступен' : 'нет связи') : 'не выбран';
  $('#bestRoute').textContent = selectedHealth.online ? (selectedHealth.route?.startsWith('relay:') ? 'через relay' : 'прямой') : '—';
  $('#bestLatency').textContent = selectedHealth.online ? `${selectedHealth.latency_ms} мс · ${selectedHealth.endpoint || ''}` : 'нет измерений';
  $('#proxyState').textContent = cfg.proxy_enabled ? 'активен' : 'выключен';
  $('#proxyAddress').textContent = cfg.proxy_enabled ? `${state.status.socks} · ${state.status.http_proxy}` : 'SOCKS5 + HTTP CONNECT';
  $('#networkToggle').classList.toggle('on', cfg.proxy_enabled);
  $('#networkToggle').setAttribute('aria-pressed', cfg.proxy_enabled ? 'true' : 'false');
  $('#powerText').textContent = cfg.proxy_enabled ? 'Выключить сеть' : 'Включить сеть';
  $('#heroTitle').textContent = cfg.proxy_enabled ? (selectedHealth.online ? 'Защищённый маршрут активен' : 'Ожидание выходного узла') : 'Прямое защищённое соединение';
  $('#heroText').textContent = cfg.proxy_enabled
    ? `Трафик направляется через ${esc(state.peers[cfg.selected_exit]?.name || 'выбранный узел')}. Маршрут: ${selectedHealth.route || 'поиск пути'}.`
    : 'Выберите выходной узел и включите сеть одной кнопкой. Lichen сначала использует прямой путь, затем безопасный relay.';

  renderEvents(); renderPeers(); renderFound(); renderTunnels(); renderInvites();
}

function renderEvents() {
  $('#events').innerHTML = state.events.length ? state.events.slice(0,12).map(event => `
    <div class="event"><span class="event-dot ${esc(event.level)}"></span><div><strong>${esc(event.message)}</strong><small>${esc(event.kind)}${event.peer_id ? ` · ${short(event.peer_id)}` : ''}</small></div><time>${timeAgo(event.time)}</time></div>`).join('') : '<div class="empty">Событий пока нет</div>';
}

function renderPeers() {
  const peers = Object.values(state.peers);
  $('#peerCards').innerHTML = peers.length ? peers.map(peer => {
    const health = state.health[peer.id] || {};
    return `<article class="peer-card">
      <div class="peer-card-head"><div class="tunnel-route"><div class="node-icon">⬡</div><div><h3>${esc(peer.name)}</h3><span class="peer-id">${esc(peer.id)}</span></div></div><span class="online-badge ${health.online?'online':''}">${health.online?'онлайн':'нет связи'}</span></div>
      <div class="route-info"><span>${health.route ? esc(health.route) : 'маршрут не определён'}</span><span>${health.online ? `${health.latency_ms} мс` : timeAgo(health.last_online)}</span></div>
      <small>Разрешено этому узлу</small><div class="tag-row">${permissionTags(peer.permissions)}</div>
      <small>Получено от узла</small><div class="tag-row">${permissionTags(peer.capabilities,'cap')}</div>
      <div class="peer-actions"><button class="ghost" data-probe="${esc(peer.id)}">Проверить</button><button class="secondary" data-edit="${esc(peer.id)}">Настроить</button></div>
    </article>`;
  }).join('') : '<div class="empty">Доверенных узлов пока нет. Создайте приглашение или подключитесь по чужому.</div>';
  $$('[data-probe]').forEach(button => button.onclick = () => probePeer(button.dataset.probe));
  $$('[data-edit]').forEach(button => button.onclick = () => openPeer(button.dataset.edit));
}

function renderFound() {
  $('#discoveredList').innerHTML = state.found.length ? state.found.map(node => `<div class="compact-item"><div><strong>${esc(node.name)}</strong><small>${short(node.id)} · ${esc(node.endpoint)}</small></div><span class="chip">${timeAgo(node.seen_at)}</span></div>`).join('') : '<div class="empty">В локальной сети пока никого не найдено.</div>';
}

function renderTunnels() {
  $('#tunnelList').innerHTML = state.tunnels.length ? state.tunnels.map(tunnel => `<article class="tunnel-card"><div class="tunnel-route"><div class="node-icon">⇄</div><div><strong>${esc(tunnel.name || 'TCP-туннель')}</strong><small>${esc(tunnel.listen)} <span class="arrow">→</span> ${esc(tunnel.target)} через ${short(tunnel.peer_id)}</small></div></div><div class="top-actions"><span class="chip">${tunnel.active?'активен':'ошибка'} · ${tunnel.connections} соедин.</span><button class="danger" data-delete-tunnel="${esc(tunnel.id)}">Удалить</button></div></article>`).join('') : '<div class="empty">Туннелей пока нет.</div>';
  $$('[data-delete-tunnel]').forEach(button => button.onclick = async () => { try { await api('/api/tunnels/'+encodeURIComponent(button.dataset.deleteTunnel), {method:'DELETE'}); toast('Туннель удалён'); await loadState(); } catch (e) { toast(e.message,true); } });
}

function renderInvites() {
  $('#inviteHistory').innerHTML = state.invites.length ? state.invites.slice(0,8).map(invite => `<div class="compact-item"><div><strong>${invite.used?'Использовано':'Активно'}</strong><small>${invite.code.slice(0,8)}… · до ${new Date(invite.expires_at).toLocaleString('ru-RU')}</small></div>${invite.used?'':'<button class="ghost" data-revoke="'+esc(invite.code)+'">Отозвать</button>'}</div>`).join('') : '<div class="empty">Нет активных приглашений.</div>';
  $$('[data-revoke]').forEach(button => button.onclick = async () => { try { await api('/api/invites/'+encodeURIComponent(button.dataset.revoke),{method:'DELETE'}); toast('Приглашение отозвано'); await loadState(); } catch(e){ toast(e.message,true); } });
}

async function saveSettings(overrides={}) {
  const cfg = state.status.config;
  const body = {
    node_name: $('#nodeName').value || cfg.node_name,
    offer_exit: $('#offerExit').checked,
    proxy_enabled: overrides.proxy_enabled ?? cfg.proxy_enabled,
    system_proxy: $('#systemProxy').checked,
    selected_exit: $('#selectedExit').value,
    auto_relay: $('#autoRelay').checked,
    manual_endpoints: $('#manualEndpoints').value.split(/\r?\n/).map(v=>v.trim()).filter(Boolean)
  };
  await api('/api/settings',{method:'POST',body:JSON.stringify(body)});
}

async function probePeer(id) {
  try { const health = await api('/api/peers/'+encodeURIComponent(id)+'/probe',{method:'POST',body:'{}'}); toast(health.online?`Узел отвечает: ${health.latency_ms} мс, ${health.route}`:`Нет связи: ${health.error}`,!health.online); await loadState(); } catch(e){ toast(e.message,true); }
}
function openPeer(id) {
  const peer = state.peers[id]; if (!peer) return;
  $('#editPeerID').value=id; $('#peerDialogTitle').textContent=peer.name; $('#editPeerName').value=peer.name; $('#editPeerEndpoints').value=(peer.endpoints||[]).join('\n');
  $('#editAllowExit').checked=!!peer.permissions?.use_exit; $('#editAllowLan').checked=!!peer.permissions?.access_lan; $('#editAllowRelay').checked=!!peer.permissions?.relay;
  $('#peerDialog').showModal();
}

$$('.nav-item').forEach(button => button.onclick = () => {
  $$('.nav-item').forEach(item=>item.classList.toggle('active',item===button));
  $$('.view').forEach(view=>view.classList.toggle('active',view.id===`view-${button.dataset.view}`));
  const titles={network:['СОСТОЯНИЕ СЕТИ','Ваша сеть'],peers:['ДОВЕРИЕ И МАРШРУТЫ','Узлы'],tunnels:['ДОСТУП К СЕРВИСАМ','Туннели'],security:['КЛЮЧИ И ДИАГНОСТИКА','Безопасность']};
  [$('#viewEyebrow').textContent,$('#viewTitle').textContent]=titles[button.dataset.view];
});

$('#inviteOpen').onclick=()=>$('#inviteDialog').showModal();
$('#joinOpen').onclick=()=>$('#joinDialog').showModal();
$('#importProfileOpen').onclick=()=>$('#importDialog').showModal();
$('#refreshBtn').onclick=()=>loadState().catch(e=>toast(e.message,true));
$('#probeAll').onclick=async()=>{ for(const id of Object.keys(state.peers)) await probePeer(id); };
$('#networkToggle').onclick=async()=>{ try { if(!state.status.config.proxy_enabled && !$('#selectedExit').value) throw new Error('Сначала выберите выходной узел'); await saveSettings({proxy_enabled:!state.status.config.proxy_enabled}); toast(state.status.config.proxy_enabled?'Сеть выключена':'Защищённый маршрут включён'); await loadState(); }catch(e){toast(e.message,true);} };
$('#saveNetwork').onclick=async()=>{try{await saveSettings();toast('Маршрут сохранён');await loadState();}catch(e){toast(e.message,true);}};
$('#saveSecurity').onclick=async()=>{try{await saveSettings();toast('Настройки устройства сохранены');await loadState();}catch(e){toast(e.message,true);}};

$('#createInvite').onclick=async()=>{try{const result=await api('/api/invites',{method:'POST',body:JSON.stringify({ttl_minutes:Number($('#inviteTTL').value),permissions:{use_exit:$('#allowExit').checked,access_lan:$('#allowLan').checked,relay:$('#allowRelay').checked}})});$('#inviteToken').value=result.token;await navigator.clipboard?.writeText(result.token);toast('Приглашение создано и скопировано');await loadState();}catch(e){toast(e.message,true);}};
$('#joinBtn').onclick=async()=>{try{await api('/api/join',{method:'POST',body:JSON.stringify({token:$('#joinToken').value.trim()})});$('#joinDialog').close();$('#joinToken').value='';toast('Узел проверен и подключён');await loadState();}catch(e){toast(e.message,true);}};
$('#savePeer').onclick=async()=>{const id=$('#editPeerID').value;try{await api('/api/peers/'+encodeURIComponent(id),{method:'PATCH',body:JSON.stringify({name:$('#editPeerName').value,endpoints:$('#editPeerEndpoints').value.split(/\r?\n/).map(v=>v.trim()).filter(Boolean),permissions:{use_exit:$('#editAllowExit').checked,access_lan:$('#editAllowLan').checked,relay:$('#editAllowRelay').checked}})});$('#peerDialog').close();toast('Права и адреса обновлены');await loadState();}catch(e){toast(e.message,true);}};
$('#removePeer').onclick=async()=>{const id=$('#editPeerID').value;if(!confirm('Отозвать доверие к этому узлу? Его ключ будет удалён.'))return;try{await api('/api/peers/'+encodeURIComponent(id),{method:'DELETE'});$('#peerDialog').close();toast('Доверие отозвано');await loadState();}catch(e){toast(e.message,true);}};
$('#addTunnel').onclick=async()=>{try{await api('/api/tunnels',{method:'POST',body:JSON.stringify({name:$('#tunnelName').value,peer_id:$('#tunnelPeer').value,listen:$('#tunnelListen').value,target:$('#tunnelTarget').value})});toast('Туннель запущен');$('#tunnelName').value='';await loadState();}catch(e){toast(e.message,true);}};
$('#exportProfile').onclick=async()=>{try{const result=await api('/api/profile/export',{method:'POST',body:JSON.stringify({password:$('#backupPassword').value})});$('#profileOutput').value=result.profile;await navigator.clipboard?.writeText(result.profile);toast('Зашифрованный профиль создан и скопирован');}catch(e){toast(e.message,true);}};
$('#importProfile').onclick=async()=>{if(!confirm('Текущий профиль будет заменён. Продолжить?'))return;try{await api('/api/profile/import',{method:'POST',body:JSON.stringify({profile:$('#importProfileText').value.trim(),password:$('#importPassword').value})});toast('Профиль импортирован. Перезапустите Lichen.');$('#importDialog').close();}catch(e){toast(e.message,true);}};
$('#runDiagnostics').onclick=async()=>{try{const items=await api('/api/diagnostics');$('#diagnostics').innerHTML=items.map(item=>`<div class="diagnostic ${item.ok?'ok':''}"><div><strong>${esc(item.name)}</strong><small>${esc(item.details)}</small></div></div>`).join('');toast(items.every(i=>i.ok)?'Все проверки пройдены':'Диагностика нашла проблемы',!items.every(i=>i.ok));}catch(e){toast(e.message,true);}};

loadState().catch(e=>toast(e.message,true));
setInterval(()=>loadState().catch(()=>{}),5000);
