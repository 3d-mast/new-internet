(() => {
  const baseRender = render;

  function renderRhizome() {
    baseRender();
    const cfg = state.status?.config || {};
    const auto = state.status?.autopilot || {};
    const enabled = !!cfg.autopilot;

    document.title = 'Rhizome — сеть сама находит путь';
    $('#autopilotReason').textContent = auto.reason || (enabled ? 'Поиск лучшего маршрута…' : 'Сеть остановлена пользователем');
    $('#networkToggle').classList.toggle('on', enabled);
    $('#networkToggle').setAttribute('aria-pressed', enabled ? 'true' : 'false');
    $('#powerText').textContent = enabled ? (auto.active ? 'Сеть работает' : 'Поиск маршрута') : 'Запустить сеть';
    $('#routeChip').textContent = auto.active ? (auto.route || 'готово') : (enabled ? 'поиск' : 'остановлено');

    if (!enabled) {
      $('#heroTitle').textContent = 'Сеть остановлена';
      $('#heroText').textContent = 'Нажмите одну кнопку. Rhizome сам проверит узлы, выберет путь и включит защиту приложений только после успешной проверки.';
    } else if (auto.active) {
      const peer = state.peers[auto.selected_exit];
      $('#heroTitle').textContent = 'Маршрут построен автоматически';
      $('#heroText').textContent = `Выход: ${peer?.name || short(auto.selected_exit)}. Путь: ${auto.route || 'direct'}, задержка ${auto.latency_ms || 0} мс.`;
      $('#bestRoute').textContent = auto.route?.startsWith('relay:') ? 'relay' : 'прямой';
      $('#bestLatency').textContent = `${auto.latency_ms || 0} мс · выбран автоматически`;
    } else {
      $('#heroTitle').textContent = 'Rhizome ищет рабочий путь';
      $('#heroText').textContent = 'Интернет не будет переключён на локальный прокси, пока не найден и не проверен доступный выходной узел.';
      $('#bestRoute').textContent = 'поиск';
      $('#bestLatency').textContent = 'система остаётся в обычной сети';
    }

    $('#selectedExit').disabled = enabled;
    $('#systemProxy').disabled = enabled;
    $('#autoRelay').disabled = enabled;
  }

  render = renderRhizome;

  $('#networkToggle').onclick = async () => {
    try {
      const enabled = !state.status.config.autopilot;
      await api('/api/autopilot', {method:'POST', body:JSON.stringify({enabled})});
      toast(enabled ? 'Автопилот запущен' : 'Сеть и системный прокси остановлены');
      await loadState();
    } catch (error) {
      toast(error.message, true);
    }
  };

  $('#saveNetwork').onclick = async () => {
    try {
      if (state.status.config.autopilot) {
        await api('/api/autopilot', {method:'POST', body:JSON.stringify({enabled:false})});
      }
      await saveSettings();
      toast('Включён ручной режим маршрута');
      await loadState();
    } catch (error) {
      toast(error.message, true);
    }
  };

  setTimeout(() => {
    if (state.status) renderRhizome();
  }, 0);
})();
