/* 批量执行 · 独立页面业务脚本（只依赖 CloudCore） */
(function () {
  'use strict';

  const { ref, reactive, computed, onMounted, onUnmounted, nextTick } = CloudCore.Vue;
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

      // 脚本库模式：正文由服务端读目录并渲染，前端只提交 script_id + params
      const mode = ref('manual'); // manual | library
      const scripts = ref([]);
      const selectedScriptId = ref('');
      const scriptParamValues = reactive({});
      const scriptPreview = ref('');
      const currentScript = computed(() =>
        scripts.value.find(s => s.id === selectedScriptId.value) || null);
      const scriptParams = computed(() => (currentScript.value && currentScript.value.params) || []);

      // 脚本选择：带实时搜索的下拉，输入即按名称 / 分类 / 文件名 / 说明过滤
      const scriptComboOpen = ref(false);
      const scriptComboEl = ref(null);
      const scriptSearch = ref('');
      const scriptHlIndex = ref(0);
      const scriptComboPlaceholder = computed(() => {
        const s = currentScript.value;
        return s ? (s.category + ' / ' + s.name + '（' + s.id + '）') : '输入名称 / 分类 / 文件名搜索脚本…';
      });
      const filteredScripts = computed(() => {
        const kw = scriptSearch.value.trim().toLowerCase();
        if (!kw) return scripts.value;
        return scripts.value.filter(s =>
          [s.name, s.id, s.category, s.description].join(' ').toLowerCase().includes(kw));
      });

      const submitting = ref(false);
      const lastError = ref('');
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

      // 统一响应处理：{code,msg,data}，code===0 成功；quiet 用于轮询时静默失败。
      // 失败文案同时写入 lastError，确认弹窗内也能看到，不会被弹窗遮住。
      async function api(url, opts, quiet) {
        try {
          const res = await authFetch(url, opts);
          const json = await res.json();
          if (json.code === 401) { logout(); return null; }
          if (json.code !== 0) {
            const msg = json.msg || '请求失败';
            if (!quiet) { lastError.value = msg; showToast(msg, 'error'); }
            return null;
          }
          if (!quiet) lastError.value = '';
          return json.data;
        } catch (e) {
          if (!quiet) { lastError.value = '网络请求失败'; showToast('网络请求失败', 'error'); }
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

      async function fetchScripts(preselectId) {
        const data = await api('/api/bastion/scripts');
        if (!data) return;
        scripts.value = (data.items || []).map(s => ({ ...s, params: s.params || [] }));
        const id = preselectId || selectedScriptId.value;
        if (id && scripts.value.some(s => s.id === id)) {
          selectedScriptId.value = id;
          onScriptChange();
        }
        nextTick(alignNativeForm);
      }

      function onScriptChange() {
        scriptPreview.value = '';
        Object.keys(scriptParamValues).forEach(k => delete scriptParamValues[k]);
        const s = currentScript.value;
        if (!s) return;
        (s.params || []).forEach(p => { scriptParamValues[p.key] = p.default || ''; });
        if (s.shell) form.shell = s.shell;
        if (s.timeout_sec) form.timeout_sec = s.timeout_sec;
        if (s.concurrency) form.concurrency = s.concurrency;
        if (!form.title.trim()) form.title = s.name || '';
      }

      function openScriptCombo() {
        // focus 与 click 都会调到；已展开时别重复清空关键字
        if (scriptComboOpen.value) return;
        scriptSearch.value = '';
        scriptComboOpen.value = true;
        const i = filteredScripts.value.findIndex(s => s.id === selectedScriptId.value);
        scriptHlIndex.value = i < 0 ? 0 : i;
      }
      function closeScriptCombo() {
        scriptComboOpen.value = false;
        scriptSearch.value = '';
      }
      function moveScriptHl(delta) {
        if (!scriptComboOpen.value) { openScriptCombo(); return; }
        const n = filteredScripts.value.length;
        if (!n) return;
        scriptHlIndex.value = (scriptHlIndex.value + delta + n) % n;
      }
      function pickScript(s) {
        selectedScriptId.value = s.id;
        onScriptChange();
        closeScriptCombo();
      }
      function pickHighlightedScript() {
        if (!scriptComboOpen.value) { openScriptCombo(); return; }
        const s = filteredScripts.value[scriptHlIndex.value];
        if (s) pickScript(s);
      }
      function clearScriptSelection() {
        selectedScriptId.value = '';
        onScriptChange();
        closeScriptCombo();
      }
      function onComboOutside(e) {
        const el = scriptComboEl.value;
        if (el && !el.contains(e.target)) closeScriptCombo();
      }

      async function previewScript() {
        if (!selectedScriptId.value) { showToast('请先选择脚本', 'error'); return; }
        const params = {};
        scriptParams.value.forEach(p => {
          const v = scriptParamValues[p.key];
          if (v !== undefined && v !== '') params[p.key] = String(v);
        });
        const data = await api('/api/bastion/scripts/preview?id=' + encodeURIComponent(selectedScriptId.value) +
          '&params=' + encodeURIComponent(JSON.stringify(params)));
        if (!data) return;
        scriptPreview.value = data.content || '';
        showToast('已按当前参数渲染预览');
      }

      // 只重跑失败/取消的主机：用任务里保存的渲染后正文快照，不重新读脚本库，版本可复现
      async function retryFailed(t) {
        const data = await api('/api/bastion/batch/tasks/' + t.id);
        if (!data || !data.task) return;
        const failed = (data.results || [])
          .filter(r => r.status === 'failed' || r.status === 'canceled')
          .map(r => Number(r.ecs_id)).filter(id => id > 0);
        if (!failed.length) { showToast('该任务没有失败主机', 'error'); return; }
        await Promise.all([fetchAssets(), fetchCredentials()]);
        const valid = new Set(assets.value.map(a => Number(a.id)));
        const picked = Array.from(new Set(failed)).filter(id => valid.has(id));
        if (!picked.length) { showToast('失败主机均已不在当前可用资产中，请重新选择', 'error'); return; }

        const task = data.task;
        mode.value = 'manual';
        selectedScriptId.value = '';
        scriptPreview.value = '';
        form.title = '重跑 #' + t.id + ' 失败主机';
        form.script = task.script || '';
        form.shell = task.shell || 'sh';
        form.network = task.network || 'public';
        form.concurrency = task.concurrency || 5;
        form.timeout_sec = task.timeout_sec || 300;
        const credId = Number(task.credential_id) || 0;
        form.credential_id = credentials.value.some(c => Number(c.id) === credId) ? credId : 0;
        selectedIds.value = picked;
        assetKeyword.value = '';
        accountFilter.value = '';
        window.scrollTo({ top: 0, behavior: 'smooth' });
        showToast('已恢复 #' + t.id + ' 的 ' + picked.length + ' 台失败主机，请确认后执行');
        confirmRun();
      }

      function confirmRun() {
        if (selectedIds.value.length === 0) { showToast('请先选择目标主机', 'error'); return; }
        // 凭证前置校验：没选、无可用凭证、或所选凭证已失效时先提示，不提交到后端再报错
        if (credentials.value.length === 0) {
          showToast('当前账号没有可用的 SSH 凭证，请先联系管理员在用户组管理中授权', 'error'); return;
        }
        const pickedCredId = Number(form.credential_id) || 0;
        if (!pickedCredId) { showToast('请先选择 SSH 凭证', 'error'); return; }
        if (!credentials.value.some(c => Number(c.id) === pickedCredId)) {
          form.credential_id = 0;
          showToast('所选凭证已不可用，请重新选择 SSH 凭证', 'error'); return;
        }
        if (mode.value === 'library') {
          if (!selectedScriptId.value) { showToast('请先选择脚本', 'error'); return; }
          for (const p of scriptParams.value) {
            if (p.required && !String(scriptParamValues[p.key] || '').trim()) {
              showToast('请填写必填参数: ' + (p.label || p.key), 'error'); return;
            }
          }
        } else {
          if (!form.script.trim()) { showToast('请填写命令或脚本', 'error'); return; }
          if (new Blob([form.script]).size > Number(options.value.max_script_bytes)) {
            showToast('脚本超过 ' + options.value.max_script_bytes + ' 字节上限', 'error'); return;
          }
        }
        if (selectedIds.value.length > Number(options.value.max_assets)) {
          showToast('单次最多选择 ' + options.value.max_assets + ' 台', 'error'); return;
        }
        if (Number(form.concurrency) < 1 || Number(form.concurrency) > Number(options.value.max_concurrency)) {
          showToast('并发台数需在 1 - ' + options.value.max_concurrency + ' 之间', 'error'); return;
        }
        if (Number(form.timeout_sec) < 1 || Number(form.timeout_sec) > Number(options.value.max_timeout_sec)) {
          showToast('超时需在 1 - ' + options.value.max_timeout_sec + ' 秒之间', 'error'); return;
        }
        lastError.value = '';
        confirmVisible.value = true;
      }

      async function doRun() {
        if (submitting.value) return;
        if (!Number(form.credential_id)) {
          lastError.value = '请先选择 SSH 凭证';
          showToast('请先选择 SSH 凭证', 'error');
          return;
        }
        lastError.value = '';
        submitting.value = true;
        const payload = {
          title: form.title.trim(),
          ecs_ids: selectedIds.value.map(Number),
          credential_id: Number(form.credential_id) || 0,
          network: form.network,
          shell: form.shell,
          concurrency: Number(form.concurrency) || 5,
          timeout_sec: Number(form.timeout_sec) || 300,
        };
        if (mode.value === 'library') {
          payload.script_id = selectedScriptId.value;
          const params = {};
          scriptParams.value.forEach(p => {
            const v = scriptParamValues[p.key];
            if (v !== undefined && v !== '') params[p.key] = String(v);
          });
          payload.params = params;
        } else {
          payload.script = form.script;
        }
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

      function clampNum(v, min, max, fallback) {
        const n = Number(v);
        if (!Number.isFinite(n) || n < 1) return fallback;
        return Math.max(min, Math.min(max, Math.round(n)));
      }

      // 把历史任务的配置和主机恢复回表单，方便直接重跑；只填表单，不自动提交
      async function applyHistory(t) {
        await Promise.all([fetchAssets(), fetchCredentials()]);
        const data = await api('/api/bastion/batch/tasks/' + t.id);
        if (!data || !data.task) return;
        const task = data.task;
        const results = Array.isArray(data.results) ? data.results : [];

        // 历史里的主机可能已下线或不再授权给当前用户，只保留当前可用资产
        const valid = new Set(assets.value.map(a => Number(a.id)));
        const wanted = Array.from(new Set(results.map(r => Number(r.ecs_id)).filter(id => id > 0)));
        const picked = wanted.filter(id => valid.has(id));
        const skipped = wanted.length - picked.length;

        const notes = [];
        const credId = Number(task.credential_id) || 0;
        let credentialId = 0;
        if (credId && credentials.value.some(c => Number(c.id) === credId)) {
          credentialId = credId;
        } else if (credId) {
          notes.push('原凭证已不可用，请重新选择');
        }
        if (skipped > 0) notes.push(skipped + ' 台主机已不在可用资产中，已跳过');

        // 历史任务恢复的是渲染后正文快照，切回手写模式避免误读脚本库最新版本
        mode.value = 'manual';
        selectedScriptId.value = '';
        scriptPreview.value = '';
        form.title = task.title || '';
        form.script = task.script || '';
        form.shell = (options.value.shells || []).indexOf(task.shell) >= 0 ? task.shell : 'sh';
        form.network = ['public', 'eip', 'private'].indexOf(task.network) >= 0 ? task.network : 'public';
        form.credential_id = credentialId;
        form.concurrency = clampNum(task.concurrency, 1, Number(options.value.max_concurrency) || 20, 5);
        form.timeout_sec = clampNum(task.timeout_sec, 1, Number(options.value.max_timeout_sec) || 3600, 300);
        selectedIds.value = picked;
        assetKeyword.value = '';
        accountFilter.value = '';
        lastError.value = '';

        window.scrollTo({ top: 0, behavior: 'smooth' });
        const msg = picked.length === 0
          ? '已应用 #' + t.id + ' 的配置，但原主机均不可用，请重新选择主机'
          : '已应用 #' + t.id + ' 的配置，选中 ' + picked.length + ' 台主机' +
            (notes.length ? '（' + notes.join('；') + '）' : '');
        showToast(msg, picked.length === 0 ? 'error' : 'success');
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

      // history.back() 回来时，浏览器会还原原生表单控件（radio/select/textarea）：
      // bfcache 下 Vue 状态还在但 DOM 被改写；重新加载时 Vue 状态整个重置，DOM 却仍是旧值。
      // 统一处理：离开前存一份快照，回来后恢复状态并把原生控件对齐到 Vue 状态。
      const FORM_STATE_KEY = 'cloud_batch_exec_state';

      function saveFormState() {
        try {
          sessionStorage.setItem(FORM_STATE_KEY, JSON.stringify({
            mode: mode.value,
            selectedScriptId: selectedScriptId.value,
            scriptParamValues: { ...scriptParamValues },
            form: { ...form },
            selectedIds: selectedIds.value,
          }));
        } catch (e) { /* 隐私模式或配额不足时忽略 */ }
      }

      function restoreFormState() {
        try {
          const raw = sessionStorage.getItem(FORM_STATE_KEY);
          const s = raw ? JSON.parse(raw) : null;
          return s && typeof s === 'object' ? s : null;
        } catch (e) { return null; }
      }

      function alignNativeForm() {
        document.querySelectorAll('input[type="radio"][name="bx-script-source"]').forEach(el => {
          el.checked = el.value === mode.value;
        });
        // 搜索框由 scriptSearch 管理，别让浏览器把上次的搜索词恢复回来
        const sel = document.querySelector('.bx-script-select');
        if (sel) sel.value = scriptSearch.value;
      }

      function onPageShow() {
        // 普通返回也会恢复表单，不能只看 bfcache 的 persisted；
        // 延迟到下一个宏任务，确保晚于浏览器的表单状态恢复。
        scriptComboOpen.value = false;
        nextTick(alignNativeForm);
        setTimeout(alignNativeForm, 0);
      }
      window.addEventListener('pagehide', saveFormState);
      window.addEventListener('pageshow', onPageShow);
      window.addEventListener('mousedown', onComboOutside);

      onMounted(() => {
        const pre = new URLSearchParams(window.location.search).get('script');
        // 带 ?script= 的「去部署」按新脚本预选，不继承旧表单；其余情况恢复上次状态
        const saved = pre ? null : restoreFormState();
        if (saved) {
          if (saved.mode === 'manual' || saved.mode === 'library') mode.value = saved.mode;
          if (Array.isArray(saved.selectedIds)) selectedIds.value = saved.selectedIds;
          if (saved.form && typeof saved.form === 'object') Object.assign(form, saved.form);
          if (saved.selectedScriptId) selectedScriptId.value = saved.selectedScriptId;
        }
        fetchOptions();
        fetchAssets();
        fetchCredentials();
        loadHistory();
        if (pre) mode.value = 'library';
        fetchScripts(pre || (saved && saved.selectedScriptId) || '').then(() => {
          // fetchScripts 的 onScriptChange 会把参数重置成默认值，这里覆盖回离开前填的值
          if (saved && saved.scriptParamValues && typeof saved.scriptParamValues === 'object') {
            Object.keys(scriptParamValues).forEach(k => delete scriptParamValues[k]);
            Object.assign(scriptParamValues, saved.scriptParamValues);
          }
        });
      });
      onUnmounted(() => {
        stopPolling();
        window.removeEventListener('pagehide', saveFormState);
        window.removeEventListener('pageshow', onPageShow);
        window.removeEventListener('mousedown', onComboOutside);
      });

      return {
        assets, assetsLoading, assetKeyword, accountFilter, accountOptions,
        selectedIds, filteredAssets, hostsOpen,
        isSelected, toggleHost, clearSelection, selectAllFiltered,
        credentials, options, form, displayIp, fetchAssets,
        submitting, lastError, confirmVisible, confirmRun, doRun, cancelTask,
        mode, scripts, selectedScriptId, scriptParamValues, scriptPreview,
        currentScript, scriptParams, fetchScripts, onScriptChange, previewScript, retryFailed,
        scriptComboOpen, scriptComboEl, scriptSearch, scriptHlIndex,
        scriptComboPlaceholder, filteredScripts, openScriptCombo, closeScriptCombo,
        moveScriptHl, pickScript, pickHighlightedScript, clearScriptSelection,
        activeTask, results, progressPct,
        history, historyLoading, historyTotal, historyPage, historyTotalPages,
        loadHistory, goHistoryPage, openHistory, applyHistory,
        detailVisible, detailResult, openResult, copyOutput,
        statusLabel, statusTone, formatDuration, formatDateTime,
      };
    },
  });
})();
