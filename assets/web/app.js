// FreeNode Frontend Application Logic

let selectedNodeId = null;
let activeConnection = null;
let isConnecting = false;
let currentPage = 1;
const pageSize = 50;

// Elements
const btnScan = document.getElementById('btnScan');
const btnCancelScan = document.getElementById('btnCancelScan');
const btnConnect = document.getElementById('btnConnect');
const scanBanner = document.getElementById('scanBanner');
const nodesTableBody = document.getElementById('nodesTableBody');
const searchInput = document.getElementById('searchInput');
const filterStatus = document.getElementById('filterStatus');
const filterProto = document.getElementById('filterProto');
const filterCountry = document.getElementById('filterCountry');
const sortBy = document.getElementById('sortBy');

// Modals & Actions
const btnSources = document.getElementById('btnSources');
const btnSettings = document.getElementById('btnSettings');
const btnRouting = document.getElementById('btnRouting');
const btnPingAll = document.getElementById('btnPingAll');
const btnSub = document.getElementById('btnSub');
const btnHistory = document.getElementById('btnHistory');
const btnClearNodes = document.getElementById('btnClearNodes');
const tabFavorites = document.getElementById('tabFavorites');
let isScanningOrPinging = false;
let isFavoritesFilterActive = false;
let latestDownloadURL = '';

// Initialize
document.addEventListener('DOMContentLoaded', () => {
  initEventListeners();
  restorePreferences();
  loadSettings();
  loadStatus();
  loadNodes();
  loadSources();
  initEventStream();

  // Poll status periodically as backup
  setInterval(loadStatus, 4000);
});

function restorePreferences() {
  try {
    // 1. Restore Filter Selections: filterStatus, filterProto, filterCountry, filterSort
    const savedStatus = localStorage.getItem('filterStatus');
    if (savedStatus !== null && filterStatus) {
      filterStatus.value = savedStatus;
      if (savedStatus === 'favorites') {
        isFavoritesFilterActive = true;
        if (tabFavorites) tabFavorites.classList.add('active');
      }
    }

    const savedProto = localStorage.getItem('filterProto');
    if (savedProto !== null && filterProto) {
      filterProto.value = savedProto;
    }

    const savedCountry = localStorage.getItem('filterCountry');
    if (savedCountry !== null && filterCountry) {
      let opt = Array.from(filterCountry.options).find(o => o.value === savedCountry);
      if (!opt && savedCountry !== 'all') {
        opt = document.createElement('option');
        opt.value = savedCountry;
        opt.textContent = `${getFlagEmoji(savedCountry)} ${savedCountry}`;
        filterCountry.appendChild(opt);
      }
      filterCountry.value = savedCountry;
    }

    const savedSort = localStorage.getItem('filterSort') || localStorage.getItem('sortBy');
    const sortElem = document.getElementById('filterSort') || sortBy;
    if (savedSort !== null && sortElem) {
      sortElem.value = savedSort;
    }

    // 2. Restore Scanner Preferences (Clean IP Scanner controls)
    ['cleanIPWorkers', 'cleanIPTimeout', 'cleanIPSampleSize', 'cleanIPPort'].forEach(id => {
      const saved = localStorage.getItem(id);
      const el = document.getElementById(id);
      if (saved !== null && el) {
        el.value = saved;
      }
    });

    // 3. Restore Scanner Preferences (Settings modal / scanner configs)
    ['setConcurrency', 'setTimeout', 'setEndpoint', 'setAutoScanInterval'].forEach(id => {
      const saved = localStorage.getItem(id);
      const el = document.getElementById(id);
      if (saved !== null && el) {
        el.value = saved;
      }
    });
    const savedAutoScan = localStorage.getItem('setAutoScan');
    const autoScanEl = document.getElementById('setAutoScan');
    if (savedAutoScan !== null && autoScanEl) {
      autoScanEl.checked = savedAutoScan === 'true';
    }
  } catch (err) {
    console.warn('Failed restoring preferences from localStorage:', err);
  }
}

function initEventListeners() {
  btnScan.addEventListener('click', startScan);
  btnCancelScan.addEventListener('click', cancelScan);
  btnConnect.addEventListener('click', toggleConnection);
  btnClearNodes.addEventListener('click', clearAllNodes);
  if (btnPingAll) btnPingAll.addEventListener('click', triggerPingAll);

  if (tabFavorites) {
    tabFavorites.addEventListener('click', () => {
      isFavoritesFilterActive = !isFavoritesFilterActive;
      tabFavorites.classList.toggle('active', isFavoritesFilterActive);
      if (isFavoritesFilterActive) {
        filterStatus.value = 'favorites';
      } else if (filterStatus.value === 'favorites') {
        filterStatus.value = 'working';
      }
      try { localStorage.setItem('filterStatus', filterStatus.value); } catch (e) {}
      currentPage = 1;
      loadNodes();
    });
  }

  searchInput.addEventListener('input', debounce(loadNodes, 300));
  filterStatus.addEventListener('change', () => {
    if (filterStatus.value === 'favorites') {
      isFavoritesFilterActive = true;
      if (tabFavorites) tabFavorites.classList.add('active');
    } else {
      isFavoritesFilterActive = false;
      if (tabFavorites) tabFavorites.classList.remove('active');
    }
    try { localStorage.setItem('filterStatus', filterStatus.value); } catch (e) {}
    currentPage = 1;
    loadNodes();
  });
  filterProto.addEventListener('change', () => {
    try { localStorage.setItem('filterProto', filterProto.value); } catch (e) {}
    currentPage = 1;
    loadNodes();
  });
  filterCountry.addEventListener('change', () => {
    try { localStorage.setItem('filterCountry', filterCountry.value); } catch (e) {}
    currentPage = 1;
    loadNodes();
  });
  const sortElem = document.getElementById('filterSort') || sortBy;
  if (sortElem) {
    sortElem.addEventListener('change', () => {
      try {
        localStorage.setItem('filterSort', sortElem.value);
        localStorage.setItem('sortBy', sortElem.value);
      } catch (e) {}
      currentPage = 1;
      loadNodes();
    });
  }
  if (sortBy && sortBy !== sortElem) {
    sortBy.addEventListener('change', () => {
      try {
        localStorage.setItem('filterSort', sortBy.value);
        localStorage.setItem('sortBy', sortBy.value);
      } catch (e) {}
      currentPage = 1;
      loadNodes();
    });
  }

  // Scanner preferences persistence on change
  ['cleanIPWorkers', 'cleanIPTimeout', 'cleanIPSampleSize', 'cleanIPPort'].forEach(id => {
    const el = document.getElementById(id);
    if (el) {
      const saveFn = () => {
        try { localStorage.setItem(id, el.value); } catch (e) {}
      };
      el.addEventListener('change', saveFn);
      el.addEventListener('input', saveFn);
    }
  });

  ['setConcurrency', 'setTimeout', 'setEndpoint', 'setAutoScanInterval'].forEach(id => {
    const el = document.getElementById(id);
    if (el) {
      const saveFn = () => {
        try { localStorage.setItem(id, el.value); } catch (e) {}
      };
      el.addEventListener('change', saveFn);
      el.addEventListener('input', saveFn);
    }
  });

  const autoScanEl = document.getElementById('setAutoScan');
  if (autoScanEl) {
    autoScanEl.addEventListener('change', () => {
      try { localStorage.setItem('setAutoScan', autoScanEl.checked ? 'true' : 'false'); } catch (e) {}
    });
  }

  const btnCheckUpdate = document.getElementById('btnCheckUpdate');
  if (btnCheckUpdate) btnCheckUpdate.addEventListener('click', checkForUpdates);
  const btnApplyUpdate = document.getElementById('btnApplyUpdate');
  if (btnApplyUpdate) btnApplyUpdate.addEventListener('click', applyUpdate);

  document.getElementById('btnPrevPage').addEventListener('click', () => {
    if (currentPage > 1) { currentPage--; loadNodes(); }
  });
  document.getElementById('btnNextPage').addEventListener('click', () => {
    currentPage++; loadNodes();
  });

  // Modal triggers
  btnSources.addEventListener('click', () => openModal('sourcesModal', loadSourcesTable));
  btnSettings.addEventListener('click', () => openModal('settingsModal', loadSettings));
  if (btnRouting) btnRouting.addEventListener('click', () => openModal('routingModal', loadRoutingSettings));
  btnSub.addEventListener('click', () => openModal('subModal'));
  btnHistory.addEventListener('click', () => openModal('historyModal', loadHistory));
  const btnCleanIP = document.getElementById('btnCleanIP');
  if (btnCleanIP) btnCleanIP.addEventListener('click', () => openModal('cleanIPModal', loadCleanIPStatus));
  const btnStartCleanIP = document.getElementById('btnStartCleanIP');
  if (btnStartCleanIP) btnStartCleanIP.addEventListener('click', startCleanIPScan);
  const btnCancelCleanIP = document.getElementById('btnCancelCleanIP');
  if (btnCancelCleanIP) btnCancelCleanIP.addEventListener('click', cancelCleanIPScan);
  const btnConnectFastestCleanIP = document.getElementById('btnConnectFastestCleanIP');
  if (btnConnectFastestCleanIP) btnConnectFastestCleanIP.addEventListener('click', connectFastestCleanIP);
  const btnSaveFastestCleanIP = document.getElementById('btnSaveFastestCleanIP');
  if (btnSaveFastestCleanIP) btnSaveFastestCleanIP.addEventListener('click', addFastestCleanIPToConfigs);
  const btnSaveAllCleanIP = document.getElementById('btnSaveAllCleanIP');
  if (btnSaveAllCleanIP) btnSaveAllCleanIP.addEventListener('click', addAllCleanIPsToConfigs);

  document.querySelectorAll('.modal-close').forEach(btn => {
    btn.addEventListener('click', () => closeModal(btn.dataset.close));
  });

  window.addEventListener('click', (e) => {
    if (e.target.classList.contains('modal')) {
      e.target.classList.remove('open');
    }
  });

  // Settings form
  document.getElementById('settingsForm').addEventListener('submit', saveSettings);
  // Routing form & mode radios
  const routingForm = document.getElementById('routingForm');
  if (routingForm) routingForm.addEventListener('submit', saveRoutingSettings);
  document.querySelectorAll('input[name="routeModeRadio"]').forEach(radio => {
    radio.addEventListener('change', (e) => switchRoutingMode(e.target.value));
  });
  const btnResetBlacklist = document.getElementById('btnResetBlacklist');
  if (btnResetBlacklist) btnResetBlacklist.addEventListener('click', resetBlacklistDefaults);
  const btnResetWhitelist = document.getElementById('btnResetWhitelist');
  if (btnResetWhitelist) btnResetWhitelist.addEventListener('click', resetWhitelistDefaults);

  // Visual App Picker for Split Tunneling
  document.querySelectorAll('.btn-browse-active-apps').forEach(btn => {
    btn.addEventListener('click', (e) => {
      e.preventDefault();
      const target = btn.dataset.target || 'direct';
      openAppPicker(target);
    });
  });

  const appPickerSearch = document.getElementById('appPickerSearch');
  const appPickerClear = document.getElementById('appPickerClearSearch');
  if (appPickerSearch) {
    appPickerSearch.addEventListener('input', () => {
      if (appPickerClear) {
        appPickerClear.style.display = appPickerSearch.value ? 'block' : 'none';
      }
      renderAppPickerList();
    });
  }
  if (appPickerClear) {
    appPickerClear.addEventListener('click', () => {
      if (appPickerSearch) {
        appPickerSearch.value = '';
        appPickerClear.style.display = 'none';
        renderAppPickerList();
        appPickerSearch.focus();
      }
    });
  }

  document.querySelectorAll('.app-picker-tab').forEach(tab => {
    tab.addEventListener('click', () => {
      document.querySelectorAll('.app-picker-tab').forEach(t => t.classList.remove('is-active'));
      tab.classList.add('is-active');
      currentAppFilter = tab.dataset.filter || 'running';
      renderAppPickerList();
    });
  });

  const btnAppRefresh = document.getElementById('appPickerRefreshBtn');
  if (btnAppRefresh) {
    btnAppRefresh.addEventListener('click', () => loadAppPickerData(true));
  }

  const appPickerListEl = document.getElementById('appPickerList');
  if (appPickerListEl) {
    appPickerListEl.addEventListener('click', handleAppPickerAction);
  }

  // Source form
  document.getElementById('addSourceForm').addEventListener('submit', addSource);

  // Exit app button
  const btnExit = document.getElementById('btnExitApp');
  if (btnExit) {
    btnExit.addEventListener('click', async () => {
      if (confirm('Are you sure you want to stop and exit Conective?')) {
        try {
          await fetch('/api/system/exit', { method: 'POST' });
          document.body.innerHTML = '<div style="display:flex;height:100vh;align-items:center;justify-content:center;font-family:sans-serif;background:#000;color:#fff;text-align:center;"><div><h2>⚡ Conective has shut down cleanly.</h2><p style="color:#8b949e;margin-top:10px;">All network adapters and routes have been restored. You can close this window.</p></div></div>';
        } catch (e) {
          window.close();
        }
      }
    });
  }

  // Subscriptions export/import
  document.getElementById('btnExportPlain').addEventListener('click', () => exportSubscription('plain'));
  document.getElementById('btnExportBase64').addEventListener('click', () => exportSubscription('base64'));
  document.getElementById('btnImportUrl').addEventListener('click', importFromUrl);
  document.getElementById('btnImportText').addEventListener('click', importFromText);
  document.getElementById('btnClearHistory').addEventListener('click', clearHistory);

  // Trust score buttons
  const btnCheckTrust = document.getElementById('btnCheckAllTrust');
  if (btnCheckTrust) btnCheckTrust.addEventListener('click', checkAllTrustScores);
  const btnRecheck = document.getElementById('btnRecheckIPData');
  if (btnRecheck) btnRecheck.addEventListener('click', () => {
    if (currentInspectedNode) inspectAndCheckIPData(currentInspectedNode.id);
  });

  // TUN Mode Hero toggle
  const chkHeroTun = document.getElementById('chkHeroTunMode');
  if (chkHeroTun) {
    chkHeroTun.addEventListener('change', async (e) => {
      const enabled = e.target.checked;
      if (enabled && activeConnection && !activeConnection.is_admin) {
        showToast('⚠️ TUN Mode requires Administrator privileges! Run Conective as Administrator if connection fails.', 'warning');
      }
      try {
        const res = await fetch('/api/connection/toggle-tun', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ enabled })
        });
        const data = await res.json();
        if (data.error) {
          showToast('TUN Mode error: ' + data.error, 'error');
          e.target.checked = !enabled;
        } else {
          if (data.tun_mode) {
            showToast('🛡️ TUN Mode enabled: All system & application traffic routes through VPN', 'success');
          } else {
            showToast('TUN Mode disabled. Standard proxy mode active.', 'info');
          }
          loadStatus();
        }
      } catch (err) {
        showToast('TUN Mode request failed: ' + err.message, 'error');
        e.target.checked = !enabled;
      }
    });
  }

  // Gaming Mode Hero toggle
  const chkHeroGaming = document.getElementById('chkHeroGamingMode');
  if (chkHeroGaming) {
    chkHeroGaming.addEventListener('change', async (e) => {
      const enabled = e.target.checked;
      try {
        const res = await fetch('/api/connection/toggle-gaming', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ enabled })
        });
        const data = await res.json();
        if (data.error) {
          showToast('Gaming Mode error: ' + data.error, 'error');
          e.target.checked = !enabled;
        } else {
          if (data.gaming_mode) {
            showToast('🎮 Gaming Mode enabled: Low MTU (1400), TCP_NODELAY & UDP acceleration active', 'success');
          } else {
            showToast('Gaming Mode disabled.', 'info');
          }
          loadStatus();
        }
      } catch (err) {
        showToast('Gaming Mode request failed: ' + err.message, 'error');
        e.target.checked = !enabled;
      }
    });
  }

  // LAN Share UI controls in Settings
  const setShareLAN = document.getElementById('setShareLAN');
  if (setShareLAN) {
    setShareLAN.addEventListener('change', (e) => {
      const box = document.getElementById('lanShareInfoBox');
      if (box) {
        box.style.display = e.target.checked ? 'block' : 'none';
        if (e.target.checked) fetchLANInfo();
      }
    });
  }

  const btnRefreshLAN = document.getElementById('btnRefreshLANInfo');
  if (btnRefreshLAN) {
    btnRefreshLAN.addEventListener('click', async () => {
      await fetchLANInfo();
      showToast('LAN info refreshed!', 'info');
    });
  }

  const btnCopyHttp = document.getElementById('btnCopyLanHttp');
  if (btnCopyHttp) {
    btnCopyHttp.addEventListener('click', () => {
      const text = document.getElementById('lanHttpProxyText')?.textContent || '';
      if (text) {
        navigator.clipboard.writeText(text);
        showToast('📋 Copied HTTP proxy URL to clipboard!', 'success');
      }
    });
  }

  const btnCopySocks = document.getElementById('btnCopyLanSocks');
  if (btnCopySocks) {
    btnCopySocks.addEventListener('click', () => {
      const text = document.getElementById('lanSocksProxyText')?.textContent || '';
      if (text) {
        navigator.clipboard.writeText(text);
        showToast('📋 Copied SOCKS5 proxy to clipboard!', 'success');
      }
    });
  }
}

// Real-Time Server-Sent Events (SSE)
function initEventStream() {
  const evtSource = new EventSource('/api/events');

  evtSource.onmessage = (event) => {
    try {
      const data = JSON.parse(event.data);
      handleLiveEvent(data);
    } catch (e) {
      console.error('Failed to parse SSE:', e);
    }
  };

  evtSource.onerror = () => {
    console.warn('SSE connection lost. Reconnecting in 3s...');
  };
}

const throttledLiveUpdate = debounce(() => {
  loadNodes();
  loadStatus();
}, 1500);

function handleLiveEvent(evt) {
  if (evt.progress) {
    updateScanUI(evt.progress);
  }

  switch (evt.type) {
    case 'ScanStarted':
      showToast('Scan started: querying v2go sources...', 'info');
      break;
    case 'ConfigsParsed':
      loadStatus();
      break;
    case 'NodeTested':
      if (evt.node) {
        throttledLiveUpdate();
      }
      break;
    case 'ScanCompleted':
      showToast(evt.message, 'success');
      loadStatus();
      loadNodes();
      break;
    case 'ScanCancelled':
      showToast('Scan was cancelled.', 'info');
      loadStatus();
      break;
  }
}

// Status & Scan State
async function loadStatus() {
  try {
    const res = await fetch('/api/status');
    const data = await res.json();

    updateConnectionUI(data.connection);
    updateScanUI(data.scan);
    updateStatsUI(data.stats);
  } catch (e) {
    console.error('Error loading status:', e);
  }
}

function updateConnectionUI(conn) {
  if (isConnecting) return;
  activeConnection = conn;
  const dot = document.getElementById('statusDot');
  const txt = document.getElementById('statusText');
  const detail = document.getElementById('statusDetail');
  const nodeStats = document.getElementById('activeNodeStats');

  document.getElementById('lblSocksPort').textContent = conn.socks_port;
  document.getElementById('lblHttpPort').textContent = conn.http_port;
  document.getElementById('badgeSysProxy').textContent = `System Proxy: ${conn.system_proxy ? 'ON' : 'OFF'}`;

  // TUN Mode controls & indicators
  const chkHeroTun = document.getElementById('chkHeroTunMode');
  const lblTunToggle = document.getElementById('lblTunToggle');
  const badgeTunStatus = document.getElementById('badgeTunStatus');

  if (chkHeroTun) chkHeroTun.checked = !!conn.tun_mode;
  if (lblTunToggle) {
    if (conn.tun_mode) lblTunToggle.classList.add('is-active');
    else lblTunToggle.classList.remove('is-active');
  }
  if (badgeTunStatus) {
    if (conn.tun_mode && conn.connected) {
      badgeTunStatus.style.display = 'inline-block';
      badgeTunStatus.textContent = 'TUN VPN: ACTIVE';
    } else if (conn.tun_mode) {
      badgeTunStatus.style.display = 'inline-block';
      badgeTunStatus.textContent = 'TUN: READY';
    } else {
      badgeTunStatus.style.display = 'none';
    }
  }

  // Gaming Mode controls & indicators
  const chkHeroGaming = document.getElementById('chkHeroGamingMode');
  const lblGamingToggle = document.getElementById('lblGamingToggle');
  const badgeLanStatus = document.getElementById('badgeLanStatus');

  if (chkHeroGaming) chkHeroGaming.checked = !!conn.gaming_mode;
  if (lblGamingToggle) {
    if (conn.gaming_mode) lblGamingToggle.classList.add('is-active');
    else lblGamingToggle.classList.remove('is-active');
  }
  if (badgeLanStatus) {
    badgeLanStatus.style.display = conn.share_lan ? 'inline-block' : 'none';
  }

  if (conn.connected && conn.active_node) {
    dot.className = 'status-dot connected';
    txt.textContent = 'CONNECTED';
    detail.textContent = conn.tun_mode ?
      `🛡️ Full System VPN (TUN Active) -> ${conn.active_node.name}` :
      `Routing traffic through ${conn.active_node.name}`;

    nodeStats.style.display = 'flex';
    document.getElementById('activeFlag').innerHTML = flagImg(conn.active_node.country);
    document.getElementById('activeCountry').textContent = conn.active_node.country_name || conn.active_node.country;
    document.getElementById('activeProto').textContent = (conn.active_node.protocol || 'VLESS').toUpperCase();
    document.getElementById('activeLatency').textContent = `${conn.latency || conn.active_node.latency} ms`;
    document.getElementById('activeExitIP').textContent = `IP: ${conn.exit_ip || conn.active_node.server}`;

    const activeTrust = document.getElementById('activeTrust');
    if (activeTrust) {
      if (conn.active_node.trust_score >= 0) {
        activeTrust.style.display = 'inline-flex';
        activeTrust.textContent = `🛡️ Trust: ${conn.active_node.trust_score}`;
        if (conn.active_node.trust_score >= 60) {
          activeTrust.className = 'stat-pill trust-low';
        } else if (conn.active_node.trust_score >= 40) {
          activeTrust.className = 'stat-pill trust-mod';
        } else {
          activeTrust.className = 'stat-pill trust-high';
        }
      } else {
        activeTrust.style.display = 'none';
      }
    }

    btnConnect.textContent = 'DISCONNECT';
    btnConnect.className = 'btn btn-connect is-connected';
    btnConnect.disabled = false;
  } else {
    dot.className = 'status-dot disconnected';
    txt.textContent = 'DISCONNECTED';
    detail.textContent = selectedNodeId ? 'Node selected. Click CONNECT.' : 'Select a working node to connect';
    nodeStats.style.display = 'none';

    const activeTrust = document.getElementById('activeTrust');
    if (activeTrust) activeTrust.style.display = 'none';

    btnConnect.textContent = 'CONNECT';
    btnConnect.className = 'btn btn-connect';
    btnConnect.disabled = !selectedNodeId;
  }
}

function updateScanUI(scan) {
  if (!scan) return;

  const isScanning = (scan.state === 'starting' || scan.state === 'fetching' || scan.state === 'parsing' || scan.state === 'testing');
  isScanningOrPinging = isScanning;

  if (isScanning) {
    scanBanner.style.display = 'block';
    btnScan.disabled = true;
    if (btnPingAll) {
      btnPingAll.classList.add('btn-danger');
      btnPingAll.classList.remove('btn-primary');
      btnPingAll.innerHTML = '<span class="btn-icon">🛑</span> Cancel Ping';
    }
    document.getElementById('scanStateText').textContent = `Scanning: ${scan.state.toUpperCase()}`;
    document.getElementById('scanMessageText').textContent = scan.message;

    document.getElementById('scanSources').textContent = `${scan.sources_done} / ${scan.sources_total}`;
    document.getElementById('scanFetched').textContent = scan.fetched_total.toLocaleString();
    document.getElementById('scanParsed').textContent = scan.parsed_total.toLocaleString();
    document.getElementById('scanDuplicates').textContent = scan.duplicates.toLocaleString();
    document.getElementById('scanTesting').textContent = `${scan.testing_done.toLocaleString()} / ${scan.testing_total.toLocaleString()}`;
    document.getElementById('scanWorking').textContent = scan.working_count.toLocaleString();
    document.getElementById('scanAvgLat').textContent = `${scan.average_latency}ms`;

    const pct = scan.testing_total > 0 ? (scan.testing_done / scan.testing_total) * 100 : 10;
    document.getElementById('scanProgressBar').style.width = `${Math.min(100, pct)}%`;
  } else {
    btnScan.disabled = false;
    if (btnPingAll) {
      btnPingAll.classList.remove('btn-danger');
      btnPingAll.classList.add('btn-primary');
      btnPingAll.innerHTML = '<span class="btn-icon">⚡</span> Ping All';
    }
    if (scan.state === 'completed' || scan.state === 'cancelled') {
      setTimeout(() => {
        if (!btnScan.disabled) scanBanner.style.display = 'none';
      }, 5000);
    } else {
      scanBanner.style.display = 'none';
    }
  }
}

function updateStatsUI(stats) {
  if (!stats) return;
  document.getElementById('statWorkingCount').textContent = stats.working_configs.toLocaleString();
  document.getElementById('statTotalCount').textContent = stats.total_configs.toLocaleString();
  document.getElementById('statAvgLatency').textContent = `${stats.avg_latency} ms`;
  document.getElementById('statActiveSources').textContent = `${stats.active_sources} / ${stats.total_sources}`;
  document.getElementById('activeSourcesCount').textContent = stats.active_sources;

  const favBadge = document.getElementById('favCountBadge');
  if (favBadge && stats.favorite_configs !== undefined) {
    favBadge.textContent = stats.favorite_configs;
    favBadge.style.display = stats.favorite_configs > 0 ? 'inline-block' : 'none';
  }

  // Populate countries filter if needed
  if (stats.by_country && Object.keys(stats.by_country).length > 0) {
    const curVal = filterCountry.value;
    const existing = Array.from(filterCountry.options).map(o => o.value);
    Object.keys(stats.by_country).forEach(code => {
      if (!existing.includes(code) && code !== 'UN') {
        const opt = document.createElement('option');
        opt.value = code;
        opt.textContent = `${getFlagEmoji(code)} ${code} (${stats.by_country[code]})`;
        filterCountry.appendChild(opt);
      }
    });
    filterCountry.value = curVal;
  }
}

function renderTagsHTML(tags) {
  if (!tags) return '';
  const parts = tags.split(',').map(t => t.trim()).filter(Boolean);
  if (parts.length === 0) return '';
  return `<div class="node-tags-row">` +
    parts.map(t => `<span class="node-tag-pill" title="Filter by tag: ${escapeHTML(t)}">${escapeHTML(t)}</span>`).join('') +
    `</div>`;
}

// Nodes List
async function loadNodes() {
  const isFav = isFavoritesFilterActive || filterStatus.value === 'favorites';
  const params = {
    status: isFav ? '' : filterStatus.value,
    protocol: filterProto.value,
    country: filterCountry.value,
    sort_by: sortBy.value === 'trust' ? 'trust' : sortBy.value,
    sort_order: (sortBy.value === 'last_tested' || sortBy.value === 'trust') ? 'DESC' : 'ASC',
    search: searchInput.value.trim(),
    limit: pageSize,
    offset: (currentPage - 1) * pageSize
  };
  if (isFav) {
    params.is_favorite = 'true';
  }
  const query = new URLSearchParams(params);

  try {
    const res = await fetch(`/api/nodes?${query}`);
    const data = await res.json();
    renderNodesTable(data.items || [], data.total || 0);
  } catch (e) {
    console.error('Error fetching nodes:', e);
  }
}

function renderNodesTable(nodes, total) {
  nodesTableBody.innerHTML = '';

  const start = (currentPage - 1) * pageSize + 1;
  const end = Math.min(currentPage * pageSize, total);
  document.getElementById('pageInfo').textContent = total > 0 ? `Showing ${start}-${end} of ${total} nodes` : 'Showing 0 nodes';
  document.getElementById('btnPrevPage').disabled = currentPage <= 1;
  document.getElementById('btnNextPage').disabled = end >= total;

  if (nodes.length === 0) {
    nodesTableBody.innerHTML = `
      <tr>
        <td colspan="8" class="text-center empty-state">
          No matching nodes found. Try changing filters or click <b>Scan Free Nodes</b>!
        </td>
      </tr>`;
    return;
  }

  nodes.forEach((n, idx) => {
    const tr = document.createElement('tr');
    if (selectedNodeId === n.id) tr.classList.add('active-row');

    const flag = getFlagEmoji(n.country);
    const pingClass = n.latency > 0 ? (n.latency < 120 ? 'ping-fast' : (n.latency < 300 ? 'ping-medium' : 'ping-slow')) : 'ping-dead';
    const pingText = n.latency > 0 ? `${n.latency}ms` : 'Dead / Untested';

    const protoClass = `proto-${n.protocol.toLowerCase()}`;

    let trustBadgeHTML = '';
    if (n.trust_score >= 0) {
      const tClass = n.trust_score >= 60 ? 'trust-low' : (n.trust_score >= 40 ? 'trust-mod' : 'trust-high');
      const rLabel = n.risk_level || (n.trust_score >= 60 ? 'Low risk' : (n.trust_score >= 40 ? 'Moderate risk' : 'High risk'));
      trustBadgeHTML = `<span class="trust-badge ${tClass} btn-open-ipdata" data-id="${n.id}" title="Threats: ${n.threats_count} | Org: ${escapeHTML(n.organisation || '')}">🛡️ ${n.trust_score} – ${rLabel}</span>`;
    } else {
      trustBadgeHTML = `<button class="btn btn-sm btn-outline-secondary btn-check-trust" data-id="${n.id}" title="Check trust score on ipdata.co">🛡️ Check</button>`;
    }

    tr.innerHTML = `
      <td>
        <div style="display:flex; align-items:center; gap:5px;">
          <button type="button" class="btn-fav ${n.is_favorite ? 'is-fav' : ''}" data-id="${n.id}" title="${n.is_favorite ? 'Remove Favorite' : 'Add to Favorites'}">${n.is_favorite ? '⭐' : '☆'}</button>
          <span>${start + idx}</span>
        </div>
      </td>
      <td>${flagImg(n.country)}<b>${(n.country === 'CF' || n.country === 'CLOUDFLARE') ? 'CLOUDFLARE' : n.country}</b> <span class="text-muted" style="font-size:11px;">${n.country_name || ''}</span></td>
      <td><span class="proto-tag ${protoClass}">${n.protocol}</span></td>
      <td>
        <div style="font-weight:600;">${escapeHTML(stripFlagEmoji(n.name))}</div>
        <div class="text-muted" style="font-size:11px;">${escapeHTML(n.server)}:${n.port}</div>
        ${renderTagsHTML(n.tags)}
      </td>
      <td>
        <div>${n.transport} / ${n.tls}</div>
        ${n.sni ? `<div class="text-muted" style="font-size:11px;">SNI: ${escapeHTML(n.sni)}</div>` : ''}
      </td>
      <td class="${pingClass}">${pingText}</td>
      <td>${trustBadgeHTML}</td>
      <td>
        <div class="table-actions">
          <button class="btn btn-sm btn-primary btn-select" data-id="${n.id}">Select</button>
          <button class="btn btn-sm btn-secondary btn-open-ipdata" data-id="${n.id}" title="View ipdata.co Intelligence">🛡️</button>
          <button class="btn btn-sm btn-secondary btn-test" data-id="${n.id}" title="Test Ping">⚡</button>
          <button class="btn btn-sm btn-secondary btn-copy" data-link="${escapeHTML(n.raw_link)}" title="Copy Link">📋</button>
        </div>
      </td>
    `;

    // Row selection & actions
    tr.querySelector('.btn-select').addEventListener('click', () => selectNode(n));
    tr.querySelector('.btn-test').addEventListener('click', (e) => {
      e.stopPropagation();
      testSingleNode(n.id);
    });
    tr.querySelector('.btn-copy').addEventListener('click', (e) => {
      e.stopPropagation();
      copyToClipboard(n.raw_link);
      showToast('Copied node share link to clipboard', 'success');
    });

    const favBtn = tr.querySelector('.btn-fav');
    if (favBtn) {
      favBtn.addEventListener('click', async (e) => {
        e.stopPropagation();
        try {
          const res = await fetch('/api/nodes/favorite', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ id: n.id })
          });
          const data = await res.json();
          if (data && data.success) {
            n.is_favorite = data.is_favorite;
            favBtn.classList.toggle('is-fav', n.is_favorite);
            favBtn.textContent = n.is_favorite ? '⭐' : '☆';
            favBtn.title = n.is_favorite ? 'Remove Favorite' : 'Add to Favorites';
            showToast(n.is_favorite ? 'Added to Favorites ⭐' : 'Removed from Favorites', 'info');
            loadStatus();
            if ((isFavoritesFilterActive || filterStatus.value === 'favorites') && !n.is_favorite) {
              tr.style.opacity = '0.35';
            }
          }
        } catch (err) {
          console.error('Failed toggling favorite:', err);
          showToast('Failed to toggle favorite', 'error');
        }
      });
    }

    tr.querySelectorAll('.node-tag-pill').forEach(pill => {
      pill.addEventListener('click', (e) => {
        e.stopPropagation();
        searchInput.value = pill.textContent.trim();
        currentPage = 1;
        loadNodes();
      });
    });

    tr.querySelectorAll('.btn-open-ipdata').forEach(btn => {
      btn.addEventListener('click', (e) => {
        e.stopPropagation();
        openIPDataModal(n);
      });
    });

    const checkBtn = tr.querySelector('.btn-check-trust');
    if (checkBtn) {
      checkBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        openIPDataModal(n);
      });
    }

    nodesTableBody.appendChild(tr);
  });
}

function selectNode(node) {
  selectedNodeId = node.id;
  document.querySelectorAll('.nodes-table tbody tr').forEach(r => r.classList.remove('active-row'));
  btnConnect.disabled = false;
  btnConnect.textContent = 'CONNECT';
  showToast(`Selected node: ${node.name}`, 'info');
  loadNodes();
}

async function testSingleNode(id) {
  showToast('Testing node latency...', 'info');
  try {
    const res = await fetch(`/api/nodes/test?id=${id}`, { method: 'POST' });
    const updated = await res.json();
    if (updated.latency > 0) {
      showToast(`Node alive: ${updated.latency}ms`, 'success');
    } else {
      showToast('Node failed to connect', 'error');
    }
    loadNodes();
    loadStatus();
  } catch (e) {
    showToast('Failed to test node', 'error');
  }
}

// Connect / Disconnect
let connProgressTimer1 = null;
let connProgressTimer2 = null;

function startConnectProgress() {
  const track = document.getElementById('connProgressTrack');
  const bar = document.getElementById('connProgressBar');
  if (!track || !bar) return;

  clearTimeout(connProgressTimer1);
  clearTimeout(connProgressTimer2);

  // Reset to 0% immediately
  bar.style.transition = 'none';
  bar.style.width = '0%';
  bar.style.backgroundColor = '#ffffff';
  bar.style.boxShadow = '0 0 10px rgba(255, 255, 255, 0.9), 0 0 20px rgba(255, 255, 255, 0.5)';
  track.classList.add('is-active');

  // Force reflow
  void bar.offsetWidth;

  // Step 1: Smoothly animate to 50%
  bar.style.transition = 'width 0.65s cubic-bezier(0.2, 0.8, 0.25, 1)';
  bar.style.width = '50%';

  // Step 2: A bit later, smoothly crawl to 80%
  connProgressTimer1 = setTimeout(() => {
    bar.style.transition = 'width 1.2s cubic-bezier(0.1, 0.7, 0.1, 1)';
    bar.style.width = '80%';
  }, 650);
}

function finishConnectProgress(success = true) {
  const track = document.getElementById('connProgressTrack');
  const bar = document.getElementById('connProgressBar');
  if (!track || !bar) return;

  clearTimeout(connProgressTimer1);
  clearTimeout(connProgressTimer2);

  if (success) {
    // Step 3: Fast finish to 100% on successful connection
    bar.style.transition = 'width 0.35s ease-out';
    bar.style.width = '100%';

    // Smoothly fade out track after reaching 100%
    connProgressTimer2 = setTimeout(() => {
      track.classList.remove('is-active');
      setTimeout(() => {
        bar.style.width = '0%';
      }, 350);
    }, 450);
  } else {
    // Error state: turn red and fade out
    bar.style.transition = 'width 0.3s ease-out';
    bar.style.backgroundColor = '#ef4444';
    bar.style.boxShadow = '0 0 10px rgba(239, 68, 68, 0.9)';
    setTimeout(() => {
      track.classList.remove('is-active');
      setTimeout(() => {
        bar.style.width = '0%';
        bar.style.backgroundColor = '#ffffff';
      }, 350);
    }, 500);
  }
}

async function toggleConnection() {
  if (activeConnection && activeConnection.connected) {
    btnConnect.disabled = true;
    try {
      await fetch('/api/nodes/disconnect', { method: 'POST' });
      showToast('Disconnected from proxy.', 'info');
      loadStatus();
    } catch (e) {
      showToast('Error disconnecting: ' + e.message, 'error');
    }
    btnConnect.disabled = false;
  } else if (selectedNodeId) {
    isConnecting = true;
    btnConnect.disabled = true;
    btnConnect.textContent = 'CONNECTING...';

    const dot = document.getElementById('statusDot');
    const txt = document.getElementById('statusText');
    const detail = document.getElementById('statusDetail');
    if (dot) dot.className = 'status-dot connecting';
    if (txt) txt.textContent = 'CONNECTING...';
    if (detail) detail.textContent = 'Establishing secure tunnel and verifying connection...';

    startConnectProgress();

    try {
      const res = await fetch(`/api/nodes/connect?id=${selectedNodeId}`, { method: 'POST' });
      const data = await res.json();
      if (data.error) {
        finishConnectProgress(false);
        showToast('Connection failed: ' + data.error, 'error');
      } else {
        finishConnectProgress(true);
        showToast('Connected successfully!', 'success');
      }
    } catch (e) {
      finishConnectProgress(false);
      showToast('Connection failed: ' + e.message, 'error');
    } finally {
      isConnecting = false;
      await loadStatus();
      btnConnect.disabled = false;
    }
  }
}

// Scan Actions
async function startScan() {
  try {
    const res = await fetch('/api/scan/start', { method: 'POST' });
    const data = await res.json();
    if (data.error) {
      showToast(data.error, 'error');
    } else {
      showToast('Starting embedded v2go scan...', 'info');
      scanBanner.style.display = 'block';
    }
  } catch (e) {
    showToast('Failed to trigger scan: ' + e.message, 'error');
  }
}

async function cancelScan() {
  try {
    await fetch('/api/scan/cancel', { method: 'POST' });
    showToast('Cancelling scan...', 'info');
  } catch (e) {
    console.error(e);
  }
}

async function triggerPingAll() {
  if (isScanningOrPinging) {
    try {
      await fetch('/api/nodes/ping-cancel', { method: 'POST' });
      showToast('Cancelling ping test...', 'info');
    } catch (e) {
      console.error(e);
    }
    return;
  }

  const targetStatus = filterStatus.value;
  showToast(`Starting concurrent ping test on "${targetStatus}" nodes...`, 'info');
  try {
    const res = await fetch(`/api/nodes/ping-all?status=${encodeURIComponent(targetStatus)}`, {
      method: 'POST'
    });
    const data = await res.json();
    if (data.error) {
      showToast(data.error, 'error');
    } else {
      showToast('⚡ Concurrent ping test started!', 'success');
      scanBanner.style.display = 'block';
    }
  } catch (err) {
    showToast('Failed to start ping test: ' + err.message, 'error');
  }
}

async function clearAllNodes() {
  if (!confirm('Are you sure you want to clear all configurations from the database?')) return;
  try {
    await fetch('/api/nodes/clear', { method: 'POST' });
    showToast('Database cleared.', 'info');
    selectedNodeId = null;
    loadNodes();
    loadStatus();
  } catch (e) {
    showToast('Failed to clear nodes: ' + e.message, 'error');
  }
}

// Sources Manager
async function loadSources() {
  try {
    const res = await fetch('/api/sources');
    const sources = await res.json();
    const active = sources.filter(s => s.Enabled).length;
    document.getElementById('activeSourcesCount').textContent = active;
  } catch (e) {
    console.error(e);
  }
}

async function loadSourcesTable() {
  const res = await fetch('/api/sources');
  const sources = await res.json();
  const tbody = document.getElementById('sourcesTableBody');
  tbody.innerHTML = '';

  sources.forEach(s => {
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td><input type="checkbox" class="toggle-src" data-id="${s.id}" ${s.enabled ? 'checked' : ''}></td>
      <td><b>${escapeHTML(s.name)}</b></td>
      <td><span class="text-muted" style="font-size:11px;">${escapeHTML(s.url.substring(0, 45))}...</span></td>
      <td>${s.format}</td>
      <td>${s.configs_found}</td>
      <td>${s.error ? `<span class="text-danger" title="${escapeHTML(s.error)}">Error</span>` : '<span class="text-green">OK</span>'}</td>
      <td><button class="btn btn-outline-danger btn-sm btn-del-src" data-id="${s.id}">Delete</button></td>
    `;

    tr.querySelector('.toggle-src').addEventListener('change', async (e) => {
      s.enabled = e.target.checked;
      await fetch('/api/sources/save', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(s)
      });
      loadSources();
    });

    tr.querySelector('.btn-del-src').addEventListener('click', async () => {
      if (confirm(`Delete source "${s.name}"?`)) {
        await fetch(`/api/sources/delete?id=${s.id}`, { method: 'POST' });
        loadSourcesTable();
        loadSources();
      }
    });

    tbody.appendChild(tr);
  });
}

async function addSource(e) {
  e.preventDefault();
  const name = document.getElementById('srcName').value.trim();
  const url = document.getElementById('srcURL').value.trim();
  const format = document.getElementById('srcFormat').value;

  try {
    const res = await fetch('/api/sources/save', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, url, format, enabled: true })
    });
    const data = await res.json();
    if (data.error) {
      showToast(data.error, 'error');
    } else {
      showToast(`Source "${name}" added!`, 'success');
      document.getElementById('srcName').value = '';
      document.getElementById('srcURL').value = '';
      loadSourcesTable();
      loadSources();
    }
  } catch (err) {
    showToast(err.message, 'error');
  }
}

// Settings
async function loadSettings() {
  try {
    const res = await fetch('/api/settings');
    const s = await res.json();
    if (document.getElementById('setConcurrency') && s.test_concurrency !== undefined) {
      document.getElementById('setConcurrency').value = s.test_concurrency;
    }
    if (document.getElementById('setTimeout') && s.test_timeout_sec !== undefined) {
      document.getElementById('setTimeout').value = s.test_timeout_sec;
    }
    if (document.getElementById('setEndpoint') && s.test_endpoint !== undefined) {
      document.getElementById('setEndpoint').value = s.test_endpoint;
    }
    if (document.getElementById('setSocksPort') && s.socks_port !== undefined) {
      document.getElementById('setSocksPort').value = s.socks_port;
    }
    if (document.getElementById('setHttpPort') && s.http_port !== undefined) {
      document.getElementById('setHttpPort').value = s.http_port;
    }
    const setTun = document.getElementById('setTunMode');
    if (setTun) setTun.checked = !!s.tun_mode;
    const setGaming = document.getElementById('setGamingMode');
    if (setGaming) setGaming.checked = !!s.gaming_mode;
    const setLAN = document.getElementById('setShareLAN');
    if (setLAN) {
      setLAN.checked = !!s.share_lan;
      const box = document.getElementById('lanShareInfoBox');
      if (box) {
        box.style.display = s.share_lan ? 'block' : 'none';
        if (s.share_lan) fetchLANInfo();
      }
    }
    if (document.getElementById('setSystemProxy') && s.system_proxy !== undefined) {
      document.getElementById('setSystemProxy').checked = s.system_proxy;
    }
    if (document.getElementById('setAutoFailover') && s.auto_failover !== undefined) {
      document.getElementById('setAutoFailover').checked = s.auto_failover;
    }
    if (document.getElementById('setAutoScan') && s.auto_scan !== undefined) {
      document.getElementById('setAutoScan').checked = s.auto_scan;
    }
    if (document.getElementById('setAutoScanInterval') && s.auto_scan_interval !== undefined) {
      document.getElementById('setAutoScanInterval').value = s.auto_scan_interval;
    }
    const setMinTray = document.getElementById('setMinimizeToTray');
    if (setMinTray && s.minimize_to_tray !== undefined) {
      setMinTray.checked = s.minimize_to_tray !== false;
    }
    const setStartWin = document.getElementById('setStartWithWindows');
    if (setStartWin && s.start_with_windows !== undefined) {
      setStartWin.checked = !!s.start_with_windows;
    }
    const updateRepoInput = document.getElementById('setUpdateRepo');
    if (updateRepoInput && s.update_repo) {
      updateRepoInput.value = s.update_repo;
    }
  } catch (err) {
    console.warn('Failed to load settings:', err);
  }
}

async function fetchLANInfo() {
  try {
    const res = await fetch('/api/system/lan-info');
    if (!res.ok) return;
    const info = await res.json();
    const ipsElem = document.getElementById('lanLocalIPsText');
    const httpElem = document.getElementById('lanHttpProxyText');
    const socksElem = document.getElementById('lanSocksProxyText');
    const ips = (info.local_ips && info.local_ips.length > 0) ? info.local_ips : ['127.0.0.1'];
    const primaryIP = ips[0];
    const httpPort = info.http_port || 10809;
    const socksPort = info.socks_port || 10808;

    if (ipsElem) ipsElem.textContent = ips.join(', ');
    if (httpElem) httpElem.textContent = `http://${primaryIP}:${httpPort}`;
    if (socksElem) socksElem.textContent = `${primaryIP}:${socksPort}`;
  } catch (err) {
    console.warn('Failed to load LAN info:', err);
  }
}

async function saveSettings(e) {
  e.preventDefault();
  const body = {
    test_concurrency: parseInt(document.getElementById('setConcurrency').value),
    test_timeout_sec: parseInt(document.getElementById('setTimeout').value),
    test_endpoint: document.getElementById('setEndpoint').value.trim(),
    socks_port: parseInt(document.getElementById('setSocksPort').value),
    http_port: parseInt(document.getElementById('setHttpPort').value),
    tun_mode: document.getElementById('setTunMode') ? document.getElementById('setTunMode').checked : false,
    gaming_mode: document.getElementById('setGamingMode') ? document.getElementById('setGamingMode').checked : false,
    share_lan: document.getElementById('setShareLAN') ? document.getElementById('setShareLAN').checked : false,
    system_proxy: document.getElementById('setSystemProxy').checked,
    auto_failover: document.getElementById('setAutoFailover').checked,
    auto_scan: document.getElementById('setAutoScan').checked,
    auto_scan_interval: parseInt(document.getElementById('setAutoScanInterval').value),
    minimize_to_tray: document.getElementById('setMinimizeToTray') ? document.getElementById('setMinimizeToTray').checked : true,
    start_with_windows: document.getElementById('setStartWithWindows') ? document.getElementById('setStartWithWindows').checked : false,
  };

  const updateRepoInput = document.getElementById('setUpdateRepo');
  if (updateRepoInput && updateRepoInput.value.trim()) {
    body.update_repo = updateRepoInput.value.trim();
  }

  try {
    localStorage.setItem('setConcurrency', document.getElementById('setConcurrency').value);
    localStorage.setItem('setTimeout', document.getElementById('setTimeout').value);
    localStorage.setItem('setEndpoint', document.getElementById('setEndpoint').value.trim());
    localStorage.setItem('setAutoScan', document.getElementById('setAutoScan').checked ? 'true' : 'false');
    localStorage.setItem('setAutoScanInterval', document.getElementById('setAutoScanInterval').value);
  } catch (e) {}

  try {
    await fetch('/api/settings', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    });
    showToast('Settings saved successfully.', 'success');
    closeModal('settingsModal');
    loadStatus();
  } catch (err) {
    showToast('Failed to save settings: ' + err.message, 'error');
  }
}

// ==========================================
// Feature 47: In-App Auto-Updater
// ==========================================

async function checkForUpdates() {
  const btn = document.getElementById('btnCheckUpdate');
  const badge = document.getElementById('updateStatusBadge');
  const previewBox = document.getElementById('updatePreviewBox');
  const updateTitle = document.getElementById('updateTitle');
  const updateTag = document.getElementById('updateTag');
  const updateNotes = document.getElementById('updateNotes');
  const btnReleaseLink = document.getElementById('btnReleaseLink');
  const btnApply = document.getElementById('btnApplyUpdate');

  if (btn) {
    btn.disabled = true;
    btn.innerHTML = '<span class="spinner" style="display:inline-block;width:12px;height:12px;margin-right:4px;border:2px solid #fff;border-top-color:transparent;border-radius:50%;animation:conn-spin .8s linear infinite;"></span> Checking...';
  }
  if (badge) badge.style.display = 'none';

  try {
    const res = await fetch('/api/system/check-update');
    const data = await res.json();

    if (btn) {
      btn.disabled = false;
      btn.innerHTML = '<span class="btn-icon">🔄</span> Check for Updates';
    }

    if (data.current_version) {
      const curElem = document.getElementById('lblCurrentVersion');
      if (curElem) curElem.textContent = data.current_version;
    }

    if (data.update_available) {
      if (badge) {
        badge.style.display = 'inline-block';
        badge.className = 'status-badge badge-update-avail';
        badge.textContent = `Update Available: ${data.latest_version}`;
      }
      if (previewBox) previewBox.style.display = 'block';
      if (updateTitle) updateTitle.textContent = `🎉 New version ${data.latest_version} available!`;
      if (updateTag) updateTag.textContent = data.published_at ? new Date(data.published_at).toLocaleDateString() : '';
      if (updateNotes) updateNotes.textContent = data.release_notes || 'No release notes provided.';
      latestDownloadURL = data.download_url || '';

      if (btnReleaseLink && data.download_url && data.download_url.startsWith('http')) {
        btnReleaseLink.href = data.download_url;
        btnReleaseLink.style.display = 'inline-block';
      }
      if (btnApply) btnApply.disabled = false;
      showToast(`New update ${data.latest_version} is available!`, 'info');
    } else {
      if (badge) {
        badge.style.display = 'inline-block';
        badge.className = 'status-badge badge-up-to-date';
        badge.textContent = `Up to Date (${data.current_version || 'v1.0.0'})`;
      }
      if (previewBox) previewBox.style.display = 'none';
      showToast('You are running the latest version.', 'success');
    }
  } catch (err) {
    if (btn) {
      btn.disabled = false;
      btn.innerHTML = '<span class="btn-icon">🔄</span> Check for Updates';
    }
    if (badge) {
      badge.style.display = 'inline-block';
      badge.className = 'status-badge badge-update-err';
      badge.textContent = 'Check Failed';
    }
    showToast('Failed to check for updates: ' + err.message, 'error');
  }
}

async function applyUpdate() {
  const btnApply = document.getElementById('btnApplyUpdate');
  if (btnApply) {
    btnApply.disabled = true;
    btnApply.innerHTML = '<span class="spinner" style="display:inline-block;width:12px;height:12px;margin-right:4px;border:2px solid #fff;border-top-color:transparent;border-radius:50%;animation:conn-spin .8s linear infinite;"></span> Launching Installer...';
  }

  try {
    const res = await fetch('/api/system/apply-update', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ download_url: latestDownloadURL })
    });
    const data = await res.json();
    if (btnApply) {
      btnApply.disabled = false;
      btnApply.innerHTML = '🚀 Install / Download Update';
    }
    if (data.path || data.message) {
      showToast(data.message || data.path || 'Update launched successfully!', 'success');
    }
  } catch (err) {
    if (btnApply) {
      btnApply.disabled = false;
      btnApply.innerHTML = '🚀 Install / Download Update';
    }
    showToast('Failed to apply update: ' + err.message, 'error');
  }
}


// Routing & Split Tunneling
function switchRoutingMode(mode) {
  if (!['proxy_all', 'blacklist', 'whitelist'].includes(mode)) {
    mode = 'blacklist';
  }

  // Update radios
  const radio = document.querySelector(`input[name="routeModeRadio"][value="${mode}"]`);
  if (radio) radio.checked = true;

  // Update card active classes
  document.querySelectorAll('.routing-mode-cards .mode-card').forEach(card => card.classList.remove('is-active'));
  if (mode === 'proxy_all') {
    document.getElementById('modeCard1')?.classList.add('is-active');
  } else if (mode === 'blacklist') {
    document.getElementById('modeCard2')?.classList.add('is-active');
  } else if (mode === 'whitelist') {
    document.getElementById('modeCard3')?.classList.add('is-active');
  }

  // Toggle sections
  const sec1 = document.getElementById('sectionMode1');
  const sec2 = document.getElementById('sectionMode2');
  const sec3 = document.getElementById('sectionMode3');

  if (sec1) sec1.style.display = (mode === 'proxy_all') ? 'flex' : 'none';
  if (sec2) sec2.style.display = (mode === 'blacklist') ? 'flex' : 'none';
  if (sec3) sec3.style.display = (mode === 'whitelist') ? 'flex' : 'none';
}

async function loadRoutingSettings() {
  try {
    const res = await fetch('/api/settings');
    const s = await res.json();
    let mode = s.routing_mode || 'blacklist';
    if (mode === 'bypass_iran' || mode === 'custom') mode = 'blacklist';
    else if (mode === 'global') mode = 'proxy_all';
    else if (mode === 'proxy_apps_only') mode = 'whitelist';

    switchRoutingMode(mode);

    document.getElementById('routeBlacklistApps').value = s.direct_apps || '';
    document.getElementById('routeBlacklistDomains').value = s.direct_domains || '';
    document.getElementById('routeWhitelistApps').value = s.proxy_apps || '';
    document.getElementById('routeWhitelistDomains').value = s.proxy_domains || '';
  } catch (e) {
    console.error('Failed to load routing settings:', e);
  }
}

async function saveRoutingSettings(e) {
  e.preventDefault();
  const selectedRadio = document.querySelector('input[name="routeModeRadio"]:checked');
  const mode = selectedRadio ? selectedRadio.value : 'blacklist';

  const body = {
    routing_mode: mode,
    direct_apps: document.getElementById('routeBlacklistApps').value,
    direct_domains: document.getElementById('routeBlacklistDomains').value,
    proxy_apps: document.getElementById('routeWhitelistApps').value,
    proxy_domains: document.getElementById('routeWhitelistDomains').value,
    block_domains: '',
  };

  try {
    const res = await fetch('/api/settings', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    });
    const data = await res.json();
    if (data.error) {
      showToast('Error saving routing: ' + data.error, 'error');
    } else {
      let modeText = '۲. بلک‌لیست (Bypass)';
      if (mode === 'proxy_all') modeText = '۱. عبور همه ترافیک (Proxy All)';
      if (mode === 'whitelist') modeText = '۳. وایت‌لیست (Proxy Only)';
      showToast(`🔀 حالت مسیریابی به «${modeText}» تغییر یافت و اعمال شد.`, 'success');
      closeModal('routingModal');
      loadStatus();
    }
  } catch (err) {
    showToast('Failed to save routing settings: ' + err.message, 'error');
  }
}

function resetBlacklistDefaults() {
  document.getElementById('routeBlacklistApps').value = [
    'cs2.exe',
    'valorant.exe',
    'dota2.exe',
    'leagueclient.exe',
    'idman.exe'
  ].join('\n');
  document.getElementById('routeBlacklistDomains').value = [
    'regexp:.*\\.ir$',
    'shaparak.ir',
    'digikala.com',
    'divar.ir',
    'snapp.ir',
    'torob.com',
    'varzesh3.com',
    'telewebion.com',
    'bale.ai',
    'eitaa.com',
    'rubika.ir',
    'aparat.com',
    'filimo.com'
  ].join('\n');
  showToast('تنظیمات پیش‌فرض بلک‌لیست (سایت‌های ایرانی و بازی‌ها) بارگذاری شد.', 'info');
}

function resetWhitelistDefaults() {
  document.getElementById('routeWhitelistApps').value = [
    'telegram.exe',
    'discord.exe',
    'chrome.exe',
    'msedge.exe',
    'firefox.exe',
    'spotify.exe'
  ].join('\n');
  document.getElementById('routeWhitelistDomains').value = [
    'google.com',
    'youtube.com',
    'twitter.com',
    'x.com',
    't.me',
    'telegram.org',
    'instagram.com',
    'facebook.com',
    'discord.com'
  ].join('\n');
  showToast('تنظیمات پیش‌فرض وایت‌لیست (برنامه‌ها و دامنه‌های خارجی) بارگذاری شد.', 'info');
}

// Subscriptions
async function exportSubscription(format) {
  const res = await fetch(`/api/sub/export?format=${format}`);
  const text = await res.text();
  copyToClipboard(text);
  showToast(`Copied ${format.toUpperCase()} subscription to clipboard!`, 'success');
}

async function importFromUrl() {
  const url = document.getElementById('importUrl').value.trim();
  if (!url) return;
  showToast('Fetching and importing subscription...', 'info');
  try {
    const res = await fetch('/api/sub/import', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url })
    });
    const data = await res.json();
    if (data.error) showToast(data.error, 'error');
    else {
      showToast(data.message, 'success');
      closeModal('subModal');
      isFavoritesFilterActive = false;
      if (tabFavorites) tabFavorites.classList.remove('active');
      filterStatus.value = 'all'; // Switch to 'All Nodes' so imported nodes show up immediately!
      currentPage = 1;
      loadNodes();
      loadStatus();
    }
  } catch (e) {
    showToast(e.message, 'error');
  }
}

async function importFromText() {
  const content = document.getElementById('importText').value.trim();
  if (!content) return;
  try {
    const res = await fetch('/api/sub/import', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ content })
    });
    const data = await res.json();
    if (data.error) showToast(data.error, 'error');
    else {
      showToast(data.message, 'success');
      closeModal('subModal');
      document.getElementById('importText').value = '';
      isFavoritesFilterActive = false;
      if (tabFavorites) tabFavorites.classList.remove('active');
      filterStatus.value = 'all'; // Switch to 'All Nodes' so imported nodes show up immediately!
      currentPage = 1;
      loadNodes();
      loadStatus();
    }
  } catch (e) {
    showToast(e.message, 'error');
  }
}

// History
async function loadHistory() {
  const res = await fetch('/api/history');
  const sessions = await res.json();
  const tbody = document.getElementById('historyTableBody');
  tbody.innerHTML = '';

  if (!sessions || sessions.length === 0) {
    tbody.innerHTML = '<tr><td colspan="7" class="text-center empty-state">No scan sessions recorded.</td></tr>';
    return;
  }

  sessions.forEach(s => {
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td>${s.started_at ? s.started_at.replace('T', ' ').substring(0, 19) : ''}</td>
      <td><span class="${s.status === 'completed' ? 'text-green' : 'text-muted'}">${s.status}</span></td>
      <td>${s.fetched_count.toLocaleString()}</td>
      <td>${s.parsed_count.toLocaleString()}</td>
      <td>${s.dedup_count.toLocaleString()}</td>
      <td>${s.tested_count.toLocaleString()}</td>
      <td><b class="text-green">${s.working_count.toLocaleString()}</b></td>
    `;
    tbody.appendChild(tr);
  });
}

async function clearHistory() {
  if (confirm('Clear all scan session history?')) {
    await fetch('/api/history/clear', { method: 'POST' });
    showToast('Scan history cleared.', 'info');
    loadHistory();
  }
}

// Modals Helper
function openModal(id, onOpen) {
  document.getElementById(id).classList.add('open');
  if (onOpen) onOpen();
}

function closeModal(id) {
  document.getElementById(id).classList.remove('open');
}

// Toast
function showToast(msg, type = 'info') {
  const container = document.getElementById('toastContainer');
  const t = document.createElement('div');
  t.className = `toast toast-${type}`;
  t.textContent = msg;
  container.appendChild(t);
  setTimeout(() => t.remove(), 4000);
}

// Helpers
function getFlagEmoji(countryCode) {
  if (!countryCode) return '🌐';
  const code = countryCode.toUpperCase();
  if (code === 'CF' || code === 'CLOUDFLARE' || code === 'WW' || code === 'GLOBAL') return '☁️';
  if (countryCode.length !== 2) return '🌐';
  return String.fromCodePoint(code.charCodeAt(0) + 127397) + String.fromCodePoint(code.charCodeAt(1) + 127397);
}

function escapeHTML(str) {
  if (!str) return '';
  return str.replace(/[&<>'"]/g, tag => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', "'": '&#39;', '"': '&quot;'
  }[tag] || tag));
}

function copyToClipboard(text) {
  navigator.clipboard.writeText(text);
}

function debounce(fn, wait) {
  let timer;
  return (...args) => {
    clearTimeout(timer);
    timer = setTimeout(() => fn.apply(this, args), wait);
  };
}

// IPData.co Intelligence & Trust Score Inspection
let currentInspectedNode = null;

async function openIPDataModal(node) {
  currentInspectedNode = node;
  openModal('ipdataModal');

  const ip = node.exit_ip || node.server;
  document.getElementById('ipdataValIP').textContent = ip;
  document.getElementById('ipdataValCountry').textContent = `${getFlagEmoji(node.country)} ${node.country_name || node.country}`;
  document.getElementById('ipdataValCountryCode').textContent = node.country || '--';
  document.getElementById('ipdataValContinent').textContent = '--';
  document.getElementById('ipdataValCurrency').textContent = '--';
  document.getElementById('ipdataValTimeZone').textContent = '--';
  document.getElementById('ipdataValOrg').textContent = node.organisation || '--';
  document.getElementById('ipdataValThreatCount').textContent = node.threats_count || '0';

  const trustScoreBox = document.querySelector('.ipdata-trust-score-box');
  const trustScoreTitle = document.getElementById('ipdataValTrustScore');

  if (node.trust_score >= 0) {
    const rLabel = node.risk_level || (node.trust_score >= 60 ? 'Low risk' : (node.trust_score >= 40 ? 'Moderate risk' : 'High risk'));
    trustScoreTitle.textContent = `${node.trust_score} – ${rLabel}`;
    trustScoreBox.className = 'ipdata-trust-score-box';
    trustScoreTitle.className = 'ipdata-trust-score-title';

    if (node.trust_score >= 60) {
      trustScoreBox.classList.add('is-safe');
      trustScoreTitle.classList.add('is-safe');
    } else if (node.trust_score >= 40) {
      trustScoreBox.classList.add('is-warning');
      trustScoreTitle.classList.add('is-warning');
    }
  } else {
    trustScoreTitle.textContent = 'Untested';
    trustScoreBox.className = 'ipdata-trust-score-box';
    trustScoreTitle.className = 'ipdata-trust-score-title';
    // Auto-fetch if unrated
    inspectAndCheckIPData(node.id);
  }
}

async function inspectAndCheckIPData(nodeId) {
  const trustScoreTitle = document.getElementById('ipdataValTrustScore');
  if (trustScoreTitle) trustScoreTitle.textContent = 'Querying ipdata.co...';

  try {
    const res = await fetch(`/api/nodes/ipdata?id=${nodeId}`, { method: 'POST' });
    if (!res.ok) {
      const err = await res.json();
      throw new Error(err.error || 'Failed querying ipdata.co');
    }
    const data = await res.json();
    populateIPDataModal(data);
    loadNodes(); // Refresh list to reflect new score
    showToast(`Trust Score: ${data.trust_score} (${data.risk_level})`, 'success');
  } catch (e) {
    showToast(e.message, 'error');
    if (trustScoreTitle) trustScoreTitle.textContent = 'Query failed';
  }
}

function populateIPDataModal(data) {
  document.getElementById('ipdataValIP').textContent = data.ip;
  document.getElementById('ipdataValCountry').textContent = `${getFlagEmoji(data.country_code)} ${data.country_name || data.country_code}`;
  document.getElementById('ipdataValCountryCode').textContent = data.country_code || '--';
  document.getElementById('ipdataValContinent').textContent = data.continent_code || '--';
  document.getElementById('ipdataValCurrency').textContent = data.currency_code || '--';
  document.getElementById('ipdataValTimeZone').textContent = data.time_zone || '--';
  document.getElementById('ipdataValOrg').textContent = data.organisation || '--';
  document.getElementById('ipdataValThreatCount').textContent = data.threats_count || '0';

  const trustScoreBox = document.querySelector('.ipdata-trust-score-box');
  const trustScoreTitle = document.getElementById('ipdataValTrustScore');

  trustScoreTitle.textContent = `${data.trust_score} – ${data.risk_level}`;
  trustScoreBox.className = 'ipdata-trust-score-box';
  trustScoreTitle.className = 'ipdata-trust-score-title';

  if (data.trust_score >= 60) {
    trustScoreBox.classList.add('is-safe');
    trustScoreTitle.classList.add('is-safe');
  } else if (data.trust_score >= 40) {
    trustScoreBox.classList.add('is-warning');
    trustScoreTitle.classList.add('is-warning');
  }

  // Threat flags
  updateThreatFlag('flagDatacenter', '🏢 Datacenter', data.is_datacenter);
  updateThreatFlag('flagVPN', '🔒 VPN', data.is_vpn);
  updateThreatFlag('flagProxy', '🌐 Proxy', data.is_proxy);
  updateThreatFlag('flagTor', '🧅 Tor', data.is_tor);
  updateThreatFlag('flagThreat', '⚠️ Threat', data.is_threat);
}

function updateThreatFlag(elementId, label, isActive) {
  const el = document.getElementById(elementId);
  if (!el) return;
  el.textContent = `${label}: ${isActive ? 'Yes' : 'No'}`;
  if (isActive) {
    el.classList.add('active-flag');
  } else {
    el.classList.remove('active-flag');
  }
}

async function checkAllTrustScores() {
  try {
    const res = await fetch('/api/nodes/check-all-trust', { method: 'POST' });
    const data = await res.json();
    showToast(data.message || 'Background trust check started', 'info');
    let checks = 0;
    const interval = setInterval(() => {
      checks++;
      loadNodes();
      if (checks >= 10) clearInterval(interval);
    }, 2500);
  } catch (e) {
    showToast('Failed starting trust check: ' + e.message, 'error');
  }
}

function flagImg(code) {
  if (!code) return '🌐 ';
  const c = code.toUpperCase();
  if (c === 'CF' || c === 'CLOUDFLARE' || c === 'WW' || c === 'GLOBAL') return '☁️ ';
  if (code.length !== 2 || c === 'UN') return '🌐 ';
  return `<img class="flag-img" src="flags/${code.toLowerCase()}.svg" alt="${code}" loading="lazy" onerror="this.style.display='none'">`;
}


function stripFlagEmoji(s) {
  return (s || '').replace(/[\u{1F1E6}-\u{1F1FF}]{2}\s*/gu, '');
}

// Cloudflare Clean IP Scanner & CDN Fronting
let cleanIPPollTimer = null;
let cleanIPList = [];

async function loadCleanIPStatus() {
  try {
    const res = await fetch('/api/tools/clean-ip/status');
    if (!res.ok) return;
    const data = await res.json();
    updateCleanIPUI(data);
  } catch (e) {
    console.error('Failed fetching clean IP status:', e);
  }
}

function updateCleanIPUI(data) {
  const state = data.state || 'idle';
  const banner = document.getElementById('cleanIPScanBanner');
  const btnStart = document.getElementById('btnStartCleanIP');
  const btnCancel = document.getElementById('btnCancelCleanIP');
  const stateText = document.getElementById('cleanIPStateText');
  const msgText = document.getElementById('cleanIPMsgText');
  const metricTotal = document.getElementById('cleanIPMetricTotal');
  const metricTested = document.getElementById('cleanIPMetricTested');
  const metricWorking = document.getElementById('cleanIPMetricWorking');
  const metricBestLat = document.getElementById('cleanIPMetricBestLat');
  const progressBar = document.getElementById('cleanIPProgressBar');

  if (metricTotal) metricTotal.textContent = data.total_ips || 0;
  if (metricTested) metricTested.textContent = data.tested_ips || 0;
  if (metricWorking) metricWorking.textContent = data.working_ips || 0;
  if (metricBestLat) metricBestLat.textContent = data.best_latency > 0 ? `${data.best_latency} ms` : '-- ms';
  if (progressBar) progressBar.style.width = `${data.progress_pct || 0}%`;

  if (state === 'scanning') {
    if (banner) banner.style.display = 'block';
    if (btnStart) {
      btnStart.disabled = true;
      btnStart.innerHTML = '<span class="spinner" style="display:inline-block;width:12px;height:12px;margin-right:6px;border:2px solid #000;border-top-color:transparent;border-radius:50%;animation:conn-spin .8s linear infinite;"></span> Scanning...';
    }
    if (btnCancel) btnCancel.style.display = 'inline-block';
    if (stateText) stateText.textContent = 'Scanning Cloudflare Clean IPs...';
    if (msgText) msgText.textContent = data.message || `Testing IPs (${data.tested_ips}/${data.total_ips})...`;

    // Ensure polling is active
    if (!cleanIPPollTimer) {
      cleanIPPollTimer = setInterval(loadCleanIPStatus, 800);
    }
  } else {
    if (cleanIPPollTimer) {
      clearInterval(cleanIPPollTimer);
      cleanIPPollTimer = null;
    }
    if (btnStart) {
      btnStart.disabled = false;
      btnStart.innerHTML = '<span class="btn-icon">⚡</span> Start Clean IP Scan';
    }
    if (btnCancel) btnCancel.style.display = 'none';

    if (state === 'completed' || state === 'cancelled') {
      if (banner) banner.style.display = 'block';
      if (stateText) stateText.textContent = state === 'completed' ? 'Clean IP Scan Complete' : 'Scan Cancelled';
      if (msgText) msgText.textContent = data.message || '';
      const spinner = banner ? banner.querySelector('.spinner') : null;
      if (spinner) spinner.style.display = 'none';
    } else {
      if (banner) banner.style.display = 'none';
    }
  }

  cleanIPList = data.best_ips || [];
  renderCleanIPResults(cleanIPList);
}

function renderCleanIPResults(ips) {
  const tbody = document.getElementById('cleanIPTableBody');
  const countEl = document.getElementById('cleanIPFoundCount');
  const btnConnectFastest = document.getElementById('btnConnectFastestCleanIP');
  const btnSaveFastest = document.getElementById('btnSaveFastestCleanIP');
  const btnSaveAll = document.getElementById('btnSaveAllCleanIP');

  if (countEl) countEl.textContent = ips.length;
  if (btnConnectFastest) {
    btnConnectFastest.disabled = ips.length === 0;
    if (ips.length > 0) {
      btnConnectFastest.innerHTML = `⚡ اتصال به سریع‌ترین (${ips[0].ip} - ${ips[0].latency}ms)`;
    } else {
      btnConnectFastest.innerHTML = '⚡ اتصال به سریع‌ترین آی‌پی (Connect VPN)';
    }
  }
  if (btnSaveFastest) {
    btnSaveFastest.disabled = ips.length === 0;
  }
  if (btnSaveAll) {
    btnSaveAll.disabled = ips.length === 0;
  }

  if (!tbody) return;

  if (ips.length === 0) {
    tbody.innerHTML = `
      <tr>
        <td colspan="5" class="text-center empty-state">
          هنوز آی‌پی تمیزی یافت نشده است. روی <b>Start Clean IP Scan</b> کلیک کنید تا آی‌پی‌های پرسرعت Anycast پیدا شوند.
        </td>
      </tr>
    `;
    return;
  }

  tbody.innerHTML = '';
  ips.forEach((item, index) => {
    const tr = document.createElement('tr');
    let latClass = 'cleanip-latency-slow';
    if (item.latency < 140) latClass = 'cleanip-latency-fast';
    else if (item.latency < 220) latClass = 'cleanip-latency-med';

    tr.innerHTML = `
      <td>${index + 1}</td>
      <td>
        <div style="display: flex; align-items: center; gap: 6px;">
          <span>${flagImg(item.country)}</span>
          <b style="font-family: monospace; font-size: 13px;">${escapeHTML(item.ip)}</b>
        </div>
      </td>
      <td>
        <span style="font-size: 12px; color: var(--text-muted);">
          ${item.country_name ? `<b>${escapeHTML(item.country_name)}</b> • ` : ''}${escapeHTML(item.subnet || 'Anycast')}
        </span>
      </td>
      <td>
        <span class="cleanip-latency-pill ${latClass}">⚡ ${item.latency} ms</span>
      </td>
      <td>
        <div class="table-actions" style="gap: 6px;">
          <button class="btn btn-sm btn-primary btn-connect-ip" data-ip="${escapeHTML(item.ip)}" data-lat="${item.latency}" title="اتصال مستقیم به اینترنت آزاد از طریق این آی‌پی تمیز">
            ⚡ اتصال مستقیم
          </button>
          <button class="btn btn-sm btn-secondary btn-save-ip" data-ip="${escapeHTML(item.ip)}" data-lat="${item.latency}" title="افزودن به لیست کانفیگ‌های اصلی">
            ➕ به کانفیگ‌ها
          </button>
          <button class="btn btn-sm btn-secondary btn-copy-ip" data-ip="${escapeHTML(item.ip)}" title="کپی آی‌پی">
            📋
          </button>
        </div>
      </td>
    `;

    tr.querySelector('.btn-connect-ip').addEventListener('click', (e) => {
      e.stopPropagation();
      connectCleanIP(item.ip, item.latency);
    });

    tr.querySelector('.btn-save-ip').addEventListener('click', (e) => {
      e.stopPropagation();
      addCleanIPToConfigs(item.ip, item.latency);
    });

    tr.querySelector('.btn-copy-ip').addEventListener('click', (e) => {
      e.stopPropagation();
      copyToClipboard(item.ip);
      showToast(`آی‌پی ${item.ip} در کلیپ‌بورد کپی شد`, 'success');
    });

    tbody.appendChild(tr);
  });
}

async function startCleanIPScan() {
  const workers = parseInt(document.getElementById('cleanIPWorkers')?.value || '100');
  const timeoutMs = parseInt(document.getElementById('cleanIPTimeout')?.value || '1500');
  const sampleSize = parseInt(document.getElementById('cleanIPSampleSize')?.value || '500');
  const port = parseInt(document.getElementById('cleanIPPort')?.value || '443');

  try {
    const res = await fetch('/api/tools/clean-ip/start', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        workers: workers,
        timeout_ms: timeoutMs,
        sample_size: sampleSize,
        port: port,
      }),
    });

    if (!res.ok) {
      const err = await res.json();
      throw new Error(err.error || 'Failed to start clean IP scan');
    }

    showToast(`در حال اسکن آی‌پی‌های تمیز Cloudflare Anycast...`, 'info');
    loadCleanIPStatus();
  } catch (e) {
    showToast(e.message, 'error');
  }
}

async function cancelCleanIPScan() {
  try {
    const res = await fetch('/api/tools/clean-ip/cancel', { method: 'POST' });
    if (!res.ok) {
      const err = await res.json();
      throw new Error(err.error || 'Failed to cancel scan');
    }
    showToast('درخواست لغو اسکن ثبت شد', 'info');
    loadCleanIPStatus();
  } catch (e) {
    showToast(e.message, 'error');
  }
}

async function connectCleanIP(ip, latency = 0) {
  try {
    showToast(`در حال اتصال به اینترنت آزاد از طریق آی‌پی تمیز ${ip}...`, 'info');
    const res = await fetch('/api/tools/clean-ip/connect', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        ip: ip,
        port: 443,
        latency: latency,
      }),
    });

    if (!res.ok) {
      const err = await res.json();
      throw new Error(err.error || 'Failed connecting to Clean IP tunnel');
    }

    const data = await res.json();
    showToast(data.message || `با موفقیت به اینترنت آزاد از طریق آی‌پی تمیز ${ip} متصل شد!`, 'success');
    closeModal('cleanIPModal');
    loadNodes();
    loadStatus();
  } catch (e) {
    showToast('خطا در اتصال به آی‌پی تمیز: ' + e.message, 'error');
  }
}

async function connectFastestCleanIP() {
  if (cleanIPList.length === 0) {
    showToast('هیچ آی‌پی تمیزی یافت نشد. لطفاً ابتدا اسکن را شروع کنید.', 'warning');
    return;
  }
  connectCleanIP(cleanIPList[0].ip, cleanIPList[0].latency);
}

async function addCleanIPToConfigs(ip, latency = 0) {
  try {
    showToast(`در حال افزودن آی‌پی تمیز ${ip} به لیست کانفیگ‌ها...`, 'info');
    const res = await fetch('/api/tools/clean-ip/save', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        ip: ip,
        port: 443,
        latency: latency,
      }),
    });

    if (!res.ok) {
      const err = await res.json();
      throw new Error(err.error || 'Failed to add clean IP to configs');
    }

    const data = await res.json();
    showToast(data.message || `نود ${ip} با موفقیت به لیست کانفیگ‌های اصلی اضافه شد`, 'success');
    loadNodes();
  } catch (e) {
    showToast('خطا در افزودن به کانفیگ‌ها: ' + e.message, 'error');
  }
}

async function addFastestCleanIPToConfigs() {
  if (!cleanIPList || cleanIPList.length === 0) {
    showToast('هیچ آی‌پی تمیزی یافت نشد. لطفاً ابتدا اسکن را شروع کنید.', 'warning');
    return;
  }
  await addCleanIPToConfigs(cleanIPList[0].ip, cleanIPList[0].latency);
}

async function addAllCleanIPsToConfigs() {
  if (!cleanIPList || cleanIPList.length === 0) {
    showToast('هیچ آی‌پی تمیزی برای افزودن وجود ندارد.', 'warning');
    return;
  }
  try {
    showToast('در حال افزودن آی‌پی‌های تمیز به کانفیگ‌ها...', 'info');
    const res = await fetch('/api/tools/clean-ip/save-all', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        ips: cleanIPList.map(item => ({ ip: item.ip, port: 1701, latency: item.latency })),
      }),
    });

    if (!res.ok) {
      const err = await res.json();
      throw new Error(err.error || 'Failed to save clean IPs');
    }

    const data = await res.json();
    showToast(data.message || `${cleanIPList.length} آی‌پی تمیز به لیست کانفیگ‌ها اضافه شد`, 'success');
    loadNodes();
  } catch (e) {
    showToast('خطا در افزودن کانفیگ‌ها: ' + e.message, 'error');
  }
}

// ==========================================
// Feature 8: Visual App Picker for Split Tunneling
// ==========================================

let runningAppsList = [];
let installedAppsList = [];
let currentAppFilter = 'running'; // 'running' | 'installed' | 'all'
let preferredAppTarget = 'direct'; // 'direct' | 'proxy'

function getAvatarColor(str) {
  const colors = [
    'linear-gradient(135deg, #2563eb, #1d4ed8)',
    'linear-gradient(135deg, #059669, #047857)',
    'linear-gradient(135deg, #d97706, #b45309)',
    'linear-gradient(135deg, #7c3aed, #6d28d9)',
    'linear-gradient(135deg, #db2777, #be185d)',
    'linear-gradient(135deg, #0891b2, #0e7490)',
    'linear-gradient(135deg, #4f46e5, #4338ca)',
    'linear-gradient(135deg, #0d9488, #0f766e)'
  ];
  if (!str) return colors[0];
  let hash = 0;
  for (let i = 0; i < str.length; i++) {
    hash = (hash << 5) - hash + str.charCodeAt(i);
  }
  return colors[Math.abs(hash) % colors.length];
}

function openAppPicker(target = 'direct') {
  preferredAppTarget = target;
  const notice = document.getElementById('appPickerNotice');
  if (notice) {
    if (target === 'direct') {
      notice.innerHTML = 'Add apps to <b>Direct Apps (Blacklist)</b> to bypass the VPN and connect directly with lowest latency.';
    } else {
      notice.innerHTML = 'Add apps to <b>Proxy Apps (Whitelist)</b> to route through the secure VPN connection.';
    }
  }

  openModal('appPickerModal');
  loadAppPickerData(false);
}

async function loadAppPickerData(forceRefresh = false) {
  const listEl = document.getElementById('appPickerList');
  if (!listEl) return;

  if (forceRefresh || runningAppsList.length === 0) {
    listEl.innerHTML = `
      <div class="app-picker-loading">
        <div class="conn-spinner"></div>
        <span>Detecting running applications on Windows...</span>
      </div>
    `;
  }

  try {
    const res = await fetch('/api/system/running-apps');
    if (!res.ok) throw new Error('HTTP ' + res.status);
    runningAppsList = (await res.json()) || [];
    
    const countRunningEl = document.getElementById('appCountRunning');
    if (countRunningEl) countRunningEl.textContent = runningAppsList.length;

    renderAppPickerList();

    // Background fetch installed apps if not loaded yet or requested
    if (installedAppsList.length === 0 || forceRefresh) {
      fetch('/api/system/running-apps?type=installed')
        .then(r => r.json())
        .then(data => {
          installedAppsList = data || [];
          const countInstEl = document.getElementById('appCountInstalled');
          if (countInstEl) countInstEl.textContent = installedAppsList.length;
          if (currentAppFilter !== 'running') {
            renderAppPickerList();
          }
        })
        .catch(err => console.error('Failed to load installed apps:', err));
    }
  } catch (err) {
    listEl.innerHTML = `
      <div class="app-picker-empty">
        <span style="font-size: 24px;">⚠️</span>
        <span>Failed to load active applications: ${escapeHTML(err.message)}</span>
        <button type="button" class="btn btn-secondary btn-sm" id="btnRetryAppPicker">Retry</button>
      </div>
    `;
    const retryBtn = document.getElementById('btnRetryAppPicker');
    if (retryBtn) retryBtn.addEventListener('click', () => loadAppPickerData(true));
  }
}

function renderAppPickerList() {
  const listEl = document.getElementById('appPickerList');
  if (!listEl) return;

  const searchInput = document.getElementById('appPickerSearch');
  const query = (searchInput ? searchInput.value : '').trim().toLowerCase();

  let sourceList = runningAppsList;
  if (currentAppFilter === 'installed') {
    sourceList = installedAppsList;
  } else if (currentAppFilter === 'all') {
    const map = new Map();
    runningAppsList.forEach(a => map.set(a.exe_name.toLowerCase(), a));
    installedAppsList.forEach(a => {
      const k = a.exe_name.toLowerCase();
      if (!map.has(k)) map.set(k, a);
    });
    sourceList = Array.from(map.values());
  }

  // Current values in Direct and Proxy textareas
  const directText = (document.getElementById('routeBlacklistApps')?.value || '').toLowerCase();
  const proxyText = (document.getElementById('routeWhitelistApps')?.value || '').toLowerCase();
  const directSet = new Set(directText.split('\n').map(s => s.trim()).filter(Boolean));
  const proxySet = new Set(proxyText.split('\n').map(s => s.trim()).filter(Boolean));

  // Running set to show badge
  const runningSet = new Set(runningAppsList.map(a => a.exe_name.toLowerCase()));

  // Filter
  const filtered = sourceList.filter(app => {
    if (!query) return true;
    return (app.name && app.name.toLowerCase().includes(query)) ||
           (app.exe_name && app.exe_name.toLowerCase().includes(query)) ||
           (app.title && app.title.toLowerCase().includes(query)) ||
           (app.path && app.path.toLowerCase().includes(query));
  });

  const summaryEl = document.getElementById('appPickerSummary');
  if (summaryEl) {
    summaryEl.textContent = `Showing ${filtered.length} of ${sourceList.length} applications`;
  }

  if (filtered.length === 0) {
    listEl.innerHTML = `
      <div class="app-picker-empty">
        <span style="font-size: 24px;">🔍</span>
        <span>${query ? `No applications found matching "${escapeHTML(query)}"` : 'No applications available.'}</span>
      </div>
    `;
    return;
  }

  let html = '';
  for (const app of filtered) {
    const exeLower = (app.exe_name || '').toLowerCase();
    const inDirect = directSet.has(exeLower);
    const inProxy = proxySet.has(exeLower);
    const isRunning = runningSet.has(exeLower);

    const initial = (app.name || app.exe_name || '?').charAt(0).toUpperCase();
    const avatarBg = getAvatarColor(app.exe_name || app.name);

    html += `
      <div class="app-picker-item" data-exe="${escapeHTML(app.exe_name)}">
        <div class="app-picker-item-left">
          <div class="app-avatar" style="background: ${avatarBg};">${escapeHTML(initial)}</div>
          <div class="app-details">
            <div class="app-title-row">
              <span class="app-name" title="${escapeHTML(app.name)}">${escapeHTML(app.name || app.exe_name)}</span>
              <span class="app-exe-badge">${escapeHTML(app.exe_name)}</span>
              ${isRunning ? '<span class="app-running-tag">Running</span>' : ''}
            </div>
            <div class="app-subtext" title="${escapeHTML(app.title || app.path)}">
              ${escapeHTML(app.title ? app.title : app.path)}
            </div>
          </div>
        </div>
        <div class="app-actions">
          <button type="button" class="btn-app-direct ${inDirect ? 'is-added' : ''}" data-exe="${escapeHTML(app.exe_name)}" title="${inDirect ? 'Remove from Direct Apps' : 'Add to Direct Apps (Bypass)'}">
            ${inDirect ? '✓ In Direct' : '+ Direct'}
          </button>
          <button type="button" class="btn-app-proxy ${inProxy ? 'is-added' : ''}" data-exe="${escapeHTML(app.exe_name)}" title="${inProxy ? 'Remove from Proxy Apps' : 'Add to Proxy Apps (Tunnel)'}">
            ${inProxy ? '✓ In Proxy' : '+ Proxy'}
          </button>
        </div>
      </div>
    `;
  }

  listEl.innerHTML = html;
}

function handleAppPickerAction(e) {
  const directBtn = e.target.closest('.btn-app-direct');
  if (directBtn) {
    const exe = directBtn.dataset.exe;
    const added = toggleAppInTextarea('routeBlacklistApps', exe, 'Direct Apps');
    directBtn.classList.toggle('is-added', added);
    directBtn.textContent = added ? '✓ In Direct' : '+ Direct';
    directBtn.title = added ? 'Remove from Direct Apps' : 'Add to Direct Apps (Bypass)';
    return;
  }

  const proxyBtn = e.target.closest('.btn-app-proxy');
  if (proxyBtn) {
    const exe = proxyBtn.dataset.exe;
    const added = toggleAppInTextarea('routeWhitelistApps', exe, 'Proxy Apps');
    proxyBtn.classList.toggle('is-added', added);
    proxyBtn.textContent = added ? '✓ In Proxy' : '+ Proxy';
    proxyBtn.title = added ? 'Remove from Proxy Apps' : 'Add to Proxy Apps (Tunnel)';
    return;
  }
}

function toggleAppInTextarea(textareaId, exeName, typeName) {
  const ta = document.getElementById(textareaId);
  if (!ta) return false;

  let lines = ta.value.split('\n').map(l => l.trim()).filter(Boolean);
  const lowerExe = exeName.toLowerCase();
  const existingIdx = lines.findIndex(l => l.toLowerCase() === lowerExe);

  if (existingIdx >= 0) {
    lines.splice(existingIdx, 1);
    ta.value = lines.join('\n');
    showToast(`Removed ${exeName} from ${typeName}`, 'info');
    return false;
  } else {
    lines.push(lowerExe);
    ta.value = lines.join('\n');
    showToast(`Added ${exeName} to ${typeName}`, 'success');
    return true;
  }
}




