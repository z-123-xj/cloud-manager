/* 批量执行 · 独立页面业务脚本（只依赖 CloudCore） */
(function () {
  'use strict';

  const { ref, reactive, computed, onMounted, onUnmounted } = CloudCore.Vue;
  const { authFetch, showToast, logout, formatDateTime, copyText } = CloudCore;

  const STATUS_LABELS = {
    pending: '等待中', running: '执行中', success: '成功', failed: '失败', canceled: '已取消',
    succeeded: '全部成功', partial: '部分失败',
  };
  const PLATFORM_LABELS = {
    aliyun: '阿里云', aws: 'AWS', tencent: '腾讯云', huawei: '华为云',
    cloudflare: 'Cloudflare', azure: 'Azure', gcp: 'Google Cloud',
  };
  function platformLabel(p) { return PLATFORM_LABELS[p] || p || ''; }

  CloudCore.mountPage({
    key: 'batchExec',
    title: '批量执行',
    icon: 'fa-bolt',
    hasNavDir: false,
    template: document.getElementById('page-template').innerHTML,
    setup() {
      const assets = ref([]);
      const assetsLoading = ref(false);
      const assetKeyword = ref('');
      const accountFilter = ref('');
      const selectedIds = ref([]);
      const hostsOpen = ref(true);
      const credentials = ref([]);
      const options = ref({
        shells: ['sh'], networks: ['public'],
        max_concurrency: 20, max_timeout_sec: 3600,
        max_script_bytes: 32768, max_output_bytes: 16384, max_assets: 200,
      });
      const form = reactive({
        title: '', script: '', shell: 'sh', network: 'public',
        credential_id: 0, concurrency: 5, timeout_sec: 300,
      });

      const submitting = ref(false);
      const confirmVisible = ref(false);
      const detailVisible = ref(false);
      const detailResult = ref({});
      const activeTask = ref(null);
      const results = ref([]);

      const history = ref([]);
      const historyLoading = ref(false);
      const historyTotal = ref(0);
      const historyPage = ref(1);
      const historySize = 10;
      const historyTotalPages = computed(() => Math.ceil(historyTotal.value / historySize) || 1);

      let pollTimer = null;

      // 账号下拉以 account_id 为唯一值，避免不同平台下的同名账号串在一起
      const accountOptions = computed(() => {
        const map = new Map();
        assets.value.forEach(a => {
          const value = String(a.account_id || a.account_name || '');
          if (!value || map.has(value)) return;
          const pf = platformLabel(a.platform);
          map.set(value, {
            value,
            label: (a.account_name || value) + (pf ? ' · ' + pf : ''),
          });
        });
        return Array.from(map.values()).sort((x, y) => x.label.localeCompare(y.label, 'zh-CN'));
      });

      const filteredAssets = computed(() => {
        const kw = assetKeyword.value.trim().toLowerCase();
        const acc = accountFilter.value;
        return assets.value.filter(a => {
          if (acc && String(a.account_id || a.account_name || '') !== acc) return false;
          if (!kw) return true;
          return [
            a.instance_name, a.instance_id, a.public_ip_address, a.eip_address,
            a.private_ip_address, a.platform, a.account_name,
          ].some(v => String(v || '').toLowerCase().includes(kw));
        });
      });

      const progressPct = computed(() => {
        if (!activeTask.value || !activeTask.value.total) return 0;
        const done = results.value.filter(r => r.status !== 'pending' && r.status !== 'running').length;
        return Math.round(done / activeTask.value.total * 100);
      });

      function displayIp(a) {
        return a.public_ip_address || a.eip_address || a.private_ip_address || '无IP';
      }
      function statusLabel(s) { return STATUS_LABELS[s] || s || '-'; }
      function statusTone(s) {
        if (s === 'success' || s === 'succeeded') return 'ok';
        if (s === 'failed' || s === 'partial') return 'fail';
        if (s === 'running' || s === 'pending') return 'run';
        return 'mute';
      }
      function formatDuration(ms) {
        const n = Number(ms || 0);
        if (!n) return '-';
        return n < 1000 ? n + ' ms' : (n / 1000).toFixed(2) + ' s';
      }

      // 统一响应处理：{code,msg,data}，code===0 成功；quiet 用于轮询时静默失败
      async function api(url, opts, quiet) {
        try {
          const res = await authFetch(url, opts);
          const json = await res.json();
          if (json.code === 401) { logout(); return null; }
          if (json.code !== 0) {
            if (!quiet) showToast(json.msg || '请求失败', 'error');
            return null;
          }
          return json.data;
        } catch (e) {
          if (!quiet) showToast('网络请求失败', 'error');
          return null;
        }
      }

      async function fetchOptions() {
        const data = await api('/api/bastion/batch/options');
        if (!data) return;
        options.value = data;
        if (!options.value.shells || !options.value.shells.length) options.value.shells = ['sh'];
        if (Number(form.concurrency) > Number(options.value.max_concurrency)) form.concurrency = options.value.max_concurrency;
      }

      async function fetchAssets() {
        assetsLoading.value = true;
        try {
          const data = await api('/api/bastion/assets');
          if (!data) return;
          assets.value = Array.isArray(data) ? data : [];
          const valid = new Set(assets.value.map(a => Number(a.id)));
          selectedIds.value = selectedIds.value.filter(id => valid.has(Number(id)));
          if (accountFilter.value && !accountOptions.value.some(o => o.value === accountFilter.value)) {
            accountFilter.value = '';
          }
        } finally {
          assetsLoading.value = false;
        }
      }

      async function fetchCredentials() {
        const data = await api('/api/bastion/credentials');
        credentials.value = Array.isArray(data) ? data : [];
      }

      async function loadHistory() {
        historyLoading.value = true;
        try {
          const data = await api('/api/bastion/batch/tasks?page=' + historyPage.value + '&size=' + historySize);
          if (!data) return;
          history.value = data.list || [];
          historyTotal.value = data.total || 0;
        } finally {
          historyLoading.value = false;
        }
      }

      function goHistoryPage(p) {
        if (p < 1 || p > historyTotalPages.value) return;
        historyPage.value = p;
        loadHistory();
      }

      function stopPolling() {
        if (pollTimer) { clearInterval(pollTimer); pollTimer = null; }
      }

      async function loadTask(id, quiet) {
        const data = await api('/api/bastion/batch/tasks/' + id, undefined, quiet);
        if (!data) return;
        activeTask.value = data.task;
        results.value = data.results || [];
        if (!activeTask.value || activeTask.value.status !== 'running') {
          stopPolling();
          loadHistory();
        }
      }

      function startPolling(id) {
        stopPolling();
        pollTimer = setInterval(() => {
          if (activeTask.value && activeTask.value.status === 'running') loadTask(id, true);
          else stopPolling();
        }, 1000);
      }

      function confirmRun() {
        if (selectedIds.value.length === 0) { showToast('请先选择目标主机', 'error'); return; }
        if (!form.script.trim()) { showToast('请填写命令或脚本', 'error'); return; }
        if (selectedIds.value.length > Number(options.value.max_assets)) {
          showToast('单次最多选择 ' + options.value.max_assets + ' 台', 'error'); return;
        }
        if (new Blob([form.script]).size > Number(options.value.max_script_bytes)) {
          showToast('脚本超过 ' + options.value.max_script_bytes + ' 字节上限', 'error'); return;
        }
        if (Number(form.concurrency) < 1 || Number(form.concurrency) > Number(options.value.max_concurrency)) {
          showToast('并发台数需在 1 - ' + options.value.max_concurrency + ' 之间', 'error'); return;
        }
        if (Number(form.timeout_sec) < 1 || Number(form.timeout_sec) > Number(options.value.max_timeout_sec)) {
          showToast('超时需在 1 - ' + options.value.max_timeout_sec + ' 秒之间', 'error'); return;
        }
        confirmVisible.value = true;
      }

      async function doRun() {
        if (submitting.value) return;
        submitting.value = true;
        const payload = {
          title: form.title.trim(),
          ecs_ids: selectedIds.value.map(Number),
          credential_id: Number(form.credential_id) || 0,
          network: form.network,
          script: form.script,
          shell: form.shell,
          concurrency: Number(form.concurrency) || 5,
          timeout_sec: Number(form.timeout_sec) || 300,
        };
        const data = await api('/api/bastion/batch/tasks', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        });
        submitting.value = false;
        if (!data) return;
        confirmVisible.value = false;
        showToast('任务已提交');
        await loadTask(data.task_id);
        if (activeTask.value && activeTask.value.status === 'running') startPolling(data.task_id);
      }

      async function cancelTask() {
        if (!activeTask.value) return;
        const id = activeTask.value.id;
        const data = await api('/api/bastion/batch/tasks/' + id + '/cancel', { method: 'POST' });
        if (!data) return;
        showToast('已请求停止，正在等待已开始的主机结束');
        await loadTask(id);
      }

      function openHistory(t) {
        loadTask(t.id);
        window.scrollTo({ top: 0, behavior: 'smooth' });
      }

      function openResult(r) {
        detailResult.value = r;
        detailVisible.value = true;
      }

      function copyOutput() {
        const r = detailResult.value;
        const text = [r.stdout || '', r.stderr || ''].filter(Boolean).join('\n');
        if (!text) { showToast('没有可复制的输出', 'error'); return; }
        copyText(text);
        showToast('已复制输出');
      }

      function isSelected(id) {
        return selectedIds.value.some(x => Number(x) === Number(id));
      }
      function toggleHost(id) {
        const n = Number(id);
        if (isSelected(n)) selectedIds.value = selectedIds.value.filter(x => Number(x) !== n);
        else selectedIds.value = selectedIds.value.concat([n]);
      }
      function clearSelection() { selectedIds.value = []; }
      function selectAllFiltered() {
        const ids = new Set(selectedIds.value.map(Number));
        filteredAssets.value.forEach(a => ids.add(Number(a.id)));
        selectedIds.value = Array.from(ids);
      }

      onMounted(() => {
        fetchOptions();
        fetchAssets();
        fetchCredentials();
        loadHistory();
      });
      onUnmounted(stopPolling);

      return {
        assets, assetsLoading, assetKeyword, accountFilter, accountOptions,
        selectedIds, filteredAssets, hostsOpen,
        isSelected, toggleHost, clearSelection, selectAllFiltered,
        credentials, options, form, displayIp, fetchAssets,
        submitting, confirmVisible, confirmRun, doRun, cancelTask,
        activeTask, results, progressPct,
        history, historyLoading, historyTotal, historyPage, historyTotalPages,
        loadHistory, goHistoryPage, openHistory,
        detailVisible, detailResult, openResult, copyOutput,
        statusLabel, statusTone, formatDuration, formatDateTime,
      };
    },
  });
})();
