/* ECS实例 · 独立页面业务脚本（只依赖 CloudCore） */
(function () {
  'use strict';

  const { ref, reactive, computed, onMounted, nextTick } = CloudCore.Vue;
  const { authFetch, showToast, logout, formatDate } = CloudCore;

  const PLATFORM_LABELS = {
    aliyun: '阿里云', aws: 'AWS', tencent: '腾讯云', cloudflare: 'Cloudflare',
    huawei: '华为云', azure: 'Azure', gcp: 'Google Cloud',
  };
  function platformLabel(p) { return PLATFORM_LABELS[p] || p || '-'; }

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

      onMounted(() => { fetchFilterOptions(); fetchList(); });

      return {
        loading, list, total, page, size, pageInput, totalPages, filters, regions,
        navDirKeyword, filteredPlatforms, accountsOfPlatform, selectFilter,
        platformLabel, formatDate, statusLabel, dotClass, search, resetFilters,
        goPage, jumpPage, onSizeChange, openSync, onKeywordInput,
        ecsManualModalVisible, ecsManualEditing, ecsManualSaving, ecsManualForm,
        openEcsManualModal, saveEcsManual,
        ecsInlineEdit, ecsInlineValue, startEcsInlineEdit, saveEcsInlineEdit, cancelEcsInlineEdit,
      };
    },
  });
})();
