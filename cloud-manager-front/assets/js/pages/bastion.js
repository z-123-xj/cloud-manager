/* 堡垒机资产 · 独立页面业务脚本（只依赖 CloudCore） */
(function () {
  'use strict';

  const { ref, reactive, computed, watch, onMounted } = CloudCore.Vue;
  const { authFetch, showToast, logout, formatDate } = CloudCore;

  const PLATFORM_LABELS = {
    aliyun: '阿里云', aws: 'AWS', tencent: '腾讯云', cloudflare: 'Cloudflare',
    huawei: '华为云', azure: 'Azure', gcp: 'Google Cloud',
  };
  function platformLabel(p) { return PLATFORM_LABELS[p] || p || '-'; }

  CloudCore.mountPage({
    key: 'bastionAssets',
    title: '堡垒机资产',
    icon: 'fa-shield-alt',
    hasNavDir: true,
    template: document.getElementById('page-template').innerHTML,
    setup() {
      const loading = ref(false);
      const list = ref([]);
      const filters = reactive({ keyword: '', platform: '', account: '', region: '' });
      const navDirKeyword = ref('');

      const platformList = computed(() => {
        return Array.from(new Set(list.value.map(a => a.platform).filter(Boolean))).sort();
      });
      const regions = computed(() => {
        let src = list.value;
        if (filters.platform) src = src.filter(a => a.platform === filters.platform);
        if (filters.account) src = src.filter(a => a.account_name === filters.account);
        return Array.from(new Set(src.map(a => a.region_name).filter(Boolean))).sort();
      });

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
        const set = new Set();
        list.value.forEach(a => {
          if (a.platform === pf && a.account_name) set.add(a.account_name);
        });
        return Array.from(set).sort().filter(name => !kw || name.toLowerCase().includes(kw));
      }

      function selectFilter(platform, account) {
        filters.platform = platform;
        filters.account = account;
        filters.region = '';
      }

      const filteredList = computed(() => {
        const kw = filters.keyword.trim().toLowerCase();
        return list.value.filter(a => {
          if (filters.platform && a.platform !== filters.platform) return false;
          if (filters.account && a.account_name !== filters.account) return false;
          if (filters.region && a.region_name !== filters.region) return false;
          if (!kw) return true;
          return [a.instance_name, a.instance_id, a.public_ip_address, a.private_ip_address, a.eip_address]
            .filter(Boolean).some(v => String(v).toLowerCase().includes(kw));
        });
      });

      // 客户端分页：/api/bastion/assets 一次返回全部已授权资产，这里按页切片
      const page = ref(1);
      const size = ref(20);
      const pageInput = ref(1);
      const total = computed(() => filteredList.value.length);
      const totalPages = computed(() => Math.ceil(total.value / size.value) || 1);
      const pagedList = computed(() => {
        const start = (page.value - 1) * size.value;
        return filteredList.value.slice(start, start + size.value);
      });
      watch(filteredList, () => { page.value = 1; pageInput.value = 1; });
      function goPage(p) {
        if (p < 1 || p > totalPages.value) return;
        page.value = p;
        pageInput.value = p;
      }
      function jumpPage() {
        const p = parseInt(pageInput.value, 10);
        if (p >= 1 && p <= totalPages.value) goPage(p);
        else pageInput.value = page.value;
      }
      function onSizeChange() { page.value = 1; pageInput.value = 1; }

      function statusLabel(status) {
        const map = { Running: '运行中', Stopped: '已停止', Starting: '启动中', Stopping: '停止中' };
        return map[status] || status || '-';
      }
      function dotClass(status) {
        const map = { Running: 'dot-online', Stopped: 'dot-offline', Starting: 'dot-warning', Stopping: 'dot-warning' };
        return map[status] || 'dot-offline';
      }

      async function fetchAssets() {
        loading.value = true;
        try {
          const res = await authFetch('/api/bastion/assets');
          const json = await res.json();
          if (json.code === 401) { logout(); return; }
          if (json.code === 0) list.value = json.data || [];
          else showToast(json.msg || '获取资产失败', 'error');
        } catch (e) {
          showToast('获取资产失败', 'error');
        } finally {
          loading.value = false;
        }
      }

      function resetFilters() {
        filters.keyword = '';
        filters.platform = '';
        filters.account = '';
        filters.region = '';
      }

      function noop() {}

      // ============ SSH 连接弹窗（与主站 connectBastion / doConnectBastion 行为一致）============
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

      onMounted(fetchAssets);

      return {
        loading, list, filters, navDirKeyword, platformList, regions, filteredPlatforms,
        accountsOfPlatform, selectFilter, filteredList, platformLabel, formatDate,
        statusLabel, dotClass, resetFilters, noop, fetchAssets,
        page, size, pageInput, total, totalPages, pagedList, goPage, jumpPage, onSizeChange,
        connectVisible, connectLoading, connectAsset, connectNetwork,
        connectCredId, connectCredList, connectCredDrpOpen,
        credLabel, closeConnect, connect, doConnect,
      };
    },
  });
})();
