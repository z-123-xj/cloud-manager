/* CDN域名 · 独立页面业务脚本（只依赖 CloudCore） */
(function () {
  'use strict';

  const { ref, reactive, computed, onMounted } = CloudCore.Vue;
  const { authFetch, showToast, logout, formatDateTime } = CloudCore;

  const PLATFORM_LABELS = {
    aliyun: '阿里云', aws: 'AWS', tencent: '腾讯云', cloudflare: 'Cloudflare',
    huawei: '华为云', azure: 'Azure', gcp: 'Google Cloud',
  };
  function platformLabel(p) { return PLATFORM_LABELS[p] || p || '-'; }

  CloudCore.mountPage({
    key: 'cdnDomains',
    title: 'CDN域名',
    icon: 'fa-tachometer-alt',
    hasNavDir: true,
    template: document.getElementById('page-template').innerHTML,
    setup() {
      const loading = ref(false);
      const list = ref([]);
      const total = ref(0);
      const page = ref(1);
      const size = ref(20);
      const pageInput = ref(1);
      const filters = reactive({ keyword: '', platform: '', account: '', status: '运行中' });

      const platformList = ref([]);
      const accountOptions = ref([]);
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

      function statusClass(status) {
        if (status === '正常运行' || status === '运行中') return 'tag tag-success';
        if (status === '已停止' || status === '配置失败' || status === '审核未通过') return 'tag tag-offline';
        return 'tag tag-platform';
      }

      // 产品类型：cdn=CDN / dcdn=全站加速（阿里云独有）
      function productLabel(p) {
        const map = { cdn: 'CDN', dcdn: '全站加速' };
        return map[p] || (p || 'CDN');
      }
      function productClass(p) { return p === 'dcdn' ? 'tag-purple' : 'tag-platform'; }

      // SSL 证书剩余天数；null 表示无证书或到期时间无法解析
      function sslDaysLeft(expire) {
        if (!expire) return null;
        const d = new Date(expire);
        if (isNaN(d.getTime())) return null;
        return Math.ceil((d.getTime() - Date.now()) / 86400000);
      }

      // SSL剩余列排序（仅当前页），无证书的记录永远排最后
      const sslSortDir = ref('');
      const sslSortIcon = computed(() => sslSortDir.value === 'asc' ? '▲' : sslSortDir.value === 'desc' ? '▼' : '⇅');
      const sortedList = computed(() => {
        if (!sslSortDir.value) return list.value;
        const dir = sslSortDir.value === 'asc' ? 1 : -1;
        return [...list.value].sort((a, b) => {
          const da = sslDaysLeft(a.cert_expire_time);
          const db = sslDaysLeft(b.cert_expire_time);
          if (da === null && db === null) return 0;
          if (da === null) return 1;
          if (db === null) return -1;
          return (da - db) * dir;
        });
      });
      function toggleSslSort() {
        sslSortDir.value = sslSortDir.value === 'asc' ? 'desc' : 'asc';
      }

      async function fetchList() {
        loading.value = true;
        try {
          const params = new URLSearchParams();
          params.append('page', String(page.value));
          params.append('size', String(size.value));
          if (filters.keyword) params.append('keyword', filters.keyword);
          if (filters.platform) params.append('platform', filters.platform);
          if (filters.account) params.append('account_name', filters.account);
          if (filters.status) params.append('status', filters.status);

          const res = await authFetch('/api/cdn-domains?' + params.toString());
          const json = await res.json();
          if (json.code === 401) { logout(); return; }
          if (json.code === 0) {
            list.value = json.data.list || [];
            total.value = json.data.total || 0;
          }
        } catch (e) { /* 静默失败，保持列表原状 */ } finally {
          loading.value = false;
        }
      }

      async function fetchFilterOptions() {
        try {
          // 专用接口只返回 cdn_domains 表里实际有数据的平台/账号，避免混入无 CDN 资源的账号
          const res = await authFetch('/api/cdn-domains/filter-options');
          const json = await res.json();
          if (json.code === 0) {
            platformList.value = (json.data.platforms || []).sort();
            accountOptions.value = json.data.accounts || [];
          }
        } catch (e) { /* ignore */ }
      }

      function search() { page.value = 1; pageInput.value = 1; fetchList(); }
      function resetFilters() {
        filters.keyword = '';
        filters.platform = '';
        filters.account = '';
        filters.status = '运行中';
        page.value = 1;
        pageInput.value = 1;
        fetchList();
      }
      let keywordTimer = null;
      function onKeywordInput() {
        if (keywordTimer) clearTimeout(keywordTimer);
        keywordTimer = setTimeout(() => { page.value = 1; pageInput.value = 1; fetchList(); }, 300);
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

      function openSync() {
        if (!window.CloudSyncModal) { showToast('同步模块未加载', 'error'); return; }
        window.CloudSyncModal.open({
          type: 'cdn',
          typeName: 'CDN加速域名',
          onDone: () => { fetchList(); fetchFilterOptions(); },
        });
      }

      onMounted(() => { fetchFilterOptions(); fetchList(); });

      return {
        loading, list, sortedList, total, page, size, pageInput, totalPages, filters,
        navDirKeyword, filteredPlatforms, accountsOfPlatform, selectFilter,
        platformLabel, formatDateTime, statusClass, productLabel, productClass,
        sslDaysLeft, sslSortIcon, toggleSslSort, search, resetFilters,
        goPage, jumpPage, onSizeChange, openSync, onKeywordInput,
      };
    },
  });
})();
