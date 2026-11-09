/* 日志排查 · 独立页面业务脚本
 * 顶栏 / 导航抽屉 / 全局搜索 / 会话由 core.js 的公共 LAYOUT 统一提供，
 * 本文件只保留日志分析业务，不再单独维护一份顶栏状态。 */
const { ref, computed, onMounted, onUnmounted } = CloudCore.Vue;
const { authFetch } = CloudCore;

// 后端未启动、仍是旧二进制（缺 /api/bastion/logs/* 路由，被 NoRoute 回退成 index.html）、
// 或网关 502 时，响应是 HTML / 空 body。直接 res.json() 会抛 SyntaxError，
// 之前被上层 catch 一律显示成「网络请求失败」，无法定位真正原因。
async function readApiJson(res, label) {
  const text = await res.text();
  try {
    return JSON.parse(text);
  } catch (e) {
    if (res.status === 404 || /^\s*<(?:!doctype|html)/i.test(text || '')) {
      throw new Error(label + '失败：接口返回了页面而非 JSON（HTTP ' + res.status + '）。通常是后端未启动，或仍是缺少 /api/bastion/logs/* 路由的旧版本，请重新编译并重启后端');
    }
    const snippet = (text || '').replace(/\s+/g, ' ').trim().slice(0, 100);
    throw new Error(label + '失败：后端返回非 JSON 响应（HTTP ' + res.status + '）' + (snippet ? '：' + snippet : ''));
  }
}
function apiErrorText(e) {
  const m = e && (e.message || String(e));
  if (!m) return '网络请求失败（未知错误）';
  if (/Failed to fetch|NetworkError|Load failed|fetch failed/i.test(m)) {
    return '无法连接后端，请确认服务已启动（' + m + '）';
  }
  return m;
}

const logDebugCmd = ref('');
const logDebugCopied = ref(false);

CloudCore.mountPage({
  key: 'logs',
  title: '日志排查',
  icon: 'fa-search',
  hasNavDir: false,
  template: document.getElementById('page-template').innerHTML,
  setup() {
    // ============ 服务器 / 凭证 / 过滤条件 ============
    const logAssets = ref([]);
    const logAssetId = ref(0);
    const logNetwork = ref('public');
    const logCredId = ref(0);
    const logCreds = ref([]);
    const logCurrentDir = ref('/usr/local/nginx/logs');
    const logBackupDir = ref('/data/backup/nginx_log');
    const logScriptPath = ref('/usr/local/sbin/nginx_analyzer.sh');
    const logFilterIp = ref('');
    const logFilterUrl = ref('');
    const logFilterStatus = ref('');
    const logStart = ref('');
    const logEnd = ref('');
    const activeLogRange = ref('today');
    const logResult = ref(null);
    const logLoading = ref(false);
    const logTaskStatus = ref('');
    const logElapsed = ref(0);
    const logError = ref('');
    const logAssetSearch = ref('');
    const logAssetComboOpen = ref(false);

    // 第一步「路径探查」的结果
    const logExploring = ref(false);
    const logExploreError = ref('');
    const logDomains = ref([]);
    const logSelectedDomains = ref([]);
    const logFormat = ref('');
    const logFormatName = ref('');
    const logFormatSource = ref('default');
    const logFormatWarning = ref('');
    const logScriptExists = ref(false);
    const logScriptSynced = ref(false);
    const logActiveDomain = ref('');

    // 「备份 + 预生成 + 分析」闭环状态（来自 /logs/explore 的只读探查）
    const logExplored = ref(false);
    const logPreparedDir = ref('/data/backup/nginx_log/analysis');
    const logPreparedFiles = ref(0);
    const logRotatePath = ref('/usr/local/sbin/nginx_log_rotate.sh');
    const logRotateExists = ref(false);
    const logRotateSynced = ref(false);
    const logRotateCron = ref(false);

    // 新脚本输出：data.global_stats + data.domain_stats
    const logGlobal = computed(() => (logResult.value && logResult.value.global_stats) || {});
    const logDomainStats = computed(() => (logResult.value && logResult.value.domain_stats) || []);
    const logActiveDomainStat = computed(() =>
      logDomainStats.value.find(d => d.domain === logActiveDomain.value) || logDomainStats.value[0] || null);

    // 兼容旧版脚本：备份文件名未被剥离时 domain 会变成 xxx.access.log.20261107.gz，
    // 在结果入口做一次规范化，避免看板出现文件名前缀、同一域名被拆成多条。
    function cleanDomainName(s) {
      return String(s || '')
        .replace(/\.gz$/, '')
        .replace(/\.[0-9]{8,14}$/, '')
        .replace(/\.(access|error)\.log$/, '')
        .replace(/\.log$/, '');
    }
    function rowKey(r) {
      return [r.url, r.ip, r.status, r.method].filter(v => v !== undefined).join('|');
    }
    function mergeRows(a, b) {
      const acc = new Map();
      (Array.isArray(a) ? a : []).forEach(r => acc.set(rowKey(r), Object.assign({}, r)));
      (Array.isArray(b) ? b : []).forEach(r => {
        const k = rowKey(r);
        const prev = acc.get(k);
        if (!prev) { acc.set(k, Object.assign({}, r)); return; }
        ['count', 'traffic'].forEach(f => {
          if (typeof r[f] === 'number') prev[f] = (prev[f] || 0) + r[f];
        });
      });
      return [...acc.values()]
        .sort((x, y) => Number(y.count ?? y.traffic ?? 0) - Number(x.count ?? x.traffic ?? 0))
        .slice(0, 10);
    }
    function normalizeLogResult(data) {
      const out = JSON.parse(JSON.stringify(data || {}));
      const fixUrl = (u) => {
        const s = String(u || '');
        const idx = s.indexOf(' ');
        if (idx < 0) return s;
        const head = s.slice(0, idx);
        return /\.log|\.gz/.test(head) ? cleanDomainName(head) + s.slice(idx) : s;
      };
      const walk = (o) => {
        if (Array.isArray(o)) { o.forEach(walk); return; }
        if (!o || typeof o !== 'object') return;
        if (typeof o.domain === 'string') o.domain = cleanDomainName(o.domain);
        if (typeof o.url === 'string') o.url = fixUrl(o.url);
        Object.keys(o).forEach(k => walk(o[k]));
      };
      walk(out);
      if (Array.isArray(out.domain_stats)) {
        const map = new Map();
        out.domain_stats.forEach(s => {
          if (!map.has(s.domain)) { map.set(s.domain, s); return; }
          const t = map.get(s.domain);
          t.total_requests = (Number(t.total_requests) || 0) + (Number(s.total_requests) || 0);
          ['top_urls_hits', 'top_urls_traffic', 'top_ips', 'status_distribution', 'method_distribution']
            .forEach(k => { t[k] = mergeRows(t[k], s[k]); });
        });
        out.domain_stats = [...map.values()];
      }
      return out;
    }

    // 脚本返回空结果时给出可行动的原因，而不是六个卡片各显示「无数据」
    const logWarning = computed(() => {
      if (!logResult.value) return '';
      const g = logGlobal.value || {};
      const hasGlobal = Object.keys(g).some(k => Array.isArray(g[k]) && g[k].length);
      if (hasGlobal || logDomainStats.value.length) return '';
      return '本次分析没有匹配到任何日志行：请确认所选时间段与域名。未选时间时脚本会扫描全部切割备份（预生成 TSV.gz + 历史原始 .gz）；仍无数据请检查日志文件命名与所选域名。';
    });

    function trendBarHeight(v) {
      const rows = logGlobal.value.hourly_trend || [];
      const max = Math.max(1, ...rows.map(r => Number(r.count) || 0));
      return Math.max(6, Math.round((Number(v) || 0) / max * 78)) + 'px';
    }
    function trendHourLabel(h) {
      const s = String(h || '');
      const m = s.match(/:(\d{2})\]?$/);
      return m ? m[1] + '时' : s.slice(-8);
    }

    // ============ 第一步：路径探查 ============
    async function fetchLogAssets() {
      try {
        const res = await authFetch('/api/bastion/assets');
        const json = await readApiJson(res, '服务器列表加载');
        if (json.code === 0) logAssets.value = json.data || [];
      } catch (e) { logError.value = apiErrorText(e); }
    }

    async function onLogAssetChange() {
      logCredId.value = 0;
      logCreds.value = [];
      if (!logAssetId.value) return;
      const asset = logAssets.value.find(a => a.id === logAssetId.value);
      if (asset) {
        if (asset.public_ip_address) logNetwork.value = 'public';
        else if (asset.eip_address) logNetwork.value = 'eip';
        else if (asset.private_ip_address) logNetwork.value = 'private';
      }
      try {
        const res = await authFetch('/api/bastion/assets/' + logAssetId.value + '/credentials');
        const json = await res.json();
        if (json.code === 0) {
          logCreds.value = json.data || [];
          if (logCreds.value.length) logCredId.value = logCreds.value[0].id;
        }
      } catch (e) { /* ignore */ }
    }

    async function exploreLogs() {
      if (!logAssetId.value) { logExploreError.value = '请先选择服务器'; return; }
      if (!logCredId.value) { logExploreError.value = '该资产无可用凭证，请先到“凭证”中添加'; return; }
      logExploring.value = true;
      logExploreError.value = '';
      try {
        const res = await authFetch('/api/bastion/logs/explore', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            ecs_id: logAssetId.value, network: logNetwork.value,
            credential_id: logCredId.value,
            script_path: (logScriptPath.value || '').trim(),
          }),
        });
        const json = await readApiJson(res, '路径探查');
        if (json.code !== 0) { logExploreError.value = json.msg || '路径探查失败'; return; }
        const d = json.data || {};
        logDomains.value = d.domains || [];
        logSelectedDomains.value = logDomains.value.slice();
        logCurrentDir.value = d.current_dir || logCurrentDir.value;
        logBackupDir.value = d.backup_dir || logBackupDir.value;
        logFormat.value = d.format || '';
        logFormatName.value = d.format_name || '';
        logFormatSource.value = d.format_source || 'default';
        logFormatWarning.value = d.format_warning || '';
        logScriptExists.value = !!d.script_exists;
        logScriptSynced.value = !!d.script_synced;
        if (d.script_path) logScriptPath.value = d.script_path;
        logPreparedDir.value = d.prepared_dir || logPreparedDir.value;
        logPreparedFiles.value = Number(d.prepared_files) || 0;
        logRotatePath.value = d.rotate_script_path || logRotatePath.value;
        logRotateExists.value = !!d.rotate_exists;
        logRotateSynced.value = !!d.rotate_synced;
        logRotateCron.value = !!d.rotate_cron;
        logExplored.value = true;
        if (!logDomains.value.length) logExploreError.value = '未发现匹配的日志文件，请确认服务器上的日志目录与切割备份';
      } catch (e) {
        logExploreError.value = apiErrorText(e);
      } finally {
        logExploring.value = false;
      }
    }

    function selectAllLogDomains() { logSelectedDomains.value = logDomains.value.slice(); }
    function invertLogDomains() {
      logSelectedDomains.value = logDomains.value.filter(n => !logSelectedDomains.value.includes(n));
    }
    function markCustomRange() { activeLogRange.value = 'custom'; }

    // ============ 第二步：提交异步分析任务并轮询 ============
    let logPollTimer = null;
    let logSubmitAt = 0;
    function pollLogTask(id, tries) {
      tries = tries || 0;
      logTaskStatus.value = '分析中... ' + tries + 's';
      clearTimeout(logPollTimer);
      logPollTimer = setTimeout(async () => {
        try {
          const res = await authFetch('/api/bastion/logs/task/' + id);
          const json = await readApiJson(res, '任务查询');
          if (json.code !== 0) {
            logError.value = json.msg || '任务查询失败';
            logLoading.value = false; logTaskStatus.value = ''; return;
          }
          const t = json.data || {};
          if (t.status === 'done') {
            logElapsed.value = Date.now() - logSubmitAt;
            logResult.value = normalizeLogResult(t.data || {});
            const ds = logResult.value.domain_stats || [];
            logActiveDomain.value = ds.length ? ds[0].domain : '';
            logScriptSynced.value = true;
            // 分析请求已顺带同步切割脚本 / cron / log_format，闭环即已接入
            logRotateExists.value = true;
            logRotateSynced.value = true;
            logLoading.value = false; logTaskStatus.value = ''; return;
          }
          if (t.status === 'error') {
            logError.value = t.error || '分析失败';
            logLoading.value = false; logTaskStatus.value = ''; return;
          }
          if (tries >= 180) {
            logError.value = '分析超时（>180s），请缩小时间范围或收紧过滤条件';
            logLoading.value = false; logTaskStatus.value = ''; return;
          }
          pollLogTask(id, tries + 1);
        } catch (e) {
          logError.value = '轮询失败：' + apiErrorText(e);
          logLoading.value = false; logTaskStatus.value = '';
        }
      }, 1000);
    }

    async function queryLogs() {
      if (!logAssetId.value) { logError.value = '请先选择服务器'; return; }
      if (!logCredId.value) { logError.value = '该资产无可用凭证，请先到“凭证”中添加'; return; }
      if (!logSelectedDomains.value.length) { logError.value = '请先点击“分析路径”并勾选至少一个域名'; return; }
      logLoading.value = true;
      logError.value = '';
      logResult.value = null;
      logTaskStatus.value = '提交任务...';
      logSubmitAt = Date.now();
      const body = {
        ecs_id: logAssetId.value,
        network: logNetwork.value,
        credential_id: logCredId.value,
        domains: logSelectedDomains.value.join(','),
        start: logStart.value,
        end: logEnd.value,
        status: (logFilterStatus.value || '').trim(),
        url: (logFilterUrl.value || '').trim(),
        ip: (logFilterIp.value || '').trim(),
        script_path: (logScriptPath.value || '').trim(),
      };
      try {
        const res = await authFetch('/api/bastion/logs/analyze', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body),
        });
        const json = await readApiJson(res, '提交分析');
        if (json.code !== 0) {
          logError.value = json.msg || '提交失败';
          logLoading.value = false; logTaskStatus.value = ''; return;
        }
        const d = json.data || {};
        if (d.debug_cmd) logDebugCmd.value = d.debug_cmd;
        if (!d.task_id) {
          logError.value = '后端未返回任务 ID，请确认后端已更新到异步分析版本（/api/bastion/logs/analyze）';
          logLoading.value = false; logTaskStatus.value = ''; return;
        }
        pollLogTask(d.task_id);
      } catch (e) {
        logError.value = apiErrorText(e);
        logLoading.value = false; logTaskStatus.value = '';
      }
    }

    // 按住 Ctrl 单击排行项：只把值写进过滤栏，不自动重跑分析；由用户点「开始分析」触发
    function filterByDomain(d) {
      logSelectedDomains.value = logDomains.value.includes(d) ? [d] : [];
    }
    function filterByUrl(u) {
      // 脚本返回的 url 形如「域名 /path」，下钻时只取路径部分；
      // 含斜杠会截断 awk 正则，用 . 通配替代
      const parts = String(u || '').trim().split(/\s+/);
      const path = parts.length > 1 ? parts.slice(1).join(' ') : parts[0];
      logFilterUrl.value = path.replace(/[^A-Za-z0-9_.-]/g, '.');
    }

    // 服务器下拉：按名称或 IP 实时筛选
    const filteredLogAssets = computed(() => {
      const q = (logAssetSearch.value || '').trim().toLowerCase();
      if (!q) return logAssets.value;
      return logAssets.value.filter(a =>
        (a.instance_name || a.name || '').toLowerCase().includes(q) ||
        (a.public_ip_address || '').toLowerCase().includes(q) ||
        (a.eip_address || '').toLowerCase().includes(q) ||
        (a.private_ip_address || '').toLowerCase().includes(q)
      );
    });

    const selectedAssetNetworks = computed(() => {
      const a = logAssets.value.find(x => x.id === logAssetId.value);
      if (!a) return [];
      const list = [];
      if (a.public_ip_address) list.push({ key: 'public', label: '公网', ip: a.public_ip_address });
      if (a.eip_address) list.push({ key: 'eip', label: '弹性', ip: a.eip_address });
      if (a.private_ip_address) list.push({ key: 'private', label: '私网', ip: a.private_ip_address });
      return list;
    });
    const selectedAssetNetwork = computed(() => {
      const n = selectedAssetNetworks.value.find(x => x.key === logNetwork.value);
      return n ? n.label : '';
    });

    function selectLogAsset(a) {
      logAssetId.value = a.id;
      logAssetSearch.value = a.instance_name || a.name || String(a.id);
      logAssetComboOpen.value = false;
      onLogAssetChange();
    }

    function setLogRange(kind) {
      activeLogRange.value = kind;
      const now = new Date();
      const pad = n => String(n).padStart(2, '0');
      const fmt = d => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
      if (kind === 'all') { logStart.value = ''; logEnd.value = ''; return; }
      let start;
      let end = now;
      if (kind === '1h') start = new Date(now.getTime() - 3600 * 1000);
      else if (kind === 'today') start = new Date(now.getFullYear(), now.getMonth(), now.getDate(), 0, 0, 0);
      else if (kind === 'yesterday') { start = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1, 0, 0, 0); end = new Date(now.getFullYear(), now.getMonth(), now.getDate(), 0, 0, 0); }
      else if (kind === '7d') start = new Date(now.getTime() - 7 * 86400 * 1000);
      logStart.value = fmt(start);
      logEnd.value = fmt(end);
    }

    function fmtBytes(n) {
      n = Number(n) || 0;
      if (n < 1024) return n + ' B';
      const units = ['KB', 'MB', 'GB', 'TB'];
      let i = -1;
      do { n /= 1024; i++; } while (n >= 1024 && i < units.length - 1);
      return n.toFixed(2) + ' ' + units[i];
    }

    function copyDebugCmd() {
      const s = logDebugCmd.value;
      if (!s) return;
      const done = () => { logDebugCopied.value = true; setTimeout(() => { logDebugCopied.value = false; }, 1500); };
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(s).then(done).catch(() => {});
        return;
      }
      const ta = document.createElement('textarea');
      ta.value = s;
      document.body.appendChild(ta);
      ta.select();
      try { document.execCommand('copy'); done(); } catch (e) { /* ignore */ }
      document.body.removeChild(ta);
    }

    onMounted(() => {
      setLogRange('today');
      if (CloudCore.checkSessionValid()) fetchLogAssets();
    });
    onUnmounted(() => {
      clearTimeout(logPollTimer);
    });

    return {
      logWarning,
      logDebugCmd, logDebugCopied, copyDebugCmd,
      logAssets, logAssetId, logNetwork, logCredId, logCreds, logScriptPath,
      logAssetSearch, logAssetComboOpen, filteredLogAssets, selectLogAsset,
      selectedAssetNetworks, selectedAssetNetwork,
      logCurrentDir, logBackupDir, logScriptSynced, logScriptExists, logActiveDomain,
      logExplored, logPreparedDir, logPreparedFiles,
      logRotatePath, logRotateExists, logRotateSynced, logRotateCron,
      logGlobal, logDomainStats, logActiveDomainStat, trendBarHeight, trendHourLabel,
      logFilterIp, logFilterUrl, logFilterStatus, filterByDomain, filterByUrl,
      logStart, logEnd, activeLogRange, setLogRange, markCustomRange, fmtBytes,
      logResult, logLoading, logError, logElapsed, logTaskStatus, queryLogs,
      logExploring, logExploreError, logDomains, logSelectedDomains,
      logFormat, logFormatName, logFormatSource, logFormatWarning,
      exploreLogs, selectAllLogDomains, invertLogDomains,
    };
  }
});
