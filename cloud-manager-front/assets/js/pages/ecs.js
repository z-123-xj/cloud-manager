/* ECS实例 · 独立页面业务脚本（只依赖 CloudCore） */
(function () {
  'use strict';

  const { ref, reactive, computed, onMounted, onUnmounted, nextTick } = CloudCore.Vue;
  const { authFetch, showToast, logout, formatDate } = CloudCore;

  const PLATFORM_LABELS = {
    aliyun: '阿里云', aws: 'AWS', tencent: '腾讯云', cloudflare: 'Cloudflare',
    huawei: '华为云', azure: 'Azure', gcp: 'Google Cloud',
  };
  function platformLabel(p) { return PLATFORM_LABELS[p] || p || '-'; }

  // 列顺序与 CSS 的 nth-child 序号严格对应：隐藏列用 display:none 保留 DOM，序号不会错位
  const ECS_COLUMNS = [
    { key: 'name',     label: '实例ID/名称', width: 180, fixed: true },
    { key: 'platform', label: '平台',       width: 70 },
    { key: 'account',  label: '账号',       width: 180 },
    { key: 'status',   label: '状态',       width: 85 },
    { key: 'type',     label: '规格',       width: 130 },
    { key: 'cpu',      label: 'CPU',        width: 55 },
    { key: 'memory',   label: '内存(GB)',   width: 80 },
    { key: 'os',       label: '操作系统',   width: 170 },
    { key: 'ip',       label: 'IP地址',     width: 190 },
    { key: 'ssh_port', label: 'SSH端口',    width: 85 },
    { key: 'region',   label: '地域',       width: 110 },
    { key: 'zone',     label: '可用区',     width: 120 },
    { key: 'created',  label: '创建时间',   width: 150 },
    { key: 'source',   label: '来源',       width: 70 },
    { key: 'action',   label: '操作',       width: 130, fixed: true },
  ];
  const ECS_COL_STORE = 'cloud_ecs_cols_';

  // 按登录账号隔离偏好：每个用户各自记住上次调整的列宽与显示列
  function ecsColStoreKey() {
    return ECS_COL_STORE + (localStorage.getItem('cloud_user') || 'anonymous');
  }
  function loadEcsColPrefs() {
    try {
      const raw = localStorage.getItem(ecsColStoreKey());
      const data = raw ? JSON.parse(raw) : null;
      return data && typeof data === 'object' ? data : {};
    } catch (e) { return {}; }
  }

  CloudCore.mountPage({
    key: 'ecs',
    title: 'ECS实例',
    icon: 'fa-server',
    hasNavDir: true,
    template: document.getElementById('page-template').innerHTML,
    setup() {
      const loading = ref(false);
      const list = ref([]);
      const total = ref(0);
      const page = ref(1);
      const size = ref(20);
      const pageInput = ref(1);
      const filters = reactive({ keyword: '', is_offline: '', platform: '', account: '', region: '' });

      const platformList = ref([]);
      const accountOptions = ref([]);
      const regions = ref([]);
      const navDirKeyword = ref('');

      const totalPages = computed(() => Math.ceil(total.value / size.value) || 1);

      const filteredPlatforms = computed(() => {
        const kw = navDirKeyword.value.trim().toLowerCase();
        if (!kw) return platformList.value;
        return platformList.value.filter(pf =>
          platformLabel(pf).toLowerCase().includes(kw) ||
          String(pf).toLowerCase().includes(kw) ||
          accountsOfPlatform(pf, true).length > 0
        );
      });

      function accountsOfPlatform(pf, ignoreKeyword) {
        const kw = ignoreKeyword ? '' : navDirKeyword.value.trim().toLowerCase();
        return accountOptions.value
          .filter(a => a.platform === pf)
          .map(a => a.name)
          .filter(name => !kw || String(name).toLowerCase().includes(kw));
      }

      function selectFilter(platform, account) {
        filters.platform = platform;
        filters.account = account;
        page.value = 1;
        pageInput.value = 1;
        fetchList();
      }

      function statusLabel(status) {
        const map = { Running: '运行中', Stopped: '已停止', Starting: '启动中', Stopping: '停止中' };
        return map[status] || status || '-';
      }
      function dotClass(status) {
        const map = { Running: 'dot-online', Stopped: 'dot-offline', Starting: 'dot-warning', Stopping: 'dot-warning' };
        return map[status] || 'dot-offline';
      }

      async function fetchList() {
        loading.value = true;
        try {
          const params = new URLSearchParams();
          params.append('page', String(page.value));
          params.append('size', String(size.value));
          if (filters.keyword) params.append('keyword', filters.keyword);
          if (filters.is_offline !== '') params.append('is_offline', filters.is_offline);
          if (filters.platform) params.append('platform', filters.platform);
          if (filters.account) params.append('account_name', filters.account);
          if (filters.region) params.append('region_name', filters.region);

          const res = await authFetch('/api/ecs?' + params.toString());
          const json = await res.json();
          if (json.code === 401) { logout(); return; }
          if (json.code === 0) {
            list.value = json.data.list || [];
            total.value = json.data.total || 0;
            const regs = new Set();
            list.value.forEach(it => { if (it.region_name) regs.add(it.region_name); });
            if (regs.size) regions.value = Array.from(regs).sort();
          }
        } catch (e) {
          showToast('ECS查询请求失败', 'error');
        } finally {
          loading.value = false;
        }
      }

      async function fetchFilterOptions() {
        try {
          const res = await authFetch('/api/ecs/filter-options');
          const json = await res.json();
          if (json.code === 0) {
            platformList.value = (json.data.platforms || []).sort();
            accountOptions.value = json.data.account_options || [];
          }
        } catch (e) { /* ignore */ }
      }

      function search() { page.value = 1; pageInput.value = 1; fetchList(); }
      function resetFilters() {
        filters.keyword = '';
        filters.is_offline = '';
        filters.platform = '';
        filters.account = '';
        filters.region = '';
        page.value = 1;
        pageInput.value = 1;
        fetchList();
      }
      function goPage(p) {
        if (p < 1 || p > totalPages.value) return;
        page.value = p;
        pageInput.value = p;
        fetchList();
      }
      function jumpPage() {
        const p = pageInput.value;
        if (p >= 1 && p <= totalPages.value) goPage(p);
      }
      function onSizeChange() { page.value = 1; pageInput.value = 1; fetchList(); }

      let keywordTimer = null;
      function onKeywordInput() {
        if (keywordTimer) clearTimeout(keywordTimer);
        keywordTimer = setTimeout(() => { page.value = 1; pageInput.value = 1; fetchList(); }, 300);
      }

      function openSync() {
        if (!window.CloudSyncModal) { showToast('同步模块未加载', 'error'); return; }
        window.CloudSyncModal.open({
          type: 'ecs',
          typeName: 'ECS实例',
          onDone: () => { fetchList(); fetchFilterOptions(); },
        });
      }

      const ecsManualModalVisible = ref(false);
      const ecsManualEditing = ref(false);
      const ecsManualSaving = ref(false);
      const ecsManualEditId = ref(null);
      const ecsManualForm = reactive({ instance_name: '', public_ip: '', eip: '', private_ip: '', ssh_port: 22, platform: '', account_name: '', instance_type: '', cpu: 0, memory: 0, os_name: '', region_name: '', zone_name: '' });

      function openEcsManualModal(item) {
        if (item) {
          ecsManualEditing.value = true;
          ecsManualEditId.value = item.id;
          ecsManualForm.instance_name = item.instance_name || '';
          ecsManualForm.public_ip = item.public_ip_address || '';
          ecsManualForm.eip = item.eip_address || '';
          ecsManualForm.private_ip = item.private_ip_address || '';
          ecsManualForm.ssh_port = item.ssh_port || 22;
          ecsManualForm.platform = item.platform || '';
          ecsManualForm.account_name = item.account_name || '';
          ecsManualForm.instance_type = item.instance_type || '';
          ecsManualForm.cpu = item.cpu || 0;
          ecsManualForm.memory = item.memory || 0;
          ecsManualForm.os_name = item.os_name || '';
          ecsManualForm.region_name = item.region_name || '';
          ecsManualForm.zone_name = item.zone_name || '';
        } else {
          ecsManualEditing.value = false;
          ecsManualEditId.value = null;
          ecsManualForm.instance_name = '';
          ecsManualForm.public_ip = '';
          ecsManualForm.eip = '';
          ecsManualForm.private_ip = '';
          ecsManualForm.ssh_port = 22;
          ecsManualForm.platform = '';
          ecsManualForm.account_name = '';
          ecsManualForm.instance_type = '';
          ecsManualForm.cpu = 0;
          ecsManualForm.memory = 0;
          ecsManualForm.os_name = '';
          ecsManualForm.region_name = '';
          ecsManualForm.zone_name = '';
        }
        ecsManualModalVisible.value = true;
      }

      async function saveEcsManual() {
        if (!ecsManualForm.instance_name) { showToast('实例名称必填', 'error'); return; }
        if (ecsManualForm.ssh_port && (ecsManualForm.ssh_port < 1 || ecsManualForm.ssh_port > 65535)) { showToast('SSH端口必须在 1-65535 之间', 'error'); return; }
        ecsManualSaving.value = true;
        try {
          let res;
          if (ecsManualEditing.value) {
            res = await authFetch('/api/ecs/' + ecsManualEditId.value, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(ecsManualForm) });
          } else {
            res = await authFetch('/api/ecs', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(ecsManualForm) });
          }
          const json = await res.json();
          if (json.code === 401) { logout(); return; }
          if (json.code === 0) { showToast(ecsManualEditing.value ? '更新成功' : '创建成功'); ecsManualModalVisible.value = false; fetchList(); }
          else showToast(json.msg || '操作失败', 'error');
        } catch (e) { showToast('请求失败', 'error'); } finally { ecsManualSaving.value = false; }
      }

      // 双击单元格修改实例名称 / SSH 端口：实例名称会同步到云端，SSH 端口仅维护本地库；
      // 两个接口都要求管理员，故这里先用 cloud_role 兜底判断。
      const ecsInlineEdit = ref({ id: null, field: '' });
      const ecsInlineValue = ref('');
      const ecsInlineSaving = ref(false);

      function startEcsInlineEdit(item, field) {
        if (localStorage.getItem('cloud_role') !== 'admin' || ecsInlineSaving.value) return;
        ecsInlineEdit.value = { id: item.id, field: field };
        ecsInlineValue.value = field === 'ssh_port'
          ? String(item.ssh_port || 22)
          : (item.instance_name || '');
        nextTick(() => {
          const el = document.querySelector('.ecs-inline-input');
          if (el) { el.focus(); try { el.select(); } catch (e) { /* ignore */ } }
        });
      }

      function cancelEcsInlineEdit() {
        ecsInlineEdit.value = { id: null, field: '' };
        ecsInlineValue.value = '';
      }

      async function saveEcsInlineEdit(item) {
        if (ecsInlineSaving.value) return;
        const edit = ecsInlineEdit.value;
        if (!edit.id || edit.id !== item.id) return;

        const field = edit.field;
        let value;
        if (field === 'ssh_port') {
          value = parseInt(ecsInlineValue.value, 10);
          if (isNaN(value) || value < 1 || value > 65535) { cancelEcsInlineEdit(); return; }
          if (value === (item.ssh_port || 22)) { cancelEcsInlineEdit(); return; }
        } else {
          value = (ecsInlineValue.value || '').trim();
          if (!value || value === item.instance_name) { cancelEcsInlineEdit(); return; }
        }

        ecsInlineSaving.value = true;
        try {
          const url = field === 'ssh_port'
            ? '/api/ecs/' + item.id + '/ssh-port'
            : '/api/ecs/' + item.id + '/name';
          const body = field === 'ssh_port' ? { ssh_port: value } : { instance_name: value };
          const res = await authFetch(url, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
          });
          const json = await res.json();
          if (json.code === 401) { logout(); return; }
          if (json.code === 0) {
            if (field === 'ssh_port') item.ssh_port = value;
            else item.instance_name = value;
            showToast('已保存');
          } else {
            showToast(json.msg || '保存失败', 'error');
          }
        } catch (e) { showToast('保存失败', 'error'); }
        finally {
          ecsInlineSaving.value = false;
          cancelEcsInlineEdit();
        }
      }

      // ============ SSH 连接弹窗（仅 can_connect 为 true 的行可触发）============
      const connectVisible = ref(false);
      const connectLoading = ref(false);
      const connectAsset = reactive({ id: 0, name: '', public_ip: '', private_ip: '', eip: '', port: 22, public_is_eip: false });
      const connectNetwork = ref('public');
      const connectCredId = ref(0);
      const connectCredList = ref([]);
      const connectCredDrpOpen = ref(false);
      const credCache = {};

      function getCredPref(assetId) {
        try { return parseInt(localStorage.getItem('bastion_cred_pref_' + assetId)) || 0; } catch (e) { return 0; }
      }
      function setCredPref(assetId, credId) {
        try { localStorage.setItem('bastion_cred_pref_' + assetId, credId); } catch (e) {}
      }
      function credLabel() {
        if (!connectCredList.value.length) return '暂无可用凭证';
        const c = connectCredList.value.find(x => x.id === connectCredId.value);
        return c ? c.name + '(' + c.username + ')' : '选择凭证';
      }
      function closeConnect() {
        connectVisible.value = false;
        connectCredDrpOpen.value = false;
      }

      // 先备好凭证数据再开弹窗，避免凭证下拉先空后闪
      async function connect(item) {
        connectAsset.id = item.id;
        connectAsset.name = item.instance_name || item.instance_id || '';
        // 与 terminal.html 的 normalizeHost 一致：只有弹性 IP 时并入公网，不单列「弹性」选项
        connectAsset.eip = item.eip_address || '';
        connectAsset.public_is_eip = !item.public_ip_address && !!item.eip_address;
        connectAsset.public_ip = item.public_ip_address || item.eip_address || '';
        connectAsset.private_ip = item.private_ip_address || '';
        connectAsset.port = item.ssh_port || 22;
        connectNetwork.value = connectAsset.public_ip ? 'public' : 'private';

        connectCredId.value = 0;
        connectCredList.value = [];
        connectCredDrpOpen.value = false;
        connectLoading.value = true;
        try {
          let creds = credCache[item.id];
          if (!creds) {
            const res = await authFetch('/api/bastion/assets/' + item.id + '/credentials');
            const json = await res.json();
            if (json.code === 0 && json.data && json.data.length) {
              credCache[item.id] = json.data;
              creds = json.data;
            }
          }
          connectCredList.value = creds || [];
          const pref = getCredPref(item.id);
          connectCredId.value = pref && connectCredList.value.some(c => c.id === pref)
            ? pref
            : (connectCredList.value[0] ? connectCredList.value[0].id : 0);
        } catch (e) {
          showToast('获取凭证失败', 'error');
        } finally {
          connectLoading.value = false;
          connectVisible.value = true;
        }
      }

      // 确认连接：保存偏好后在新标签打开独立终端页
      function doConnect() {
        if (!connectCredId.value) { showToast('请先选择凭证', 'error'); return; }
        try { localStorage.setItem('bastion_network_pref', connectNetwork.value); } catch (e) {}
        setCredPref(connectAsset.id, connectCredId.value);
        const net = connectNetwork.value;
        const ip = net === 'private' ? connectAsset.private_ip : connectAsset.public_ip;
        const params = new URLSearchParams({
          id: connectAsset.id, network: net,
          credential_id: connectCredId.value, name: connectAsset.name,
          ip: ip, public_ip: connectAsset.public_ip || '', private_ip: connectAsset.private_ip || '',
          eip: connectAsset.eip || '', ssh_port: connectAsset.port,
        });
        // 只有弹性 IP 的资产：公网选项实际连 EipAddress，与 terminal.html 的 publicIsEip 一致
        if (net === 'public' && connectAsset.public_is_eip) params.set('public_is_eip', '1');
        closeConnect();
        const win = window.open('/terminal.html?v=' + Date.now() + '&' + params.toString(), '_blank');
        if (!win) showToast('新窗口被浏览器拦截，请允许弹出窗口', 'error');
      }

      // ---- 列设置：拖拽宽度 + 显隐，按登录用户持久化 ----
      const prefs = loadEcsColPrefs();
      const colWidths = reactive({});
      const colVisible = reactive({});
      const hiddenList = Array.isArray(prefs.hidden) ? prefs.hidden : [];
      ECS_COLUMNS.forEach(c => {
        const w = Number((prefs.widths || {})[c.key]);
        colWidths[c.key] = (w >= 50 && w <= 600) ? w : c.width;
        // 操作列新增「连接」按钮，把旧版保存的 96px 默认宽度迁移到新的 130px
        if (c.key === 'action' && colWidths[c.key] === 96) colWidths[c.key] = c.width;
        colVisible[c.key] = c.fixed ? true : hiddenList.indexOf(c.key) === -1;
      });

      function saveEcsColPrefs() {
        try {
          localStorage.setItem(ecsColStoreKey(), JSON.stringify({
            widths: colWidths,
            hidden: ECS_COLUMNS.filter(c => !colVisible[c.key]).map(c => c.key),
          }));
        } catch (e) { /* 存储不可用时不阻断操作 */ }
      }

      const tableCssVars = computed(() => {
        const vars = {};
        ECS_COLUMNS.forEach((c, i) => { vars['--ecs-col-' + (i + 1)] = colWidths[c.key] + 'px'; });
        return vars;
      });
      const visibleColCount = computed(() => ECS_COLUMNS.filter(c => colVisible[c.key]).length);
      const hiddenColClasses = computed(() => ECS_COLUMNS.filter(c => !colVisible[c.key]).map(c => 'hide-' + c.key));
      function isColVisible(key) { return colVisible[key] !== false; }

      const colPickerOpen = ref(false);
      const colPickerPos = reactive({ top: '120px', right: '56px' });
      function toggleColPicker(e) {
        if (colPickerOpen.value) { colPickerOpen.value = false; return; }
        const rect = (e && e.currentTarget && e.currentTarget.getBoundingClientRect)
          ? e.currentTarget.getBoundingClientRect() : null;
        colPickerPos.top = (rect ? rect.bottom + 8 : 120) + 'px';
        colPickerPos.right = Math.max(8, window.innerWidth - (rect ? rect.right : 80)) + 'px';
        colPickerOpen.value = true;
      }
      function toggleCol(c) {
        if (c.fixed) return;
        colVisible[c.key] = !colVisible[c.key];
        saveEcsColPrefs();
      }
      function showAllCols() {
        ECS_COLUMNS.forEach(c => { colVisible[c.key] = true; });
        saveEcsColPrefs();
      }
      function resetColPrefs() {
        ECS_COLUMNS.forEach(c => {
          colWidths[c.key] = c.width;
          colVisible[c.key] = true;
        });
        saveEcsColPrefs();
      }

      let resizeState = null;
      function startResize(e, key) {
        if (!e || e.button !== 0) return;
        resizeState = { key: key, startX: e.clientX, startW: colWidths[key] };
        document.body.classList.add('ecs-col-resizing');
      }
      function onResizeMove(e) {
        if (!resizeState) return;
        const w = Math.round(resizeState.startW + (e.clientX - resizeState.startX));
        colWidths[resizeState.key] = Math.max(50, Math.min(600, w));
      }
      function onResizeEnd() {
        if (!resizeState) return;
        resizeState = null;
        document.body.classList.remove('ecs-col-resizing');
        saveEcsColPrefs();
      }

      onMounted(() => {
        document.addEventListener('mousemove', onResizeMove);
        document.addEventListener('mouseup', onResizeEnd);
        window.addEventListener('blur', onResizeEnd);
        fetchFilterOptions(); fetchList();
      });
      onUnmounted(() => {
        document.removeEventListener('mousemove', onResizeMove);
        document.removeEventListener('mouseup', onResizeEnd);
        window.removeEventListener('blur', onResizeEnd);
        document.body.classList.remove('ecs-col-resizing');
      });

      return {
        loading, list, total, page, size, pageInput, totalPages, filters, regions,
        navDirKeyword, filteredPlatforms, accountsOfPlatform, selectFilter,
        platformLabel, formatDate, statusLabel, dotClass, search, resetFilters,
        goPage, jumpPage, onSizeChange, openSync, onKeywordInput,
        ecsManualModalVisible, ecsManualEditing, ecsManualSaving, ecsManualForm,
        openEcsManualModal, saveEcsManual,
        ecsInlineEdit, ecsInlineValue, startEcsInlineEdit, saveEcsInlineEdit, cancelEcsInlineEdit,
        ecsColumns: ECS_COLUMNS, tableCssVars, hiddenColClasses, visibleColCount,
        isColVisible, colPickerOpen, colPickerPos, toggleColPicker,
        toggleCol, showAllCols, resetColPrefs, startResize,
        connectVisible, connectLoading, connectAsset, connectNetwork,
        connectCredId, connectCredList, connectCredDrpOpen,
        credLabel, closeConnect, connect, doConnect,
      };
    },
  });
})();
