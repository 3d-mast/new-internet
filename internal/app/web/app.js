const $=s=>document.querySelector(s);let state={};
async function api(path,options={}){const r=await fetch(path,{headers:{'Content-Type':'application/json'},...options});const body=await r.json();if(!r.ok)throw new Error(body.error||'Ошибка');return body}
function toast(text){const el=$('#toast');el.textContent=text;el.classList.add('show');setTimeout(()=>el.classList.remove('show'),2600)}
async function refresh(){
  const [status,peers,found,tunnels]=await Promise.all([api('/api/status'),api('/api/peers'),api('/api/discovered'),api('/api/tunnels')]);state={status,peers,found,tunnels};
  $('#identity').textContent=`${status.config.node_name} · ${status.node_id}`;$('#peerCount').textContent=Object.keys(peers).length;$('#discoveredCount').textContent=found.length;$('#socksState').textContent=status.config.socks_enabled?status.socks:'выключен';
  $('#nodeName').value=status.config.node_name;$('#offerExit').checked=status.config.offer_exit;$('#socksEnabled').checked=status.config.socks_enabled;
  const options=Object.values(peers).map(p=>`<option value="${p.id}">${escapeHTML(p.name)} · ${p.id.slice(0,8)}</option>`).join('');
  $('#selectedExit').innerHTML='<option value="">Не выбран</option>'+options;$('#selectedExit').value=status.config.selected_exit||'';$('#tunnelPeer').innerHTML='<option value="">Выбери узел</option>'+options;
  $('#peers').innerHTML=Object.values(peers).map(peerCard).join('')||'<p>Пока пусто. Создай приглашение или подключись по чужому.</p>';
  $('#tunnels').innerHTML=tunnels.map(t=>`<div class="tunnel"><span><b>${escapeHTML(t.listen)}</b> → ${escapeHTML(t.target)} через ${t.peer_id.slice(0,8)}</span><button data-delete="${t.id}">Удалить</button></div>`).join('');
  document.querySelectorAll('[data-delete]').forEach(b=>b.onclick=async()=>{await api('/api/tunnels/'+b.dataset.delete,{method:'DELETE'});toast('Туннель удалён');refresh()});
}
function peerCard(p){const tags=[p.permissions.use_exit&&'выход в интернет',p.permissions.access_lan&&'локальная сеть',p.permissions.relay&&'ретрансляция'].filter(Boolean);return `<div class="peer"><h3>${escapeHTML(p.name)}</h3><code>${p.id}</code><div class="permissions">${tags.map(x=>`<span class="tag">${x}</span>`).join('')||'<span class="tag">без разрешений</span>'}</div></div>`}
function escapeHTML(s=''){return s.replace(/[&<>'"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;',"'":'&#39;','"':'&quot;'}[c]))}
$('#connectBtn').onclick=()=>$('#connectDialog').showModal();
$('#joinBtn').onclick=async e=>{e.preventDefault();try{await api('/api/join',{method:'POST',body:JSON.stringify({token:$('#joinToken').value})});$('#connectDialog').close();toast('Узел подключён');refresh()}catch(err){toast(err.message)}};
$('#createInvite').onclick=async()=>{try{const body=await api('/api/invites',{method:'POST',body:JSON.stringify({ttl_minutes:15,permissions:{use_exit:$('#allowExit').checked,access_lan:$('#allowLan').checked,relay:$('#allowRelay').checked}})});$('#inviteToken').value=body.token;navigator.clipboard?.writeText(body.token);toast('Приглашение создано и скопировано')}catch(err){toast(err.message)}};
$('#saveSettings').onclick=async()=>{try{await api('/api/settings',{method:'POST',body:JSON.stringify({node_name:$('#nodeName').value,offer_exit:$('#offerExit').checked,socks_enabled:$('#socksEnabled').checked,selected_exit:$('#selectedExit').value})});toast('Настройки сохранены');refresh()}catch(err){toast(err.message)}};
$('#addTunnel').onclick=async()=>{try{await api('/api/tunnels',{method:'POST',body:JSON.stringify({peer_id:$('#tunnelPeer').value,listen:$('#tunnelListen').value,target:$('#tunnelTarget').value})});toast('Туннель запущен');refresh()}catch(err){toast(err.message)}};
refresh().catch(e=>toast(e.message));setInterval(()=>refresh().catch(()=>{}),4000);
