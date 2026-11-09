/* 域名管理 · 独立页面业务脚本（只依赖 CloudCore） */
(function () {
  'use strict';

  const { ref, reactive, computed, onMounted, nextTick } = CloudCore.Vue;
  const { authFetch, showToast, logout, formatDate } = CloudCore;

  const COL_KEY = 'cloudDomainColumns';

  CloudCore.mountPage({
    key: 'domains',
    title: '域名管理',
    icon: 'fa-globe',
    hasNavDir: true,
    template: document.getElementById('page-template').innerHTML,
    setup() {
      /* ---------------- 基础状态 ---------------- */
      const loading = ref(false);
      const domains = ref([]);
      const total = ref(0);
      const page = ref(1);
      const size = ref(20);
      const pageInput = ref(1);

      const filters = reactive({
        keyword: '', platform: '', account: '',
        is_offline: '', expire_start: '', expire_end: '',
      });

      const stats = reactive({ online: 0, offline: 0, expiringSoon: 0, expired: 0 });

      const platformList = ref([]);
      const accountOptions = ref([]);   // [{ name, platform }]
      const navDirKeyword = ref('');

      /* ---------------- 列显示 ---------------- */
      const DEFAULT_COLUMNS = [
        { key: 'domain_name', label: '域名', visible: true },
        { key: 'platform', label: '平台', visible: true },
        { key: 'account_name', label: '账号', visible: true },
        { key: 'owner', label: '所有者', visible: true },
        { key: 'registration_date', label: '注册日期', visible: true },
        { key: 'expiration_date', label: '到期日期', visible: true },
        { key: 'expiration_days_left', label: '剩余天数', visible: true },
        { key: 'auto_renew_enabled', label: '自动续费', visible: true },
        { key: 'status', label: '状态', visible: true },
        { key: 'source', label: '来源', visible: true },
      ];
      const allColumns = reactive(loadColPrefs());
      const showColPicker = ref(false);

      function loadColPrefs() {
        let saved = {};
        try { saved = JSON.parse(localStorage.getItem(COL_KEY) || '{}') || {}; } catch (e) { saved = {}; }
        return DEFAULT_COLUMNS.map(c => ({ key: c.key, label: c.label, visible: saved[c.key] !== false }));
      }
      function saveColPrefs() {
        const map = {};
        allColumns.forEach(c => { map[c.key] = !!c.visible; });
        try { localStorage.setItem(COL_KEY, JSON.stringify(map)); } catch (e) { /* ignore */ }
      }
      function colVisible(key) {
        const col = allColumns.find(c => c.key === key);
        return col ? col.visible !== false : true;
      }
      const visibleColumns = computed(() => allColumns.filter(c => c.visible !== false));

      /* ---------------- 排序 / 漏斗 ---------------- */
      const sortKey = ref('');
      const sortDir = ref('desc');
      const colFilterOpen = ref(null);
      const colFilters = reactive({ auto_renews: [] });

      function toggleSort(key) {
        if (sortKey.value === key) sortDir.value = sortDir.value === 'asc' ? 'desc' : 'asc';
        else { sortKey.value = key; sortDir.value = 'desc'; }
        page.value = 1;
        fetchDomains();
      }
      function sortIcon(key) {
        if (sortKey.value !== key) return '⇅';
        return sortDir.value === 'asc' ? '▲' : '▼';
      }
      function toggleColFilter(key) {
        colFilterOpen.value = colFilterOpen.value === key ? null : key;
      }
      function toggleFilterOption(arr, value) {
        const i = arr.indexOf(value);
        if (i >= 0) arr.splice(i, 1); else arr.push(value);
      }
      function toggleAllFilter(arr, all) {
        if (arr.length === all.length) arr.splice(0, arr.length);
        else arr.splice(0, arr.length, ...all);
      }
      function isAllSelected(arr, all) {
        return arr.length === all.length && all.length > 0;
      }
      function applyColFilter() {
        page.value = 1;
        pageInput.value = 1;
        fetchDomains();
      }

      /* ---------------- 平台 / 账号 ---------------- */
      const PLATFORM_LABELS = {
        aliyun: '阿里云', aws: 'AWS', tencent: '腾讯云', cloudflare: 'Cloudflare',
        huawei: '华为云', azure: 'Azure', gcp: 'Google Cloud',
      };
      function platformLabel(p) {
        return PLATFORM_LABELS[p] || p || '-';
      }
      const filteredPlatforms = computed(() => {
        const kw = navDirKeyword.value.trim().toLowerCase();
        if (!kw) return platformList.value;
        return platformList.value.filter(pf => {
          if (platformLabel(pf).toLowerCase().includes(kw) || String(pf).toLowerCase().includes(kw)) return true;
          return accountsOfPlatform(pf, true).length > 0;
        });
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
        fetchDomains();
      }

      /* ---------------- 数据加载 ---------------- */
      const totalPages = computed(() => Math.ceil(total.value / size.value) || 1);

      async function fetchDomains() {
        loading.value = true;
        try {
          const needClientFilter = colFilters.auto_renews.length > 0;
          const params = new URLSearchParams();
          params.append('page', needClientFilter ? '1' : String(page.value));
          params.append('size', needClientFilter ? '100' : String(size.value));
          if (filters.keyword) params.append('keyword', filters.keyword);
          if (filters.platform) params.append('platform', filters.platform);
          if (filters.account) params.append('account_name', filters.account);
          if (filters.is_offline !== '') params.append('is_offline', filters.is_offline);
          if (filters.expire_start) params.append('expire_start', filters.expire_start);
          if (filters.expire_end) params.append('expire_end', filters.expire_end);
          if (sortKey.value) {
            params.append('sort_key', sortKey.value);
            params.append('sort_dir', sortDir.value);
          }

          let list = [];
          let serverTotal = 0;

          const res = await authFetch('/api/domains?' + params.toString());
          const json = await res.json();
          if (json.code === 401) { logout(); return; }
          if (json.code !== 0) { showToast(json.msg || '查询失败', 'error'); return; }

          list = json.data.list || [];
          serverTotal = json.data.total || 0;

          if (needClientFilter && serverTotal > 100) {
            const pages = Math.min(Math.ceil(serverTotal / 100), 100);
            for (let p = 2; p <= pages; p++) {
              params.set('page', String(p));
              const r2 = await authFetch('/api/domains?' + params.toString());
              const j2 = await r2.json();
              if (j2.code === 0) list = list.concat(j2.data.list || []);
            }
          }

          if (colFilters.auto_renews.length > 0) {
            list = list.filter(it => colFilters.auto_renews.includes(it.auto_renew_enabled ? '1' : '0'));
          }

          if (needClientFilter) {
            total.value = list.length;
            const start = (page.value - 1) * size.value;
            domains.value = list.slice(start, start + size.value);
          } else {
            domains.value = list;
            total.value = serverTotal;
          }
        } catch (e) {
          showToast('网络请求失败: ' + e.message, 'error');
        } finally {
          loading.value = false;
        }
      }

      async function fetchStats() {
        try {
          const r1 = await authFetch('/api/domains?size=1&is_offline=0');
          const j1 = await r1.json();
          if (j1.code === 0) stats.online = j1.data.total || 0;

          const r2 = await authFetch('/api/domains?size=1&is_offline=1');
          const j2 = await r2.json();
          if (j2.code === 0) stats.offline = j2.data.total || 0;

          const now = new Date();
          const end30 = new Date(now.getTime() + 30 * 24 * 3600 * 1000);
          const todayStr = now.toISOString().slice(0, 10);
          const endStr = end30.toISOString().slice(0, 10);

          const r3 = await authFetch('/api/domains?size=1&is_offline=0&expire_start=' + todayStr + '&expire_end=' + endStr);
          const j3 = await r3.json();
          if (j3.code === 0) stats.expiringSoon = j3.data.total || 0;

          const r4 = await authFetch('/api/domains?size=1&is_offline=0&expire_end=' + todayStr);
          const j4 = await r4.json();
          if (j4.code === 0) stats.expired = j4.data.total || 0;
        } catch (e) { /* ignore */ }
      }

      async function fetchFilterOptions() {
        try {
          const res = await authFetch('/api/domains/filter-options');
          const json = await res.json();
          if (json.code === 0) {
            platformList.value = (json.data.platforms || []).sort();
            accountOptions.value = json.data.account_options || [];
          }
        } catch (e) { /* ignore */ }
      }

      /* ---------------- 查询 / 分页 ---------------- */
      function search() {
        page.value = 1;
        pageInput.value = 1;
        fetchDomains();
      }
      function resetFilters() {
        filters.keyword = '';
        filters.platform = '';
        filters.account = '';
        filters.is_offline = '';
        filters.expire_start = '';
        filters.expire_end = '';
        colFilters.auto_renews = [];
        page.value = 1;
        pageInput.value = 1;
        fetchDomains();
      }
      function goPage(p) {
        page.value = p;
        pageInput.value = p;
        fetchDomains();
      }
      function jumpPage() {
        const p = pageInput.value;
        if (p >= 1 && p <= totalPages.value) goPage(p);
      }
      function onSizeChange() {
        page.value = 1;
        pageInput.value = 1;
        nextTick(fetchDomains);
      }
      let keywordTimer = null;
      function onKeywordInput() {
        if (keywordTimer) clearTimeout(keywordTimer);
        keywordTimer = setTimeout(() => { page.value = 1; pageInput.value = 1; fetchDomains(); }, 300);
      }
      function daysClass(days) {
        if (days < 0) return 'text-danger';
        if (days <= 30) return 'text-warning';
        return 'text-success';
      }

      /* ---------------- 同步 ---------------- */
      function openSync() {
        if (!window.CloudSyncModal) { showToast('同步模块未加载', 'error'); return; }
        window.CloudSyncModal.open({
          type: 'domains',
          typeName: '域名注册列表',
          onDone: () => { fetchDomains(); fetchStats(); fetchFilterOptions(); },
        });
      }

      /* ---------------- 详情 ---------------- */
      const detailVisible = ref(false);
      const detail = ref(null);
      const remarkSaving = ref(false);

      async function showDetail(item) {
        try {
          const res = await authFetch('/api/domains/' + encodeURIComponent(item.domain_name));
          const json = await res.json();
          if (json.code === 0) {
            detail.value = json.data;
            if (detail.value && detail.value.remark === null) detail.value.remark = '';
            detailVisible.value = true;
          } else {
            showToast(json.msg || '查询失败', 'error');
          }
        } catch (e) {
          showToast('查询详情失败', 'error');
        }
      }

      async function saveRemark() {
        if (!detail.value) return;
        remarkSaving.value = true;
        try {
          const res = await authFetch('/api/domains/' + detail.value.id + '/remark', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ remark: detail.value.remark || '' }),
          });
          const json = await res.json();
          if (json.code === 0) {
            const d = json.data || {};
            let tip = '备注已保存';
            if (d.synced) tip += '（已同步到云端）';
            else if (d.sync_msg) tip += '（' + d.sync_msg + '）';
            showToast(tip.length > 120 ? tip.slice(0, 120) + '…' : tip);
            fetchDomains();
          } else {
            showToast(json.msg || '保存失败', 'error');
          }
        } catch (e) {
          showToast('网络请求失败', 'error');
        } finally {
          remarkSaving.value = false;
        }
      }

      /* ---------------- 手动增删改 ---------------- */
      const manualVisible = ref(false);
      const manualEditing = ref(false);
      const manualEditId = ref(null);
      const manualSaving = ref(false);
      const manualForm = reactive({
        domain_name: '', platform: '', account_name: '', owner: '',
        auto_renew_enabled: false, registration_date: '', expiration_date: '',
        remark: '', is_offline: 0,
      });

      function openManual(item) {
        if (item) {
          manualEditing.value = true;
          manualEditId.value = item.id;
          manualForm.domain_name = item.domain_name || '';
          manualForm.platform = item.platform || '';
          manualForm.account_name = item.account_name || '';
          manualForm.owner = item.owner || '';
          manualForm.auto_renew_enabled = !!item.auto_renew_enabled;
          manualForm.registration_date = item.registration_date ? String(item.registration_date).slice(0, 10) : '';
          manualForm.expiration_date = item.expiration_date ? String(item.expiration_date).slice(0, 10) : '';
          manualForm.remark = item.remark || '';
          manualForm.is_offline = item.is_offline || 0;
        } else {
          manualEditing.value = false;
          manualEditId.value = null;
          manualForm.domain_name = '';
          manualForm.platform = '';
          manualForm.account_name = '';
          manualForm.owner = '';
          manualForm.auto_renew_enabled = false;
          manualForm.registration_date = '';
          manualForm.expiration_date = '';
          manualForm.remark = '';
          manualForm.is_offline = 0;
        }
        manualVisible.value = true;
      }

      async function saveManual() {
        if (!manualForm.domain_name) { showToast('域名必填', 'error'); return; }
        manualSaving.value = true;
        try {
          const url = manualEditing.value ? '/api/domains/' + manualEditId.value : '/api/domains';
          const res = await authFetch(url, {
            method: manualEditing.value ? 'PUT' : 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(manualForm),
          });
          const json = await res.json();
          if (json.code === 0) {
            showToast(manualEditing.value ? '更新成功' : '创建成功');
            manualVisible.value = false;
            fetchDomains();
            fetchStats();
            fetchFilterOptions();
          } else {
            showToast(json.msg || '操作失败', 'error');
          }
        } catch (e) {
          showToast('请求失败', 'error');
        } finally {
          manualSaving.value = false;
        }
      }

      async function deleteManual(item) {
        if (!window.confirm('确定删除域名 "' + (item.domain_name || '') + '" 吗？')) return;
        try {
          const res = await authFetch('/api/domains/' + item.id, { method: 'DELETE' });
          const json = await res.json();
          if (json.code === 0) { showToast('删除成功'); fetchDomains(); fetchStats(); }
          else showToast(json.msg || '删除失败', 'error');
        } catch (e) {
          showToast('请求失败', 'error');
        }
      }

      /* ---------------- 解析跳转（DNS 尚未拆页，先跳 SPA） ---------------- */
      function openDnsResolve(item) {
        const url = '/dns/' + encodeURIComponent(item.domain_name)
          + '?platform=' + encodeURIComponent(item.platform || '')
          + '&account=' + encodeURIComponent(item.account_name || '')
          + '&accountId=' + encodeURIComponent(item.account_id || '');
        window.location.href = url;
      }

      onMounted(() => {
        fetchFilterOptions();
        fetchDomains();
        fetchStats();
      });

      return {
        loading, domains, total, page, size, pageInput, totalPages,
        filters, stats, navDirKeyword, filteredPlatforms, accountsOfPlatform, selectFilter,
        allColumns, colVisible, visibleColumns, showColPicker, saveColPrefs,
        sortKey, sortDir, toggleSort, sortIcon,
        colFilterOpen, colFilters, toggleColFilter, toggleFilterOption, toggleAllFilter,
        isAllSelected, applyColFilter,
        fetchDomains, search, resetFilters, goPage, jumpPage, onSizeChange, onKeywordInput, daysClass,
        platformLabel, formatDate, openSync, openDnsResolve,
        detailVisible, detail, showDetail, saveRemark, remarkSaving,
        manualVisible, manualEditing, manualSaving, manualForm, openManual, saveManual, deleteManual,
      };
    },
  });
})();
