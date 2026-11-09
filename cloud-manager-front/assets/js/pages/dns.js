/* DNS解析 · 独立页面业务脚本（只依赖 CloudCore） */
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
    key: 'dnsDomains',
    title: 'DNS解析',
    icon: 'fa-network-wired',
    hasNavDir: true,
    template: document.getElementById('page-template').innerHTML,
    setup() {
      const loading = ref(false);
      const list = ref([]);
      const total = ref(0);
      const page = ref(1);
      const size = ref(20);
      const pageInput = ref(1);
      const filters = reactive({ keyword: '', platform: '', account: '' });

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

      async function fetchList() {
        loading.value = true;
        try {
          const params = new URLSearchParams();
          params.append('page', String(page.value));
          params.append('size', String(size.value));
          if (filters.keyword) params.append('keyword', filters.keyword);
          if (filters.platform) params.append('platform', filters.platform);
          if (filters.account) params.append('account_name', filters.account);

          const res = await authFetch('/api/dns-domains?' + params.toString());
          const json = await res.json();
          if (json.code === 401) { logout(); return; }
          if (json.code === 0) {
            list.value = json.data.list || [];
            total.value = json.data.total || 0;
          } else {
            showToast(json.msg || '查询失败', 'error');
          }
        } catch (e) {
          showToast('网络请求失败', 'error');
        } finally {
          loading.value = false;
        }
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

      function search() { page.value = 1; pageInput.value = 1; fetchList(); }
      function resetFilters() {
        filters.keyword = '';
        filters.platform = '';
        filters.account = '';
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
          type: 'dns',
          typeName: 'DNS可管理域名',
          onDone: () => { page.value = 1; pageInput.value = 1; fetchList(); },
        });
      }

      // 解析记录管理暂未拆页：跳到主站 SPA 的解析页，上下文（平台/账号）一起带过去
      function openResolve(item) {
        const url = '/dns/' + encodeURIComponent(item.domain_name)
          + '?platform=' + encodeURIComponent(item.platform || '')
          + '&account=' + encodeURIComponent(item.account_name || '')
          + '&accountId=' + encodeURIComponent(item.account_id || '');
        window.location.href = url;
      }

      onMounted(() => { fetchFilterOptions(); fetchList(); });

      return {
        loading, list, total, page, size, pageInput, totalPages, filters,
        navDirKeyword, filteredPlatforms, accountsOfPlatform, selectFilter,
        platformLabel, formatDateTime, search, resetFilters, goPage, jumpPage,
        onSizeChange, openResolve, onKeywordInput, openSync,
      };
    },
  });
})();
