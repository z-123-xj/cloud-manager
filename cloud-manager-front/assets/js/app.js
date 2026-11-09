// 域名管理 - Vue 3 应用
const { createApp, ref, reactive, computed, onMounted, nextTick, watch } = Vue;

const COL_PREFS_KEY = 'domain_col_prefs';
const TOKEN_KEY = 'cloud_token';
const USER_KEY = 'cloud_user';
const ROLE_KEY = 'cloud_role';

// ============ 会话守卫（空闲超时 + token 过期自动退出）============
const TOKEN_EXP_KEY = 'cloud_token_exp';
const IDLE_TIMEOUT_MS = 12 * 60 * 60 * 1000; // 12 小时无操作自动退出
let _lastActivity = Date.now();
let _tokenCheckTimer = null;

function recordActivity() { _lastActivity = Date.now(); }

// 清空认证信息
function clearAuthStorage() {
  localStorage.removeItem(TOKEN_KEY);
  localStorage.removeItem(USER_KEY);
  localStorage.removeItem(ROLE_KEY);
  localStorage.removeItem(TOKEN_EXP_KEY);
}

// 判断当前会话是否有效（token 存在 + 未过期 + 未空闲超时）
function checkSessionValid() {
  const token = localStorage.getItem(TOKEN_KEY);
  if (!token) return false;
  const exp = parseInt(localStorage.getItem(TOKEN_EXP_KEY) || '0', 10);
  if (exp && Date.now() > exp) return false;
  if (Date.now() - _lastActivity > IDLE_TIMEOUT_MS) return false;
  return true;
}

function startSessionGuard() {
  stopSessionGuard();
  recordActivity();
  ['mousemove', 'keydown', 'click', 'scroll', 'touchstart'].forEach((ev) =>
    document.addEventListener(ev, recordActivity, true)
  );
  _tokenCheckTimer = setInterval(() => {
    if (checkSessionValid()) return;
    if (!localStorage.getItem(TOKEN_KEY)) { stopSessionGuard(); return; }
    const exp = parseInt(localStorage.getItem(TOKEN_EXP_KEY) || '0', 10);
    const reason = (exp && Date.now() > exp) ? '登录已过期，请重新登录' : '长时间未操作，已自动退出登录';
    window.dispatchEvent(new CustomEvent('auth:unauthorized', { detail: { reason } }));
  }, 30 * 1000);
}

function stopSessionGuard() {
  if (_tokenCheckTimer) { clearInterval(_tokenCheckTimer); _tokenCheckTimer = null; }
  ['mousemove', 'keydown', 'click', 'scroll', 'touchstart'].forEach((ev) =>
    document.removeEventListener(ev, recordActivity, true)
  );
}

// 由 401 响应触发：清空并广播失效事件
function handleUnauthorized() {
  clearAuthStorage();
  window.dispatchEvent(new CustomEvent('auth:unauthorized', { detail: { reason: '登录已失效，请重新登录' } }));
}

// ============ 封装带认证的 fetch ============
function authFetch(url, options = {}) {
  const token = localStorage.getItem(TOKEN_KEY);
  if (!options.headers) options.headers = {};
  if (token) {
    options.headers['Authorization'] = token;
  }
  return fetch(url, options).then((res) => {
    if (res.status === 401) handleUnauthorized();
    return res;
  });
}

const app = createApp({
  setup() {
    // ============ 登录状态 ============
    const isLoggedIn = ref(false);
    const username = ref('');
    const userRole = ref('');
    const bastionEnabled = ref(true);
    const sidebarCollapsed = ref(false);  // 侧边栏默认展开；子菜单（域名/服务器）仍默认收起
    const isAdmin = computed(() => userRole.value === 'admin');
    const loginLoading = ref(false);
    const loginError = ref('');
    const captchaId = ref('');
    const captchaImg = ref('');
    // 登录字段回车依次跳转需要的 DOM 引用
    const usernameInput = ref(null);
    const passwordInput = ref(null);
    const captchaInput = ref(null);

    const loginForm = reactive({
      username: '',
      password: '',
      captcha: '',
    });

    // 初始化时检查 token
    function checkLogin() {
      // 任何「未登录/重新加载」场景下，先确保同步日志面板关闭，避免它残留在登录页上
      syncPanelVisible.value = false;
      syncPanelFullscreen.value = false;
      const token = localStorage.getItem(TOKEN_KEY);
      const savedUser = localStorage.getItem(USER_KEY);
      const savedRole = localStorage.getItem(ROLE_KEY);
      if (token && savedUser) {
        if (checkSessionValid()) {
          isLoggedIn.value = true;
          username.value = savedUser;
          userRole.value = savedRole || '';
          startSessionGuard();
        } else {
          clearAuthStorage();
          isLoggedIn.value = false;
        }
      }
    }

    async function refreshCaptcha() {
      try {
        const res = await fetch('/api/captcha?t=' + Date.now());
        const json = await res.json();
        if (json.code === 0) {
          captchaId.value = json.data.captcha_id;
          captchaImg.value = json.data.image;
          loginForm.captcha = '';
        }
      } catch (e) { /* 刷新失败不影响登录页展示 */ }
    }

    async function handleLogin() {
      loginError.value = '';
      if (!loginForm.username || !loginForm.password) {
        loginError.value = '请输入用户名和密码';
        return;
      }

      loginLoading.value = true;
      try {
        const res = await fetch('/api/login/', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            username: loginForm.username,
            password: loginForm.password,
            captcha_id: captchaId.value,
            captcha_code: loginForm.captcha,
          }),
        });
        const json = await res.json();

        if (json.code === 0) {
          localStorage.setItem(TOKEN_KEY, json.data.token);
          localStorage.setItem(USER_KEY, json.data.email || loginForm.username);
          localStorage.setItem(ROLE_KEY, json.data.role || '');
          const expSec = (json.data && json.data.expires_at) ? json.data.expires_at : Math.floor(Date.now() / 1000) + 24 * 3600;
          localStorage.setItem(TOKEN_EXP_KEY, String(expSec * 1000));
          isLoggedIn.value = true;
          username.value = json.data.email || loginForm.username;
          userRole.value = json.data.role || '';
          startSessionGuard();
          // 登录成功后加载数据
          loadColPrefs();
          fetchAccountList();
          fetchDomains();
          fetchStats();
          fetchHomeStats();
        } else if (res.status === 403 || json.code === 403) {
          loginError.value = json.msg || '账号已被锁定，请联系管理员重置';
          refreshCaptcha();
        } else {
          loginError.value = json.msg || '登录失败';
          refreshCaptcha();
        }
      } catch (e) {
        loginError.value = '网络请求失败';
      } finally {
        loginLoading.value = false;
      }
    }

    function logout() {
      stopSessionGuard();
      localStorage.removeItem(TOKEN_KEY);
      localStorage.removeItem(USER_KEY);
      localStorage.removeItem(ROLE_KEY);
      isLoggedIn.value = false;
      username.value = '';
      userRole.value = '';
      loginForm.username = '';
      loginForm.password = '';
      // 重置数据
      domains.value = [];
      total.value = 0;
      // 关闭可能残留的同步日志面板，避免退出后它仍盖在登录页上
      syncPanelVisible.value = false;
      syncPanelFullscreen.value = false;
      if (syncAbortController) { try { syncAbortController.abort(); } catch (_) {} syncAbortController = null; }
      domainSyncRunning.value = false;
      ecsSyncRunning.value = false;
      refreshCaptcha();
    }

    // token 失效（401 / 过期 / 空闲超时）统一走这里：自动退出并提示
    window.addEventListener('auth:unauthorized', (e) => {
      stopSessionGuard();
      isLoggedIn.value = false;
      // 登录超时/令牌失效：清空密码，要求重新输入（与手动退出一致）
      loginForm.password = '';
      const reason = (e.detail && e.detail.reason) || '登录已失效，请重新登录';
      showToast(reason, 'error');
    });

    // ============ 列定义 ============
    const allColumns = reactive([
      { key: 'domain_name',         label: '域名',           visible: true },
      { key: 'platform',            label: '平台',           visible: true },
      { key: 'account_name',        label: '账号',           visible: true },
      { key: 'owner',               label: '所有者',         visible: false },
      { key: 'registration_date',   label: '注册日期',       visible: true },
      { key: 'expiration_date',     label: '到期日期',       visible: true },
      { key: 'expiration_days_left',label: '剩余天数',       visible: true },
      { key: 'auto_renew_enabled',  label: '自动续费',       visible: true },
      { key: 'status',              label: '状态',           visible: true },
      { key: 'source',              label: '来源',           visible: true },
    ]);

    function loadColPrefs() {
      try {
        const saved = localStorage.getItem(COL_PREFS_KEY);
        if (saved) {
          const prefs = JSON.parse(saved);
          allColumns.forEach(col => {
            if (prefs[col.key] !== undefined) col.visible = prefs[col.key];
          });
        }
      } catch (e) { /* ignore */ }
    }

    function saveColPrefs() {
      const prefs = {};
      allColumns.forEach(col => { prefs[col.key] = col.visible; });
      localStorage.setItem(COL_PREFS_KEY, JSON.stringify(prefs));
    }

    const visibleColumns = computed(() => allColumns.filter(c => c.visible));

    function colVisible(key) {
      const col = allColumns.find(c => c.key === key);
      return col ? col.visible : false;
    }

    // ============ 排序 ============
    const sortKey = ref('');
    const sortDir = ref('asc');

    function toggleSort(key) {
      if (sortKey.value === key) {
        sortDir.value = sortDir.value === 'asc' ? 'desc' : 'asc';
      } else {
        sortKey.value = key;
        sortDir.value = 'asc';
      }
      fetchDomains();
    }

    function getSortIcon(key) {
      if (sortKey.value !== key) return '⇅';
      return sortDir.value === 'asc' ? '↑' : '↓';
    }

    // ============ 状态 ============
    const currentTab = ref('home');

    // 按住 Ctrl/⌘ 或中键点击任意导航入口时统一在新标签打开。
    // 捕获阶段记录修饰键，让 navigate('/path') 这类没传 $event 的调用也能生效；
    // 不少入口是 <a> 但没有 href，只放行给浏览器会出现「点了没反应」。
    let navMods = { ctrl: false, meta: false, middle: false };
    if (typeof window !== 'undefined') {
      window.addEventListener('click', (e) => {
        navMods = { ctrl: !!e.ctrlKey, meta: !!e.metaKey, middle: e.button === 1 };
      }, true);
    }

    // ============ 顶栏「账号」下拉（可输入筛选）============
    const navDirOpen = ref(false);
    const navDirKeyword = ref('');
    const hasNavDir = computed(() => [
      'domains', 'dnsDomains', 'cdnDomains', 'ecs', 'resourcePackages', 'bastionAssets',
    ].includes(currentTab.value));
    // 筛选栏「账号」按钮的选中摘要（5.html：账号：xxx）
    const navDirLabel = computed(() => {
      const t = currentTab.value;
      let platform = '', account = '';
      if (t === 'domains') { platform = filters.platform; account = filters.account; }
      else if (t === 'dnsDomains') { platform = dnsDomainFilters.platform; account = dnsDomainFilters.account; }
      else if (t === 'cdnDomains') { platform = cdnDomainFilters.platform; account = cdnDomainFilters.account; }
      else if (t === 'ecs') { platform = ecsFilters.platform; account = ecsFilters.account; }
      else if (t === 'bastionAssets') { platform = bastionAssetPlatform.value; account = bastionAssetAccount.value; }
      else if (t === 'resourcePackages') {
        const acc = rpAccounts.value.find(a => a.account_id === rpFilters.value.account);
        account = acc ? (acc.account_name || acc.name || acc.account_id) : (rpFilters.value.account ? '已选账号' : '');
      }
      if (account) return account;
      if (platform) return platform;
      return '全部';
    });
    function toggleNavDir() {
      navDirOpen.value = !navDirOpen.value;
      if (navDirOpen.value) navDirKeyword.value = '';
      nextTick(() => {
        positionNavDir();
        applyNavDirFilter();
      });
    }
    // 左侧抽屉固定贴左，清理旧版按按钮位置写入的 inline 定位
    function positionNavDir() {
      const drawer = document.querySelector('.cdn-drawer.nav-dir.open');
      if (!drawer) return;
      drawer.style.left = '';
      drawer.style.top = '';
    }
    // 筛选树是各页自己的 DOM，这里按文本实时显隐，避免 6 个页面各写一套过滤逻辑。
    // 平台标题命中时其下账号整组显示；「全部」项始终保留。
    function applyNavDirFilter() {
      const drawer = document.querySelector('.cdn-drawer.nav-dir.open');
      if (!drawer) return;
      const kw = navDirKeyword.value.trim().toLowerCase();
      let groupMatched = true;
      let matchedItems = 0;
      Array.from(drawer.children).forEach(el => {
        if (el.classList.contains('nav-dir-head') ||
            el.classList.contains('nav-dir-search') ||
            el.classList.contains('cdn-drawer-title') ||
            el.classList.contains('nav-dir-empty')) {
          el.style.display = '';
          return;
        }
        const text = (el.textContent || '').trim().toLowerCase();
        if (el.classList.contains('cdn-drawer-group')) {
          groupMatched = !kw || text.includes(kw);
          el.style.display = groupMatched ? '' : 'none';
          return;
        }
        if (!el.classList.contains('cdn-drawer-item')) return;
        const isTotal = !el.classList.contains('sub-2') && !el.classList.contains('sub');
        const show = !kw || isTotal || groupMatched || text.includes(kw);
        el.style.display = show ? '' : 'none';
        if (show && !isTotal) matchedItems++;
      });
      let empty = drawer.querySelector('.nav-dir-empty');
      if (!empty) {
        empty = document.createElement('div');
        empty.className = 'nav-dir-empty';
        empty.textContent = '无匹配账号';
        drawer.appendChild(empty);
      }
      empty.style.display = (kw && matchedItems === 0) ? '' : 'none';
    }
    watch(navDirKeyword, () => nextTick(applyNavDirFilter));

    // 顶栏页面标题图标（对齐日志排查独立页的顶栏样式）
    const PAGE_ICONS = {
      home: 'fa-gauge-high', domains: 'fa-globe', dnsDomains: 'fa-sitemap', dns: 'fa-list',
      cdnDomains: 'fa-cloud', ecs: 'fa-server', regions: 'fa-map-location-dot',
      resourcePackages: 'fa-cubes', bastionAssets: 'fa-shield-halved',
      bastionGroups: 'fa-users-gear', bastionCredentials: 'fa-key',
      bastionTerminal: 'fa-terminal', users: 'fa-user-gear', accounts: 'fa-address-card',
      batchExec: 'fa-bolt',
    };
    const currentPageIcon = computed(() => PAGE_ICONS[currentTab.value] || 'fa-cube');

    // ============ 阿里云式导航抽屉 / 工作台 ============
    const LOG_PAGE = '/assets/pages/logs.html?v=20261108i';
    const drawerOpen = ref(false);
    const drawerCategory = ref('all');
    const recentVisits = [
      { name: '域名管理', path: '/assets/pages/domains.html' },
      { name: '云服务器 ECS', path: '/assets/pages/ecs.html' },
      { name: 'DNS解析', path: '/assets/pages/dns.html' },
      { name: '堡垒机资产', path: '/assets/pages/bastion.html' },
      { name: '日志排查', path: LOG_PAGE },
    ];
    // 收藏与最近访问不放同一入口，避免抽屉里同一个页面出现两遍
    const favorites = [
      { name: 'CDN域名', path: '/assets/pages/cdn.html' },
      { name: '云平台账号', path: '/accounts' },
      { name: '资源包', path: '/resource-packages' },
    ];
    function goTab(path) {
      if (navMods.ctrl || navMods.meta || navMods.middle) {
        window.open(path, '_blank', 'noopener');
        return;
      }
      drawerOpen.value = false;
      // 日志排查是独立页面，需整页跳转；其余走 SPA 伪路由
      if (path.includes('.html')) { window.location.href = path; return; }
      navigate(path);
    }

    const loading = ref(false);
    const syncing = ref(false);
    const page = ref(1);
    const size = ref(20);
    const total = ref(0);
    const domains = ref([]);
    const detailVisible = ref(false);
    const detail = ref(null);
    const showColPicker = ref(false);

    const colFilterOpen = ref(null);
    const colFilters = reactive({
      auto_renews: [],  // 多选自动续费
    });

    const domainPlatformList = ref([]);
    const domainAccountOptions = ref([]); // [{name, platform}] 用于左侧目录树分组

    const filters = reactive({
      keyword: '',
      platform: '',
      account: '',
      is_offline: '',
      expire_start: '',
      expire_end: '',
    });

    const stats = reactive({
      online: 0,
      offline: 0,
      expiringSoon: 0,
      expired: 0,
    });

    // ============ 首页概览数据 ============
    const homeStats = reactive({
      domainTotal: 0,
      domainOnline: 0,
      domainExpiring: 0,
      domainExpired: 0,
      ecsTotal: 0,
      ecsOnline: 0,
      ecsOffline: 0,
      accountCount: 0,
      backendAlive: false,
    });

    // 工作台 / 抽屉共用的资源分组；每个入口只归属一个分组，避免「全部」里出现重复卡片
    const workbenchGroups = computed(() => [
      { key: 'domains', title: '域名与网站', icon: 'fas fa-globe', items: [
        { label: '域名管理', path: '/assets/pages/domains.html', count: `${homeStats.domainTotal} 域名` },
        { label: 'DNS解析', path: '/assets/pages/dns.html', count: `${homeStats.domainTotal} 域名` },
        { label: 'CDN域名', path: '/assets/pages/cdn.html', count: '加速域名' },
      ] },
      { key: 'compute', title: '计算', icon: 'fas fa-server', items: [
        { label: '云服务器 ECS', path: '/assets/pages/ecs.html', count: `${homeStats.ecsTotal} 实例` },
        { label: '实例地域开通', path: '/assets/pages/regions.html', count: '地域管理' },
      ] },
      { key: 'security', title: '安全运维', icon: 'fas fa-shield-alt', items: [
        { label: '堡垒机资产', path: '/assets/pages/bastion.html', count: '资产管理' },
        { label: '凭证管理', path: '/bastion/credentials', count: 'SSH 凭证' },
        { label: '用户组管理', path: '/bastion/groups', count: '账号管理 / 授权' },
        { label: '批量执行', path: '/assets/pages/batch-exec.html', count: '命令/脚本下发' },
        { label: '日志排查', path: LOG_PAGE, count: 'Nginx 日志分析' },
      ] },
      { key: 'resource', title: '资源', icon: 'fas fa-cubes', items: [
        { label: '资源包', path: '/resource-packages', count: '资源包管理' },
      ] },
      { key: 'accounts', title: '账号与用户', icon: 'fas fa-users', items: [
        { label: '云平台账号', path: '/accounts', count: `${homeStats.accountCount} 账号` },
        { label: '用户管理', path: '/users', count: '权限管理' },
      ] },
    ]);

    // ============ 顶栏全局搜索 ============
    // 直接复用工作台分组作为唯一入口源，避免搜索结果与抽屉两处维护不一致
    const globalSearchInput = ref(null);
    const globalSearchKeyword = ref('');
    const globalSearchOpen = ref(false);
    const globalSearchActive = ref(-1);
    const globalSearchIndex = computed(() => {
      const list = [];
      const seen = new Set();
      workbenchGroups.value.forEach(g => g.items.forEach(item => {
        if (seen.has(item.path)) return;
        seen.add(item.path);
        list.push({ label: item.label, path: item.path, group: g.title, hint: item.count || '' });
      }));
      return list;
    });
    const globalSearchResults = computed(() => {
      const kw = globalSearchKeyword.value.trim().toLowerCase();
      if (!kw) return globalSearchIndex.value.slice(0, 8);
      return globalSearchIndex.value.filter(i =>
        i.label.toLowerCase().includes(kw) ||
        i.group.toLowerCase().includes(kw) ||
        i.path.toLowerCase().includes(kw) ||
        String(i.hint).toLowerCase().includes(kw)
      ).slice(0, 8);
    });
    function openGlobalSearch() {
      globalSearchOpen.value = true;
      globalSearchActive.value = -1;
    }
    function closeGlobalSearch() {
      globalSearchOpen.value = false;
      globalSearchActive.value = -1;
    }
    function clearGlobalSearch() {
      globalSearchKeyword.value = '';
      openGlobalSearch();
      if (globalSearchInput.value) globalSearchInput.value.focus();
    }
    function goGlobalSearch(item) {
      if (!item) return;
      if (navMods.ctrl || navMods.meta || navMods.middle) {
        globalSearchKeyword.value = '';
        closeGlobalSearch();
        window.open(item.path, '_blank', 'noopener');
        return;
      }
      globalSearchKeyword.value = '';
      closeGlobalSearch();
      if (globalSearchInput.value) globalSearchInput.value.blur();
      goTab(item.path);
    }
    function onGlobalSearchKey(e) {
      const list = globalSearchResults.value;
      const key = String(e.key || '');
      if (key === 'ArrowDown') {
        e.preventDefault();
        globalSearchActive.value = Math.min(globalSearchActive.value + 1, list.length - 1);
      } else if (key === 'ArrowUp') {
        e.preventDefault();
        globalSearchActive.value = Math.max(globalSearchActive.value - 1, -1);
      } else if (key === 'Enter') {
        e.preventDefault();
        goGlobalSearch(list[globalSearchActive.value >= 0 ? globalSearchActive.value : 0]);
      } else if (key === 'Escape') {
        e.preventDefault();
        closeGlobalSearch();
        if (globalSearchInput.value) globalSearchInput.value.blur();
      }
    }
    function onGlobalSearchShortcut(e) {
      const t = e.target;
      const tag = t && t.tagName ? t.tagName.toLowerCase() : '';
      if (tag === 'input' || tag === 'textarea' || tag === 'select' || (t && t.isContentEditable)) return;
      const key = String(e.key || '').toLowerCase();
      if ((e.ctrlKey || e.metaKey) && key === 'k') {
        e.preventDefault();
        if (!globalSearchInput.value) return;
        globalSearchInput.value.focus();
        openGlobalSearch();
      }
    }

    // 左侧分类直接由分组派生，分类和内容永远一一对应
    const drawerCats = computed(() => [
      { key: 'all', name: '全部' },
      ...workbenchGroups.value.map(g => ({ key: g.key, name: g.title })),
    ]);
    const drawerTitle = computed(() => {
      if (drawerCategory.value === 'favorites') return '我的收藏';
      if (drawerCategory.value === 'all') return '我的资源';
      const g = workbenchGroups.value.find(g => g.key === drawerCategory.value);
      return g ? g.title : '我的资源';
    });
    const filteredWorkbenchGroups = computed(() => {
      if (drawerCategory.value === 'favorites') return [];
      if (drawerCategory.value === 'all') return workbenchGroups.value;
      return workbenchGroups.value.filter(g => g.key === drawerCategory.value);
    });

    async function fetchHomeStats() {
      // 后台健康检查独立执行：统计接口任一异常都不应让「后台服务不可用」误报
      try {
        const hz = await fetch('/api/healthz');
        const hj = await hz.json();
        homeStats.backendAlive = !!(hj && hj.message === 'ok');
      } catch (e) {
        homeStats.backendAlive = false;
      }
      try {
        // 域名总数
        const r1 = await authFetch('/api/domains?size=1');
        const j1 = await r1.json();
        if (j1.code === 0) homeStats.domainTotal = j1.data.total || 0;

        // 在线域名
        const r2 = await authFetch('/api/domains?size=1&is_offline=0');
        const j2 = await r2.json();
        if (j2.code === 0) homeStats.domainOnline = j2.data.total || 0;

        // 即将到期域名
        const now = new Date();
        const end30 = new Date(now.getTime() + 30 * 24 * 3600 * 1000);
        const todayStr = now.toISOString().slice(0, 10);
        const endStr = end30.toISOString().slice(0, 10);
        const r3 = await authFetch(`/api/domains?size=1&is_offline=0&expire_start=${todayStr}&expire_end=${endStr}`);
        const j3 = await r3.json();
        if (j3.code === 0) homeStats.domainExpiring = j3.data.total || 0;

        // 已过期域名
        const r4 = await authFetch(`/api/domains?size=1&is_offline=0&expire_end=${todayStr}`);
        const j4 = await r4.json();
        if (j4.code === 0) homeStats.domainExpired = j4.data.total || 0;

        // ECS总数
        const r5 = await authFetch('/api/ecs?size=1');
        const j5 = await r5.json();
        if (j5.code === 0) homeStats.ecsTotal = j5.data.total || 0;

        // ECS在线
        const r6 = await authFetch('/api/ecs?size=1&is_offline=0');
        const j6 = await r6.json();
        if (j6.code === 0) homeStats.ecsOnline = j6.data.total || 0;

        // ECS已下线
        const r7 = await authFetch('/api/ecs?size=1&is_offline=1');
        const j7 = await r7.json();
        if (j7.code === 0) homeStats.ecsOffline = j7.data.total || 0;

        // 云账号数量（使用无需管理员权限的接口）
        const r8 = await authFetch('/api/accounts/count');
        const j8 = await r8.json();
        if (j8.code === 0) homeStats.accountCount = j8.data.total || 0;

      } catch (e) { /* ignore */ }
    }

    const toast = reactive({
      show: false,
      msg: '',
      type: 'success',
    });

    // ============ 个人资料 / 修改密码（顶栏头像下拉）============
    const profileMenuOpen = ref(false);
    const profileVisible = ref(false);
    const profileSaving = ref(false);
    const profile = reactive({ username: '', role: '', email: '', real_name: '' });
    const passwordVisible = ref(false);
    const passwordSaving = ref(false);
    const passwordForm = reactive({ old_password: '', new_password: '', confirm_password: '' });

    // 头像文字：取用户名首字符大写
    const avatarText = computed(() => {
      const n = (username.value || '').trim();
      return n ? n[0].toUpperCase() : 'U';
    });

    // 解析接口响应：后端未更新时路由不存在会返回 HTML(404)，需给出可读错误而非笼统“网络请求失败”
    async function parseApiRes(res, action) {
      const text = await res.text();
      try {
        return JSON.parse(text);
      } catch (e) {
        if (res.status === 404) {
          throw new Error(action + '：接口不存在(404)，请确认后端已更新到最新版本并重启');
        }
        throw new Error(action + '失败（HTTP ' + res.status + '）');
      }
    }

    async function openProfile() {
      profileMenuOpen.value = false;
      try {
        const res = await authFetch('/api/profile');
        const json = await parseApiRes(res, '获取个人资料');
        if (json.code === 0) {
          profile.username = json.data.username || '';
          profile.role = json.data.role || '';
          profile.email = json.data.email || '';
          profile.real_name = json.data.real_name || '';
          profileVisible.value = true;
        } else {
          showToast(json.msg || '获取个人资料失败', 'error');
        }
      } catch (e) { showToast(e.message || '网络请求失败', 'error'); }
    }

    async function saveProfile() {
      if (!profile.email) { showToast('邮箱不能为空', 'error'); return; }
      profileSaving.value = true;
      try {
        const res = await authFetch('/api/profile', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ email: profile.email, real_name: profile.real_name })
        });
        const json = await parseApiRes(res, '保存个人资料');
        if (json.code === 0) {
          showToast('保存成功');
          profileVisible.value = false;
        } else {
          showToast(json.msg || '保存失败', 'error');
        }
      } catch (e) { showToast(e.message || '网络请求失败', 'error'); }
      finally { profileSaving.value = false; }
    }

    function openChangePassword() {
      profileMenuOpen.value = false;
      passwordForm.old_password = '';
      passwordForm.new_password = '';
      passwordForm.confirm_password = '';
      passwordVisible.value = true;
    }

    async function savePassword() {
      if (!passwordForm.old_password) { showToast('请输入原密码', 'error'); return; }
      if (!passwordForm.new_password || passwordForm.new_password.length < 6) { showToast('新密码至少 6 位', 'error'); return; }
      if (passwordForm.new_password !== passwordForm.confirm_password) { showToast('两次输入的新密码不一致', 'error'); return; }
      passwordSaving.value = true;
      try {
        const res = await authFetch('/api/profile/password', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ old_password: passwordForm.old_password, new_password: passwordForm.new_password })
        });
        const json = await parseApiRes(res, '修改密码');
        if (json.code === 0) {
          showToast('密码修改成功，请牢记新密码');
          passwordVisible.value = false;
        } else {
          showToast(json.msg || '修改失败', 'error');
        }
      } catch (e) { showToast(e.message || '网络请求失败', 'error'); }
      finally { passwordSaving.value = false; }
    }

    // ============ 点击外部关闭 ============
    function closeAllPopups() {
      showColPicker.value = false;
      colFilterOpen.value = null;
      ecsColFilterOpen.value = null;
      profileMenuOpen.value = false;
      closeGlobalSearch();
      navDirOpen.value = false;
    }

    if (typeof window !== 'undefined') {
      window.addEventListener('click', closeAllPopups);
      window.addEventListener('keydown', onGlobalSearchShortcut);
    }

    const totalPages = computed(() => Math.ceil(total.value / size.value) || 1);

    // ============ 方法 ============

    function showToast(msg, type = 'success') {
      toast.msg = msg;
      toast.type = type;
      toast.show = true;
      setTimeout(() => { toast.show = false; }, 2500);
    }

    // 复制文本到剪贴板（带 execCommand 降级方案）
    async function copyText(text) {
      if (text == null || text === '') return;
      const str = String(text);
      try {
        if (navigator.clipboard && navigator.clipboard.writeText) {
          await navigator.clipboard.writeText(str);
        } else {
          const ta = document.createElement('textarea');
          ta.value = str;
          ta.style.position = 'fixed';
          ta.style.opacity = '0';
          document.body.appendChild(ta);
          ta.select();
          document.execCommand('copy');
          document.body.removeChild(ta);
        }
        showToast('已复制: ' + str, 'success');
      } catch (e) {
        showToast('复制失败', 'error');
      }
    }

    function formatDate(dateStr) {
      if (!dateStr) return '-';
      const d = new Date(dateStr);
      if (!isNaN(d.getTime())) {
        const y = d.getFullYear();
        const m = String(d.getMonth() + 1).padStart(2, '0');
        const day = String(d.getDate()).padStart(2, '0');
        return `${y}-${m}-${day}`;
      }
      // 解析不了的格式（如带时区名的字符串）只取日期部分，避免整串日期被截断
      const m = String(dateStr).match(/^(\d{4})[-/](\d{1,2})[-/](\d{1,2})/);
      if (m) return `${m[1]}-${String(m[2]).padStart(2, '0')}-${String(m[3]).padStart(2, '0')}`;
      return dateStr;
    }

    function formatDateTime(dateStr) {
      if (!dateStr) return '-';
      // 阿里云API返回的时间格式如 "2022-05-18T09:16Z" (UTC时间)
      // 直接用 new Date() 解析，然后用 toLocaleString 转为 UTC+8 显示
      const d = new Date(dateStr);
      if (isNaN(d.getTime())) return dateStr;
      // 转为 UTC+8 显示
      const options = { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false };
      try {
        const s = d.toLocaleString('zh-CN', options);
        return s.replace(/\//g, '-');
      } catch (e) {
        // fallback: 手动计算 UTC+8
        const utcMs = d.getTime() + d.getTimezoneOffset() * 60000;
        const c8 = new Date(utcMs + 8 * 3600000);
        const y = c8.getFullYear();
        const m = String(c8.getMonth() + 1).padStart(2, '0');
        const day = String(c8.getDate()).padStart(2, '0');
        const h = String(c8.getHours()).padStart(2, '0');
        const min = String(c8.getMinutes()).padStart(2, '0');
        const s2 = String(c8.getSeconds()).padStart(2, '0');
        return `${y}-${m}-${day} ${h}:${min}:${s2}`;
      }
    }

    function getDaysClass(days) {
      if (days < 0) return 'text-danger';
      if (days <= 30) return 'text-warning';
      return 'text-success';
    }

    function toggleColFilter(key) {
      colFilterOpen.value = colFilterOpen.value === key ? null : key;
    }

    // 多选漏斗：切换选项
    function toggleColFilterOption(filterArr, value) {
      const idx = filterArr.indexOf(value);
      if (idx >= 0) {
        filterArr.splice(idx, 1);
      } else {
        filterArr.push(value);
      }
    }

    // 多选漏斗：是否选中
    function isColFilterSelected(filterArr, value) {
      return filterArr.includes(value);
    }

    // 多选漏斗：全选/取消全选
    function toggleColFilterAll(filterArr, allValues) {
      if (filterArr.length === allValues.length) {
        filterArr.splice(0, filterArr.length);
      } else {
        filterArr.splice(0, filterArr.length, ...allValues);
      }
    }

    // 多选漏斗：是否全选
    function isColFilterAllSelected(filterArr, allValues) {
      return filterArr.length === allValues.length && allValues.length > 0;
    }

    function applyColFilter() {
      page.value = 1;
      fetchDomains();
    }

    // 拉取域名漏斗选项（账号）- 用后端 DISTINCT 查询，不再遍历全量数据
    async function fetchAccountList() {
      try {
        const res = await authFetch('/api/domains/filter-options');
        const json = await res.json();
        if (json.code === 0) {
          domainPlatformList.value = (json.data.platforms || []).sort();
          domainAccountOptions.value = json.data.account_options || [];
        }
      } catch (e) { /* ignore */ }
    }

    function domainAccountsOfPlatform(pf) {
      return domainAccountOptions.value.filter(a => a.platform === pf).map(a => a.name);
    }

    function domainSelectFilter(platform, account) {
      filters.platform = platform;
      filters.account = account;
      page.value = 1;
      pageInput.value = 1;
      fetchDomains();
    }

    // 服务端分页查询域名
    async function fetchDomains() {
      loading.value = true;
      try {
        // 需要前端筛选的字段（accounts多选、auto_renew、status），需要拉取全量数据
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

        let allList = [];
        let serverTotal = 0;

        // 第一页
        const res = await authFetch('/api/domains?' + params.toString());
        const json = await res.json();

        if (json.code === 401) {
          logout();
          return;
        }

        if (json.code === 0) {
          allList = json.data.list || [];
          serverTotal = json.data.total || 0;

          // 如果需要全量数据且超过100条，继续拉取
          if (needClientFilter && serverTotal > 100) {
            const totalPages = Math.ceil(serverTotal / 100);
            for (let p = 2; p <= totalPages && p <= 100; p++) {
              params.set('page', String(p));
              const r2 = await authFetch('/api/domains?' + params.toString());
              const j2 = await r2.json();
              if (j2.code === 0) {
                allList = allList.concat(j2.data.list || []);
              }
            }
          }

          let list = allList;

          // 前端筛选：多选自动续费
          if (colFilters.auto_renews.length > 0) {
            list = list.filter(item => {
              const val = item.auto_renew_enabled ? '1' : '0';
              return colFilters.auto_renews.includes(val);
            });
          }

          // 前端分页（仅在拉取全量时）
          if (needClientFilter) {
            total.value = list.length;
            const start = (page.value - 1) * size.value;
            domains.value = list.slice(start, start + size.value);
          } else {
            domains.value = list;
            total.value = serverTotal;
          }
        } else {
          showToast(json.msg || '查询失败', 'error');
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
        const r3 = await authFetch(`/api/domains?size=1&is_offline=0&expire_start=${todayStr}&expire_end=${endStr}`);
        const j3 = await r3.json();
        if (j3.code === 0) stats.expiringSoon = j3.data.total || 0;

        const r4 = await authFetch(`/api/domains?size=1&is_offline=0&expire_end=${todayStr}`);
        const j4 = await r4.json();
        if (j4.code === 0) stats.expired = j4.data.total || 0;
      } catch (e) { /* ignore */ }
    }

    function onSizeChange() {
      page.value = 1;
      pageInput.value = 1;
      nextTick(() => fetchDomains());
    }

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
      if (p >= 1 && p <= totalPages.value) {
        goPage(p);
      }
    }

    // 页码输入框
    const pageInput = ref(1);

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

    // 修改域名备注：阿里云国内站会同步到云端，其它平台（国际站/AWS/Cloudflare等）仅保存在本平台
    const domainRemarkSaving = ref(false);

    async function saveDomainRemark() {
      if (!detail.value) return;
      domainRemarkSaving.value = true;
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
          if (d.synced) {
            tip += '（已同步到云端）';
          } else if (d.sync_msg) {
            tip += '（' + d.sync_msg + '）';
          }
          showToast(tip.length > 120 ? tip.slice(0, 120) + '…' : tip);
          fetchDomains();
        } else {
          showToast(json.msg || '保存失败', 'error');
        }
      } catch (e) {
        showToast('网络请求失败', 'error');
      } finally {
        domainRemarkSaving.value = false;
      }
    }

    // ============ 同步进度面板 ============
    const syncPanelVisible = ref(false);
    const syncPanelFullscreen = ref(false);
    const syncLogs = ref([]);
    let syncAbortController = null;

    // 域名同步独立状态
    const domainSyncRunning = ref(false);
    const domainSyncError = ref(false);
    const domainSyncLabel = ref('');

    // ECS同步独立状态
    const ecsSyncRunning = ref(false);
    const ecsSyncError = ref(false);
    const ecsSyncLabel = ref('');

    // 同步开始时自动打开面板（页面日志面板已移除：同步日志改看服务端日志文件/journalctl，
    // 页面只通过 toast 反馈触发与完成结果，此处保留空实现避免改动各同步函数调用点）
    function autoOpenSyncPanel() {
      // no-op：同步日志面板已下线
    }

    function closeSyncPanel() {
      syncPanelVisible.value = false;
      syncPanelFullscreen.value = false;
    }

    // 兜底保护：退出登录时强制关闭同步日志面板，防止残留到登录页
    watch(isLoggedIn, (newVal) => {
      if (!newVal) closeSyncPanel();
    });

    function clearSyncLogs() {
      syncLogs.value = [];
    }

    function showSyncLog() {
      syncPanelVisible.value = true;
    }

    // 读取 SSE 流日志的公共函数
    function readSSELog(url, signal, onLine, onDone, onError) {
      const token = localStorage.getItem(TOKEN_KEY);
      return fetch(url, { headers: { 'Authorization': 'Bearer ' + token }, signal })
        .then(async (response) => {
          const reader = response.body.getReader();
          const decoder = new TextDecoder();
          let buffer = '';
          while (true) {
            const { done, value } = await reader.read();
            if (done) break;
            buffer += decoder.decode(value, { stream: true });
            const lines = buffer.split('\n');
            buffer = lines.pop() || '';
            for (const line of lines) {
              if (line.startsWith('data: ')) {
                const raw = line.slice(6);
                let text = raw;
                try { text = JSON.parse(raw); } catch (_) { /* raw */ }
                onLine(text);
              } else if (line.startsWith('event: done')) {
                onDone();
                return;
              }
            }
          }
          onDone();
        })
        .catch((err) => {
          if (err.name !== 'AbortError') {
            onError(err.message);
          }
        });
    }

    async function syncDomains() {
      // 重复点击检查
      if (domainSyncRunning.value) {
        showToast('域名同步正在进行中，请稍候', 'warning');
        return;
      }
      syncing.value = true;
      domainSyncRunning.value = true;
      domainSyncError.value = false;
      domainSyncLabel.value = '同步域名中...';
      syncLogs.value = ['[启动] 域名同步任务已提交...'];
      autoOpenSyncPanel();
      try {
        const res = await authFetch('/api/sync/domains', { method: 'POST' });
        const json = await res.json();
        if (json.code === 0) {
          syncAbortController = new AbortController();
          readSSELog(
            '/api/sync/progress?task_id=' + json.task_id,
            syncAbortController.signal,
            (text) => {
              if (/error|err|错误|失败|panic|fatal/i.test(text)) domainSyncError.value = true;
              syncLogs.value.push(text);
            },
            () => {
              syncing.value = false;
              domainSyncRunning.value = false;
              domainSyncLabel.value = domainSyncError.value ? '同步失败' : '同步完成';
              if (!domainSyncError.value) showToast('域名同步完成');
              else showToast('域名同步出现错误', 'error');
              fetchDomains(); fetchStats();
              setTimeout(() => { domainSyncLabel.value = ''; domainSyncError.value = false; }, 30000);
            },
            (errMsg) => {
              domainSyncError.value = true;
              syncing.value = false;
              domainSyncRunning.value = false;
              domainSyncLabel.value = '同步失败';
              syncLogs.value.push('[错误] 无法连接同步进度: ' + errMsg);
              showToast('域名同步失败', 'error');
              closeSyncPanel(); // 同步异常时关闭面板，防止残留
              setTimeout(() => { domainSyncLabel.value = ''; domainSyncError.value = false; }, 30000);
            }
          );
        } else {
          domainSyncError.value = true;
          domainSyncRunning.value = false;
          domainSyncLabel.value = '同步失败';
          syncing.value = false;
          syncLogs.value.push('[失败] ' + (json.msg || '同步失败'));
          showToast(json.msg || '同步失败', 'error');
          closeSyncPanel(); // 同步失败时关闭面板，防止残留
          setTimeout(() => { domainSyncLabel.value = ''; domainSyncError.value = false; }, 30000);
        }
      } catch (e) {
        domainSyncError.value = true;
        domainSyncRunning.value = false;
        domainSyncLabel.value = '同步失败';
        syncing.value = false;
        syncLogs.value.push('[错误] 同步请求失败: ' + e.message);
        showToast('同步请求失败', 'error');
        closeSyncPanel(); // 同步异常时关闭面板，防止残留
        setTimeout(() => { domainSyncLabel.value = ''; domainSyncError.value = false; }, 30000);
      }
    }

    // DNS解析域名列表同步（独立于域名注册同步）
    async function syncDnsDomains() {
      if (domainSyncRunning.value) {
        showToast('域名同步正在进行中，请稍候', 'warning');
        return;
      }
      syncing.value = true;
      domainSyncRunning.value = true;
      domainSyncError.value = false;
      domainSyncLabel.value = '同步DNS域名中...';
      syncLogs.value = ['[启动] DNS域名同步任务已提交...'];
      autoOpenSyncPanel();
      try {
        const res = await authFetch('/api/sync/dns', { method: 'POST' });
        const json = await res.json();
        if (json.code === 0) {
          syncAbortController = new AbortController();
          readSSELog(
            '/api/sync/progress?task_id=' + json.task_id,
            syncAbortController.signal,
            (text) => {
              if (/error|err|错误|失败|panic|fatal/i.test(text)) domainSyncError.value = true;
              syncLogs.value.push(text);
            },
            () => {
              syncing.value = false;
              domainSyncRunning.value = false;
              domainSyncLabel.value = domainSyncError.value ? '同步失败' : '同步完成';
              if (!domainSyncError.value) showToast('DNS域名同步完成');
              else showToast('DNS域名同步出现错误', 'error');
              fetchDnsDomains(); fetchDnsDomainFilterOptions();
              setTimeout(() => { domainSyncLabel.value = ''; domainSyncError.value = false; }, 30000);
            },
            (errMsg) => {
              domainSyncError.value = true;
              syncing.value = false;
              domainSyncRunning.value = false;
              domainSyncLabel.value = '同步失败';
              syncLogs.value.push('[错误] 无法连接同步进度: ' + errMsg);
              showToast('DNS域名同步失败', 'error');
              closeSyncPanel(); // 同步异常时关闭面板，防止残留
              setTimeout(() => { domainSyncLabel.value = ''; domainSyncError.value = false; }, 30000);
            }
          );
        } else {
          domainSyncError.value = true;
          domainSyncRunning.value = false;
          domainSyncLabel.value = '同步失败';
          syncing.value = false;
          syncLogs.value.push('[失败] ' + (json.msg || 'DNS域名同步失败'));
          showToast(json.msg || 'DNS域名同步失败', 'error');
          closeSyncPanel(); // 同步失败时关闭面板，防止残留
          setTimeout(() => { domainSyncLabel.value = ''; domainSyncError.value = false; }, 30000);
        }
      } catch (e) {
        domainSyncError.value = true;
        domainSyncRunning.value = false;
        domainSyncLabel.value = '同步失败';
        syncing.value = false;
        syncLogs.value.push('[错误] 同步请求失败: ' + e.message);
        showToast('DNS域名同步请求失败', 'error');
        closeSyncPanel(); // 同步异常时关闭面板，防止残留
        setTimeout(() => { domainSyncLabel.value = ''; domainSyncError.value = false; }, 30000);
      }
    }

    // ============ ECS 同步 ============
    const ecsSyncing = ref(false);
    async function syncEcsInstances() {
      if (ecsSyncRunning.value) {
        showToast('ECS同步正在进行中，请稍候', 'warning');
        return;
      }
      ecsSyncing.value = true;
      ecsSyncRunning.value = true;
      ecsSyncError.value = false;
      ecsSyncLabel.value = 'ECS同步中...';
      syncLogs.value = ['[启动] ECS同步任务已提交...'];
      autoOpenSyncPanel();
      try {
        const res = await authFetch('/api/sync/ecs', { method: 'POST' });
        const json = await res.json();
        if (json.code === 0) {
          syncAbortController = new AbortController();
          readSSELog(
            '/api/sync/progress?task_id=' + json.task_id,
            syncAbortController.signal,
            (text) => {
              if (/error|err|错误|失败|panic|fatal/i.test(text)) ecsSyncError.value = true;
              syncLogs.value.push(text);
            },
            () => {
              ecsSyncing.value = false;
              ecsSyncRunning.value = false;
              ecsSyncLabel.value = ecsSyncError.value ? '同步失败' : '同步完成';
              if (!ecsSyncError.value) showToast('ECS同步完成');
              else showToast('ECS同步出现错误', 'error');
              fetchEcsInstances(); fetchEcsStats();
              setTimeout(() => { ecsSyncLabel.value = ''; ecsSyncError.value = false; }, 30000);
            },
            (errMsg) => {
              ecsSyncError.value = true;
              ecsSyncing.value = false;
              ecsSyncRunning.value = false;
              ecsSyncLabel.value = '同步失败';
              syncLogs.value.push('[错误] 无法连接同步进度: ' + errMsg);
              showToast('ECS同步失败', 'error');
              closeSyncPanel(); // 同步异常时关闭面板，防止残留
              setTimeout(() => { ecsSyncLabel.value = ''; ecsSyncError.value = false; }, 30000);
            }
          );
        } else {
          ecsSyncError.value = true;
          ecsSyncRunning.value = false;
          ecsSyncLabel.value = '同步失败';
          ecsSyncing.value = false;
          syncLogs.value.push('[失败] ' + (json.msg || 'ECS同步失败'));
          showToast(json.msg || 'ECS同步失败', 'error');
          closeSyncPanel(); // 同步失败时关闭面板，防止残留
          setTimeout(() => { ecsSyncLabel.value = ''; ecsSyncError.value = false; }, 30000);
        }
      } catch (e) {
        ecsSyncError.value = true;
        ecsSyncRunning.value = false;
        ecsSyncLabel.value = '同步失败';
        ecsSyncing.value = false;
        syncLogs.value.push('[错误] 同步请求失败: ' + e.message);
        showToast('ECS同步请求失败', 'error');
        closeSyncPanel(); // 同步异常时关闭面板，防止残留
        setTimeout(() => { ecsSyncLabel.value = ''; ecsSyncError.value = false; }, 30000);
      }
    }

    // ============ ECS 实例 ============
    const ecsLoading = ref(false);
    const ecsList = ref([]);
    const ecsPage = ref(1);
    const ecsSize = ref(20);
    const ecsTotal = ref(0);
    const ecsFilters = reactive({
      keyword: '',
      is_offline: '0',
      platform: '',
      account: '',
      region: '',
    });

    // ECS 列漏斗筛选（平台、状态、账号、地域）- 多选
    const ecsColFilterOpen = ref(null);
    const ecsColFilters = reactive({
      platforms: [],
      statuses: [],
      accounts: [],
      regions: [],
    });

    const ecsAccountList = ref([]);
    const ecsRegionList = ref([]);
    const ecsPlatformList = ref([]);
    const ecsAccountOptions = ref([]); // [{name, platform}] 用于左侧目录树分组

    const ecsStats = reactive({
      online: 0,
      offline: 0,
    });

    const ecsTotalPages = computed(() => Math.ceil(ecsTotal.value / ecsSize.value) || 1);

    // ECS 列表排序：实例ID/名称列，默认不排序，点击切换升/降（针对当前页）
    const ecsSortDir = ref('');   // ''=不排序, 'asc'/'desc'
    const ecsSortIcon = computed(() => ecsSortDir.value === 'asc' ? '▲' : ecsSortDir.value === 'desc' ? '▼' : '⇅');
    const nameOfEcs = (x) => (x.instance_name || x.InstanceName || '').toString().toLowerCase();
    const ecsSortedList = computed(() => {
      const list = ecsList.value;
      if (!ecsSortDir.value) return list;
      const dir = ecsSortDir.value === 'asc' ? 1 : -1;
      return [...list].sort((a, b) => nameOfEcs(a).localeCompare(nameOfEcs(b)) * dir);
    });
    function toggleEcsNameSort() {
      ecsSortDir.value = ecsSortDir.value === 'asc' ? 'desc' : 'asc';
    }

    // 获取ECS漏斗选项（直接从数据库 DISTINCT 查询，快）
    async function fetchEcsFilterOptions(platform = '', account = '') {
      try {
        const params = new URLSearchParams();
        if (platform) params.append('platform', platform);
        if (account) params.append('account_name', account);
        const res = await authFetch('/api/ecs/filter-options' + (params.toString() ? '?' + params.toString() : ''));
        const json = await res.json();
        if (json.code === 0) {
          ecsAccountList.value = (json.data.accounts || []).sort();
          ecsRegionList.value = (json.data.regions || []).sort();
          ecsPlatformList.value = (json.data.platforms || []).sort();
          ecsAccountOptions.value = json.data.account_options || [];
        }
      } catch (e) { /* ignore */ }
    }

    function ecsAccountsOfPlatform(pf) {
      return ecsAccountOptions.value.filter(a => a.platform === pf).map(a => a.name);
    }

    function ecsSelectFilter(platform, account) {
      ecsFilters.platform = platform;
      ecsFilters.account = account;
      ecsFilters.region = ''; // 切换目录树时清空地域（地域选项随之按账号过滤）
      ecsPage.value = 1;
      ecsPageInput.value = 1;
      fetchEcsFilterOptions(platform, account); // 地域选项按当前平台/账号过滤，与资产页一致
      fetchEcsInstances();
    }

    function toggleEcsColFilter(key) {
      ecsColFilterOpen.value = ecsColFilterOpen.value === key ? null : key;
    }

    function ecsToggleColFilterOption(filterArr, value) {
      const idx = filterArr.indexOf(value);
      if (idx >= 0) {
        filterArr.splice(idx, 1);
      } else {
        filterArr.push(value);
      }
    }

    function ecsIsColFilterSelected(filterArr, value) {
      return filterArr.includes(value);
    }

    // ECS 漏斗全选/取消全选
    function ecsToggleColFilterAll(filterArr, allValues) {
      if (filterArr.length === allValues.length) {
        filterArr.splice(0, filterArr.length);
      } else {
        filterArr.splice(0, filterArr.length, ...allValues);
      }
    }

    function ecsIsColFilterAllSelected(filterArr, allValues) {
      return filterArr.length === allValues.length && allValues.length > 0;
    }

    function ecsApplyColFilter() {
      ecsPage.value = 1;
      ecsPageInput.value = 1;
      fetchEcsInstances();
    }

    function ecsStatusClass(status) {
      const map = { Running: 'tag tag-success', Stopped: 'tag tag-default', Starting: 'tag tag-warning', Stopping: 'tag tag-warning' };
      return map[status] || 'tag';
    }

    function ecsStatusLabel(status) {
      const map = { Running: '运行中', Stopped: '已停止', Starting: '启动中', Stopping: '停止中' };
      return map[status] || status;
    }

    function ecsDotClass(status) {
      const map = { Running: 'dot-online', Stopped: 'dot-offline', Starting: 'dot-warning', Stopping: 'dot-warning' };
      return map[status] || 'dot-offline';
    }

    // 查询 ECS 实例（服务端分页 + 服务端筛选）
    async function fetchEcsInstances() {
      ecsLoading.value = true;
      try {
        const params = new URLSearchParams();
        params.append('page', String(ecsPage.value));
        params.append('size', String(ecsSize.value));
        if (ecsFilters.keyword) params.append('keyword', ecsFilters.keyword);
        if (ecsFilters.is_offline !== '') params.append('is_offline', ecsFilters.is_offline);
        if (ecsFilters.platform) params.append('platform', ecsFilters.platform);
        if (ecsFilters.account) params.append('account_name', ecsFilters.account);
        if (ecsFilters.region) params.append('region_name', ecsFilters.region);
        // 漏斗多选：账号/地域支持多值（逗号分隔）传给后端做 IN 查询；平台/状态后端暂只支持单值，取第一个
        if (ecsColFilters.platforms.length === 1) params.append('platform', ecsColFilters.platforms[0]);
        if (ecsColFilters.statuses.length === 1) params.append('status', ecsColFilters.statuses[0]);
        if (ecsColFilters.accounts.length >= 1) params.append('account_name', ecsColFilters.accounts.join(','));
        if (ecsColFilters.regions.length >= 1) params.append('region_name', ecsColFilters.regions.join(','));

        const res = await authFetch('/api/ecs?' + params.toString());
        const json = await res.json();

        if (json.code === 401) {
          logout();
          return;
        }

        if (json.code === 0) {
          // 账号/地域多选已由后端 IN 查询处理；平台/状态后端暂只支持单值，多选时在前端对当前页二次筛选
          let list = json.data.list || [];
          if (ecsColFilters.platforms.length > 1) {
            list = list.filter(item => ecsColFilters.platforms.includes(item.platform));
          }
          if (ecsColFilters.statuses.length > 1) {
            list = list.filter(item => ecsColFilters.statuses.includes(item.status));
          }
          ecsList.value = list;
          ecsTotal.value = json.data.total || 0;
        }
      } catch (e) {
        showToast('ECS查询请求失败', 'error');
      } finally {
        ecsLoading.value = false;
      }
    }

    async function fetchEcsStats() {
      try {
        const r1 = await authFetch('/api/ecs?size=1&is_offline=0');
        const j1 = await r1.json();
        if (j1.code === 0) ecsStats.online = j1.data.total || 0;

        const r2 = await authFetch('/api/ecs?size=1&is_offline=1');
        const j2 = await r2.json();
        if (j2.code === 0) ecsStats.offline = j2.data.total || 0;
      } catch (e) { /* ignore */ }
    }

    function ecsOnSizeChange() {
      ecsPage.value = 1;
      ecsPageInput.value = 1;
      nextTick(() => fetchEcsInstances());
    }

    function ecsSearch() {
      ecsPage.value = 1;
      ecsPageInput.value = 1;
      fetchEcsInstances();
    }

    function ecsResetFilters() {
      ecsFilters.keyword = '';
      ecsFilters.is_offline = '0';
      ecsFilters.platform = '';
      ecsFilters.account = '';
      ecsFilters.region = '';
      ecsColFilters.platforms = [];
      ecsColFilters.statuses = [];
      ecsColFilters.accounts = [];
      ecsColFilters.regions = [];
      ecsPage.value = 1;
      ecsPageInput.value = 1;
      fetchEcsFilterOptions('', ''); // 重置后地域选项恢复全局
      fetchEcsInstances();
    }

    function ecsGoPage(p) {
      ecsPage.value = p;
      ecsPageInput.value = p;
      fetchEcsInstances();
    }

    function ecsJumpPage() {
      const p = ecsPageInput.value;
      if (p >= 1 && p <= ecsTotalPages.value) {
        ecsGoPage(p);
      }
    }

    // ECS 页码输入框
    const ecsPageInput = ref(1);

    // ============ DNS可管理域名列表（dns_domains 表） ============
    const dnsDomainLoading = ref(false);
    const dnsDomainList = ref([]);
    const dnsDomainPage = ref(1);
    const dnsDomainSize = ref(20);
    const dnsDomainTotal = ref(0);
    const dnsDomainPageInput = ref(1);
    const dnsDomainFilters = reactive({
      keyword: '',
      platform: '',
      account: '',
    });
    const dnsDomainPlatformList = ref([]);
    const dnsDomainAccountOptions = ref([]); // [{name, platform}] 用于左侧目录树分组

    // 域名（注册列表）手动管理
    const domainManualModalVisible = ref(false);
    const domainManualEditing = ref(false);
    const domainManualSaving = ref(false);
    const domainManualEditId = ref(null);
    const domainManualForm = reactive({
      domain_name: '', platform: '', account_name: '', owner: '',
      auto_renew_enabled: false, registration_date: '', expiration_date: '', remark: '', is_offline: 0
    });

    const dnsDomainTotalPages = computed(() => Math.ceil(dnsDomainTotal.value / dnsDomainSize.value) || 1);

    function dnsDomainPlatformLabel(p) {
      const map = { aliyun: '阿里云', aws: 'AWS', tencent: '腾讯云', cloudflare: 'Cloudflare' };
      return map[p] || p;
    }

    async function fetchDnsDomainFilterOptions() {
      try {
        const res = await authFetch('/api/dns-domains/filter-options');
        const json = await res.json();
        if (json.code === 0) {
          dnsDomainPlatformList.value = (json.data.platforms || []).sort();
          dnsDomainAccountOptions.value = json.data.account_options || [];
        }
      } catch (e) { /* ignore */ }
    }

    function dnsDomainAccountsOfPlatform(pf) {
      return dnsDomainAccountOptions.value.filter(a => a.platform === pf).map(a => a.name);
    }

    function dnsDomainSelectFilter(platform, account) {
      dnsDomainFilters.platform = platform;
      dnsDomainFilters.account = account;
      dnsDomainPage.value = 1;
      dnsDomainPageInput.value = 1;
      fetchDnsDomains();
    }

    async function fetchDnsDomains() {
      dnsDomainLoading.value = true;
      try {
        const params = new URLSearchParams();
        params.append('page', String(dnsDomainPage.value));
        params.append('size', String(dnsDomainSize.value));
        if (dnsDomainFilters.keyword) params.append('keyword', dnsDomainFilters.keyword);
        if (dnsDomainFilters.platform) params.append('platform', dnsDomainFilters.platform);
        if (dnsDomainFilters.account) params.append('account_name', dnsDomainFilters.account);

        const res = await authFetch('/api/dns-domains?' + params.toString());
        const json = await res.json();
        if (json.code === 401) { logout(); return; }
        if (json.code === 0) {
          dnsDomainList.value = json.data.list || [];
          dnsDomainTotal.value = json.data.total || 0;
        } else {
          showToast(json.msg || '查询失败', 'error');
        }
      } catch (e) {
        showToast('网络请求失败', 'error');
      } finally {
        dnsDomainLoading.value = false;
      }
    }

    function dnsDomainSearch() {
      dnsDomainPage.value = 1;
      dnsDomainPageInput.value = 1;
      fetchDnsDomains();
    }

    function dnsDomainResetFilters() {
      dnsDomainFilters.keyword = '';
      dnsDomainFilters.platform = '';
      dnsDomainFilters.account = '';
      dnsDomainPage.value = 1;
      dnsDomainPageInput.value = 1;
      fetchDnsDomains();
    }

    function dnsDomainGoPage(p) {
      dnsDomainPage.value = p;
      dnsDomainPageInput.value = p;
      fetchDnsDomains();
    }

    function dnsDomainJumpPage() {
      const p = dnsDomainPageInput.value;
      if (p >= 1 && p <= dnsDomainTotalPages.value) dnsDomainGoPage(p);
    }

    function dnsDomainOnSizeChange() {
      dnsDomainPage.value = 1;
      dnsDomainPageInput.value = 1;
      nextTick(() => fetchDnsDomains());
    }

    // ============ 域名（注册列表）手动管理 ============
    function openDomainManualModal(item) {
      if (item) {
        domainManualEditing.value = true;
        domainManualEditId.value = item.id;
        domainManualForm.domain_name = item.domain_name || '';
        domainManualForm.platform = item.platform || '';
        domainManualForm.account_name = item.account_name || '';
        domainManualForm.owner = item.owner || '';
        domainManualForm.auto_renew_enabled = !!item.auto_renew_enabled;
        domainManualForm.registration_date = item.registration_date ? String(item.registration_date).slice(0, 10) : '';
        domainManualForm.expiration_date = item.expiration_date ? String(item.expiration_date).slice(0, 10) : '';
        domainManualForm.remark = item.remark || '';
        domainManualForm.is_offline = item.is_offline || 0;
      } else {
        domainManualEditing.value = false;
        domainManualEditId.value = null;
        domainManualForm.domain_name = '';
        domainManualForm.platform = '';
        domainManualForm.account_name = '';
        domainManualForm.owner = '';
        domainManualForm.auto_renew_enabled = false;
        domainManualForm.registration_date = '';
        domainManualForm.expiration_date = '';
        domainManualForm.remark = '';
        domainManualForm.is_offline = 0;
      }
      domainManualModalVisible.value = true;
    }

    async function saveDomainManual() {
      if (!domainManualForm.domain_name) { showToast('域名必填', 'error'); return; }
      domainManualSaving.value = true;
      try {
        let res;
        if (domainManualEditing.value) {
          res = await authFetch('/api/domains/' + domainManualEditId.value, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(domainManualForm) });
        } else {
          res = await authFetch('/api/domains', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(domainManualForm) });
        }
        const json = await res.json();
        if (json.code === 0) { showToast(domainManualEditing.value ? '更新成功' : '创建成功'); domainManualModalVisible.value = false; fetchDomains(); }
        else showToast(json.msg || '操作失败', 'error');
      } catch (e) { showToast('请求失败', 'error'); } finally { domainManualSaving.value = false; }
    }

    async function deleteDomainManual(item) {
      if (!confirm('确定删除域名 "' + (item.domain_name || '') + '" 吗？')) return;
      try {
        const res = await authFetch('/api/domains/' + item.id, { method: 'DELETE' });
        const json = await res.json();
        if (json.code === 0) { showToast('删除成功'); fetchDomains(); }
        else showToast(json.msg || '删除失败', 'error');
      } catch (e) { showToast('请求失败', 'error'); }
    }

    // ============ DNS解析记录（全屏页面） ============
    const dnsModalLoading = ref(false);
    const dnsModalError = ref('');
    const dnsModalList = ref([]);
    const dnsCurrentDomain = ref('');
    const dnsCurrentPlatform = ref('');   // 当前域名所属平台（决定走哪套解析界面）
    const dnsCurrentAccount = ref('');    // 当前域名所属账号别名（站点），仅用于页面显示
    const dnsCurrentAccountId = ref('');  // 当前域名所属账号ID（唯一键），传给后端精确匹配凭证
    const dnsRecordKeyword = ref('');
    const dnsRecordTypeFilter = ref('');

    // 构造 DNS 记录 API 地址：把平台+账号显式拼到 query，后端按显式值路由到对应云厂商，不再猜平台
    function dnsApi(path) {
      // 优先用 account_id 精确匹配凭证，缺失时回退账号别名
      const acc = dnsCurrentAccountId.value || dnsCurrentAccount.value;
      const q = '?platform=' + encodeURIComponent(dnsCurrentPlatform.value) + '&account=' + encodeURIComponent(acc);
      return '/api/domains/' + encodeURIComponent(dnsCurrentDomain.value) + path + q;
    }

    // 平台显示名（解析页标题用）
    function dnsPlatformTitle(p) {
      const map = { aliyun: '阿里云', tencent: '腾讯云', cloudflare: 'Cloudflare', aws: 'AWS' };
      return map[p] || p;
    }

    // 点击列表「解析」：按该行的 platform+account（站点）路由到对应平台的解析界面
    function openDnsResolve(item) {
      dnsCurrentDomain.value = item.domain_name;
      dnsCurrentPlatform.value = item.platform || '';
      dnsCurrentAccount.value = item.account_name || '';    // 页面「站点」显示用
      dnsCurrentAccountId.value = item.account_id || '';    // 后端匹配账号凭证用
      const url = '/dns/' + encodeURIComponent(item.domain_name)
        + '?platform=' + encodeURIComponent(item.platform || '')
        + '&account=' + encodeURIComponent(item.account_name || '')
        + '&accountId=' + encodeURIComponent(item.account_id || '');
      navigate(url);
    }

    // 前端搜索过滤后的解析记录列表
    const filteredDnsModalList = computed(() => {
      let list = dnsModalList.value;
      if (dnsRecordTypeFilter.value) {
        list = list.filter(rec => rec.type === dnsRecordTypeFilter.value);
      }
      if (dnsRecordKeyword.value) {
        const kw = dnsRecordKeyword.value.toLowerCase();
        list = list.filter(rec => {
          return (rec.rr && rec.rr.toLowerCase().includes(kw)) ||
                 (rec.value && rec.value.toLowerCase().includes(kw)) ||
                 (rec.type && rec.type.toLowerCase().includes(kw));
        });
      }
      return list;
    });

    // DNS记录表单
    const dnsFormVisible = ref(false);
    const dnsFormEditing = ref(false);
    const dnsFormSaving = ref(false);
    const dnsCollapseOpen = ref(false); // TTL/负载策略折叠面板

    // 记录值格式校验结果（null=未输入, true=正确, false=错误）
    const dnsValueValid = ref(null);

    // 校验正则（参考阿里云DNS规范）
    const DNS_RULES = {
      A:     { pattern: /^(\d{1,3}\.){3}\d{1,3}$/, test: v => { const p = v.split('.'); return p.length === 4 && p.every(n => parseInt(n) >= 0 && parseInt(n) <= 255); }, hint: 'A记录的记录值为IPv4形式（如10.10.10.10）', placeholder: '如 10.10.10.10', errorMsg: 'A记录必须为合法IPv4地址，如 10.10.10.10' },
      AAAA:  { pattern: /^([0-9a-fA-F]{0,4}:){2,7}[0-9a-fA-F]{0,4}$/, test: v => /^([0-9a-fA-F]{0,4}:){2,7}[0-9a-fA-F]{0,4}$/.test(v) || /^([0-9a-fA-F]{0,4}:){2,6}((\d{1,3}\.){3}\d{1,3})$/.test(v), hint: 'AAAA记录的记录值为IPv6形式（如fe80::1）', placeholder: '如 fe80::1', errorMsg: 'AAAA记录必须为合法IPv6地址' },
      CNAME: { pattern: /^([a-zA-Z0-9]([a-zA-Z0-9\-]*[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$/, test: v => /^([a-zA-Z0-9]([a-zA-Z0-9\-]*[a-zA-Z0-9])?\.)*[a-zA-Z0-9]([a-zA-Z0-9\-]*[a-zA-Z0-9])?\.?$/.test(v) && v.includes('.'), hint: 'CNAME记录的记录值为域名形式（如example.com，不能是IP地址）', placeholder: '如 www.example.com', errorMsg: 'CNAME记录必须为合法域名，不能是IP地址' },
      MX:    { pattern: /^([a-zA-Z0-9]([a-zA-Z0-9\-]*[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$/, test: v => /^([a-zA-Z0-9]([a-zA-Z0-9\-]*[a-zA-Z0-9])?\.)*[a-zA-Z0-9]([a-zA-Z0-9\-]*[a-zA-Z0-9])?\.?$/.test(v) && v.includes('.'), hint: 'MX记录的记录值为邮件服务器域名（如mail.example.com）', placeholder: '如 mail.example.com', errorMsg: 'MX记录必须为合法邮件服务器域名' },
      TXT:   { pattern: /^.+$/, test: v => v.length <= 255, hint: 'TXT记录值为任意文本，最长255字符，支持中文', placeholder: '如 v=spf1 include:spf.example.com ~all', errorMsg: 'TXT记录值不能超过255个字符' },
      NS:    { pattern: /^([a-zA-Z0-9]([a-zA-Z0-9\-]*[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$/, test: v => /^([a-zA-Z0-9]([a-zA-Z0-9\-]*[a-zA-Z0-9])?\.)*[a-zA-Z0-9]([a-zA-Z0-9\-]*[a-zA-Z0-9])?\.?$/.test(v) && v.includes('.'), hint: 'NS记录的记录值为DNS服务器域名（如ns1.example.com）', placeholder: '如 ns1.example.com', errorMsg: 'NS记录必须为合法DNS服务器域名' },
      SRV:   { pattern: /^\d+\s+\d+\s+\d+\s+\S+$/, test: v => /^\d{1,5}\s+\d{1,5}\s+\d{1,5}\s+.+$/.test(v), hint: 'SRV记录格式：优先级 权重 端口 目标主机（如5 0 5060 sip.example.com）', placeholder: '如 5 0 5060 sip.example.com', errorMsg: 'SRV记录格式为"优先级 权重 端口 目标主机"' },
      CAA:   { pattern: /^\d+\s+\S+\s+\S+$/, test: v => /^\d{1,3}\s+\S+\s+.+$/.test(v), hint: 'CAA记录格式：flags tag value（如0 issue "ca.example.com"）', placeholder: '如 0 issue "ca.example.com"', errorMsg: 'CAA记录格式为"flags tag value"' },
      PTR:   { pattern: /^([a-zA-Z0-9]([a-zA-Z0-9\-]*[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$/, test: v => /^([a-zA-Z0-9]([a-zA-Z0-9\-]*[a-zA-Z0-9])?\.)*[a-zA-Z0-9]([a-zA-Z0-9\-]*[a-zA-Z0-9])?\.?$/.test(v) && v.includes('.'), hint: 'PTR记录的记录值为反向域名形式（如1.0.0.10.in-addr.arpa）', placeholder: '如 host.example.com', errorMsg: 'PTR记录必须为合法域名' },
    };

    // 获取记录值提示文字
    function getDnsValueHint(type) {
      const rule = DNS_RULES[type];
      if (rule) return rule.hint;
      if (type === '显性URL' || type === '隐性URL') return 'URL记录的记录值需以 http:// 或 https:// 开头';
      return '请输入对应的记录值';
    }

    // 获取记录值占位符
    function getDnsValuePlaceholder(type) {
      const rule = DNS_RULES[type];
      if (rule) return rule.placeholder;
      if (type === '显性URL' || type === '隐性URL') return '如 https://www.example.com';
      return '请输入记录值';
    }

    // 获取格式错误提示
    function getDnsValueError(type) {
      const rule = DNS_RULES[type];
      if (rule) return rule.errorMsg;
      if (type === '显性URL' || type === '隐性URL') return 'URL记录必须以 http:// 或 https:// 开头';
      return '记录值格式不正确';
    }

    // 校验单个记录值格式
    function validateDnsValue(type, value) {
      if (!value || value.trim() === '') return null;
      const v = value.trim();
      if (type === '显性URL' || type === '隐性URL') {
        return /^https?:\/\/.+/.test(v);
      }
      const rule = DNS_RULES[type];
      if (rule) return rule.test(v);
      return null; // 未知类型不校验
    }

    // 校验轮询模式下单个记录值
    function dnsValidateRoundRobinValue(value) {
      if (!value || value.trim() === '') return null;
      return validateDnsValue(dnsForm.type, value);
    }

    const dnsForm = reactive({
      record_id: '',
      domain_name: '',
      rr: '',
      type: 'A',
      value: '',
      ttl: 600,
      priority: 0,
      line: 'default',
      line_type: 'default', // default / region
      load_balance: 'weight', // weight / round_robin
      weight: 1,
      remark: '',
      old_remark: '',   // 编辑时原始备注，用于比对
      old_status: '',   // 编辑时原始状态，用于比对
      status: '',       // 编辑时当前状态
      proxied: false,   // Cloudflare CDN 代理（橙云）状态，仅 CF 平台有效
    });
    // 轮询模式下的多记录值
    const roundRobinValues = ref([{ value: '', status: 'ENABLE', remark: '' }]);

    function addRoundRobinValue() {
      roundRobinValues.value.push({ value: '', status: 'ENABLE', remark: '' });
    }

    function removeRoundRobinValue(idx) {
      if (roundRobinValues.value.length <= 1) {
        showToast('至少保留一条记录值', 'warning');
        return;
      }
      roundRobinValues.value.splice(idx, 1);
    }

    // 打开DNS解析页面并加载记录（按当前平台/账号路由到对应云厂商）
    async function showDnsRecords(item) {
      if (item && item.platform) dnsCurrentPlatform.value = item.platform;
      if (item && item.account_name) dnsCurrentAccount.value = item.account_name;
      if (item && item.account_id) dnsCurrentAccountId.value = item.account_id;
      dnsCurrentDomain.value = item.domain_name;
      dnsModalLoading.value = true;
      dnsModalError.value = '';
      dnsModalList.value = [];

      try {
        const res = await authFetch(dnsApi('/dns'));
        const json = await res.json();
        if (json.code === 0) {
          dnsModalList.value = json.data.list || [];
        } else {
          dnsModalError.value = json.msg || '查询失败';
        }
      } catch (e) {
        dnsModalError.value = '网络请求失败: ' + e.message;
      } finally {
        dnsModalLoading.value = false;
      }
    }

    function retryDnsRecords() {
      if (dnsCurrentDomain.value) {
        showDnsRecords({ domain_name: dnsCurrentDomain.value });
      }
    }

    function openDnsAddForm() {
      dnsFormEditing.value = false;
      dnsCollapseOpen.value = false;
      dnsValueValid.value = null;
      dnsForm.record_id = '';
      dnsForm.domain_name = dnsCurrentDomain.value;
      dnsForm.rr = '';
      dnsForm.type = 'A';
      dnsForm.value = '';
      dnsForm.ttl = 600;
      dnsForm.priority = 0;
      dnsForm.line = 'default';
      dnsForm.line_type = 'default';
      dnsForm.load_balance = 'weight';
      dnsForm.weight = 1;
      dnsForm.remark = '';
      dnsForm.status = '';
      dnsForm.proxied = false;
      roundRobinValues.value = [{ value: '', status: 'ENABLE', remark: '' }];
      dnsFormVisible.value = true;
    }

    function openDnsEditForm(rec) {
      dnsFormEditing.value = true;
      dnsCollapseOpen.value = false;
      dnsValueValid.value = null;
      dnsForm.record_id = rec.record_id;
      dnsForm.domain_name = dnsCurrentDomain.value;
      dnsForm.rr = rec.rr;
      dnsForm.type = rec.type;
      dnsForm.value = rec.value;
      dnsForm.ttl = rec.ttl;
      dnsForm.priority = rec.priority || 0;
      // 根据 line 值设置 line_type
      if (rec.line === 'default') {
        dnsForm.line = 'default';
        dnsForm.line_type = 'default';
      } else if (rec.line === 'oversea' || rec.line === 'internal') {
        dnsForm.line = rec.line;
        dnsForm.line_type = 'region';
      } else {
        dnsForm.line = 'default';
        dnsForm.line_type = 'default';
      }
      // 负载策略仅阿里云/腾讯云有效；Cloudflare/AWS 固定权重模式(单值)
      const isCnDns = dnsCurrentPlatform.value === 'aliyun' || dnsCurrentPlatform.value === 'tencent';
      if (!isCnDns) {
        dnsForm.load_balance = 'weight';
        dnsForm.weight = 1;
      } else if (rec.weight && rec.weight > 0) {
        // 根据 weight 判断负载策略：weight > 0 则权重模式，否则轮询
        dnsForm.load_balance = 'weight';
        dnsForm.weight = rec.weight;
        roundRobinValues.value = [{ value: '', status: 'ENABLE', remark: '' }];
      } else {
        dnsForm.load_balance = 'round_robin';
        dnsForm.weight = 0;
        // 轮询模式暂用单值展示当前记录值
        roundRobinValues.value = [{ value: rec.value || '', status: rec.status || 'ENABLE', remark: rec.remark || '' }];
      }
      dnsForm.remark = rec.remark || '';
      dnsForm.status = rec.status || '';       // 当前状态（可能在前端被切换）
      dnsForm.proxied = !!rec.proxied;         // Cloudflare 橙云代理状态（编辑时预填）
      dnsFormVisible.value = true;
    }

    // DNS记录线路标签
    function dnsRecordSearch() {
      // 关键词搜索通过 computed 自动过滤，这里不需要额外操作
    }

    function dnsRecordLineLabel(line) {
      if (!line || line === 'default') return '默认';
      if (line === 'internal') return '中国地区';
      if (line === 'oversea') return '境外';
      return line;
    }

    // DNS记录负载策略标签
    function dnsRecordStrategyLabel(weight) {
      if (weight && weight > 0) return '权重';
      return '轮询';
    }

    // DNS记录权重数值
    function dnsRecordWeightNum(weight) {
      if (weight && weight > 0) return weight;
      return '-';
    }

    async function saveDnsRecord() {
      if (!dnsForm.rr) { showToast('请输入主机记录', 'error'); return; }
      if (!dnsForm.type) { showToast('请选择记录类型', 'error'); return; }

      // 负载策略仅阿里云/腾讯云有效；其余平台一律用单值 dnsForm.value
      const isCnDns = dnsCurrentPlatform.value === 'aliyun' || dnsCurrentPlatform.value === 'tencent';
      let recordValue = dnsForm.value;
      if (isCnDns && dnsForm.load_balance === 'round_robin') {
        recordValue = roundRobinValues.value[0].value;
      }
      if (!recordValue) { showToast('请输入记录值', 'error'); return; }

      // 格式校验
      if (isCnDns && dnsForm.load_balance === 'round_robin') {
        for (let i = 0; i < roundRobinValues.value.length; i++) {
          const v = roundRobinValues.value[i];
          if (v.value && v.value.trim()) {
            const valid = validateDnsValue(dnsForm.type, v.value);
            if (valid === false) {
              showToast('第' + (i + 1) + '行记录值格式错误：' + getDnsValueError(dnsForm.type), 'error');
              return;
            }
          }
        }
      } else {
        const valid = validateDnsValue(dnsForm.type, dnsForm.value);
        if (valid === false) {
          showToast(getDnsValueError(dnsForm.type), 'error');
          return;
        }
      }

      dnsFormSaving.value = true;
      try {
        // 按平台组装请求体：阿里云/腾讯云带线路+权重+优先级；Cloudflare 带代理状态；AWS 仅基础字段
        const platform = dnsCurrentPlatform.value;
        const body = {
          domain_name: dnsForm.domain_name,
          rr: dnsForm.rr,
          type: dnsForm.type,
          value: recordValue,
          ttl: dnsForm.ttl,
          remark: dnsForm.remark,
        };
        if (platform === 'aliyun' || platform === 'tencent') {
          body.priority = dnsForm.priority;
          body.line = dnsForm.line_type === 'default' ? 'default' : dnsForm.line;
          body.weight = dnsForm.load_balance === 'round_robin' ? 0 : (dnsForm.weight || 1);
        } else if (platform === 'cloudflare') {
          body.proxied = dnsForm.proxied;
        }

        let res;
        if (dnsFormEditing.value) {
          body.record_id = dnsForm.record_id;
          body.status = dnsForm.status || '';
          res = await authFetch(dnsApi('/dns'), {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
          });
        } else {
          res = await authFetch(dnsApi('/dns'), {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
          });
        }

        const json = await res.json();
        if (json.code === 0) {
          showToast(dnsFormEditing.value ? '修改成功' : '添加成功');
          dnsFormVisible.value = false;
          // 重新加载DNS记录
          showDnsRecords({ domain_name: dnsForm.domain_name });
        } else {
          showToast(json.msg || '操作失败', 'error');
        }
      } catch (e) {
        showToast('网络请求失败', 'error');
      } finally {
        dnsFormSaving.value = false;
      }
    }

    async function toggleDnsRecordStatus(rec) {
      const newStatus = rec.status === 'ENABLE' ? 'Disable' : 'Enable';
      const actionText = newStatus === 'Enable' ? '启用' : '禁用';
      if (!confirm('确定' + actionText + '解析记录 "' + rec.rr + ' ' + rec.type + ' ' + rec.value + '" 吗？')) return;
      try {
        const res = await authFetch(dnsApi('/dns/status'), {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ record_id: rec.record_id, status: newStatus }),
        });
        const json = await res.json();
        if (json.code === 0) {
          showToast(actionText + '成功');
          // 重新加载
          showDnsRecords({ domain_name: dnsCurrentDomain.value });
        } else {
          showToast(json.msg || actionText + '失败', 'error');
        }
      } catch (e) {
        showToast('网络请求失败', 'error');
      }
    }

    async function deleteDnsRecord(rec) {
      if (!confirm('确定删除解析记录 "' + rec.rr + ' ' + rec.type + ' ' + rec.value + '" 吗？')) return;
      try {
        const res = await authFetch(dnsApi('/dns'), {
          method: 'DELETE',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ record_id: rec.record_id }),
        });
        const json = await res.json();
        if (json.code === 0) {
          showToast('删除成功');
          // 重新加载
          showDnsRecords({ domain_name: dnsCurrentDomain.value });
        } else {
          showToast(json.msg || '删除失败', 'error');
        }
      } catch (e) {
        showToast('网络请求失败', 'error');
      }
    }

    // ============ 用户管理 ============
    const userLoading = ref(false);
    const userSaving = ref(false);
    const userList = ref([]);
    const userPage = ref(1);
    const userSize = ref(20);
    const userTotal = ref(0);
    const userPageInput = ref(1);
    const userModalVisible = ref(false);
    const userEditing = ref(false);
    const userEditingId = ref(null);
    const userRoleDrpOpen = ref(false);

    const userFilters = reactive({
      keyword: '',
    });

    const userForm = reactive({
      username: '',
      password: '',
      email: '',
      real_name: '',
      role: 'user',
    });

    const userTotalPages = computed(() => Math.ceil(userTotal.value / userSize.value) || 1);

    function userRoleClass(role) {
      const map = { admin: 'tag tag-danger', readonly: 'tag tag-default', user: 'tag tag-success' };
      return map[role] || 'tag tag-default';
    }

    function userRoleLabel(role) {
      const map = { admin: '管理员', readonly: '只读', user: '普通用户' };
      return map[role] || role;
    }

    async function fetchUsers() {
      userLoading.value = true;
      try {
        const params = new URLSearchParams();
        params.append('page', String(userPage.value));
        params.append('size', String(userSize.value));
        if (userFilters.keyword) params.append('keyword', userFilters.keyword);

        const res = await authFetch('/api/user/?' + params.toString());
        const json = await res.json();
        if (json.code === 401) { logout(); return; }
        if (json.code === 0) {
          userList.value = json.data.list || [];
          userTotal.value = json.data.total || 0;
        } else {
          showToast(json.msg || '查询失败', 'error');
        }
      } catch (e) {
        showToast('网络请求失败', 'error');
      } finally {
        userLoading.value = false;
      }
    }

    function openUserModal(item) {
      if (item) {
        userEditing.value = true;
        userEditingId.value = item.id;
        userForm.username = item.username || '';
        userForm.password = '';
        userForm.email = item.email || '';
        userForm.real_name = item.real_name || '';
        userForm.role = item.role || 'user';
      } else {
        userEditing.value = false;
        userEditingId.value = null;
        userForm.username = '';
        userForm.password = '';
        userForm.email = '';
        userForm.real_name = '';
        userForm.role = 'user';
      }
      userModalVisible.value = true;
    }

    async function saveUser() {
      if (!userForm.username) { showToast('请输入用户名', 'error'); return; }
      if (!userEditing.value && !userForm.password) { showToast('请输入密码', 'error'); return; }

      userSaving.value = true;
      try {
        const body = {
          username: userForm.username,
          email: userForm.email,
          real_name: userForm.real_name,
          role: userForm.role,
        };
        if (userForm.password) body.password = userForm.password;

        let res;
        if (userEditing.value) {
          res = await authFetch('/api/user/' + userEditingId.value, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
          });
        } else {
          res = await authFetch('/api/user/', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
          });
        }
        const json = await res.json();
        if (json.code === 0) {
          showToast(userEditing.value ? '更新成功' : '创建成功');
          userModalVisible.value = false;
          fetchUsers();
        } else {
          showToast(json.msg || '保存失败', 'error');
        }
      } catch (e) {
        showToast('网络请求失败', 'error');
      } finally {
        userSaving.value = false;
      }
    }

    async function deleteUser(item) {
      if (!confirm('确定删除用户 "' + item.username + '" 吗？此操作不可撤销。')) return;
      try {
        const res = await authFetch('/api/user/' + item.id, { method: 'DELETE' });
        const json = await res.json();
        if (json.code === 0) {
          showToast('删除成功');
          fetchUsers();
        } else {
          showToast(json.msg || '删除失败', 'error');
        }
      } catch (e) {
        showToast('网络请求失败', 'error');
      }
    }

    // 解锁被锁定的用户
    async function unlockUser(item) {
      if (!confirm('确定解锁用户 "' + item.username + '" 吗？')) return;
      try {
        const res = await authFetch('/api/user/' + item.id + '/unlock', { method: 'PUT' });
        const json = await res.json();
        if (json.code === 0) {
          showToast('解锁成功');
          fetchUsers();
        } else {
          showToast(json.msg || '解锁失败', 'error');
        }
      } catch (e) {
        showToast('网络请求失败', 'error');
      }
    }

    function userSearch() {
      userPage.value = 1;
      userPageInput.value = 1;
      fetchUsers();
    }

    function userResetFilters() {
      userFilters.keyword = '';
      userPage.value = 1;
      userPageInput.value = 1;
      fetchUsers();
    }

    function userOnSizeChange() {
      userPage.value = 1;
      userPageInput.value = 1;
      nextTick(() => fetchUsers());
    }

    function userGoPage(p) {
      userPage.value = p;
      userPageInput.value = p;
      fetchUsers();
    }

    function userJumpPage() {
      const p = userPageInput.value;
      if (p >= 1 && p <= userTotalPages.value) userGoPage(p);
    }

    // ============ 云平台账号管理 ============
    const accountLoading = ref(false);
    const accountSaving = ref(false);
    const accountList2 = ref([]);
    const accountPage = ref(1);
    const accountSize = ref(20);
    const accountTotal = ref(0);
    const accountPageInput = ref(1);
    const accountModalVisible = ref(false);
    const accountEditing = ref(false);
    const accountEditingId = ref(null);

    const accountFilters = reactive({
      keyword: '',
      platform: '',
      site: '',
    });

    const accountForm = reactive({
      platform: 'aliyun',
      site: 'cn',
      name: '',
      access_key: '',
      access_secret: '',
      enabled: 1,
    });

    const accountTotalPages = computed(() => Math.ceil(accountTotal.value / accountSize.value) || 1);

    function accountPlatformLabel(platform) {
      const map = { aliyun: '阿里云', aws: 'AWS', tencent: '腾讯云', cloudflare: 'Cloudflare' };
      return map[platform] || platform;
    }

    function accountSiteLabel(site) {
      const map = { cn: '国内站', intl: '国际站' };
      return map[site] || site;
    }

    function ecsPlatformLabel(platform) {
      const map = { aliyun: '阿里云', aws: 'AWS', tencent: '腾讯云', cloudflare: 'Cloudflare' };
      return map[platform] || platform;
    }

    function domainPlatformLabel(platform) {
      const map = { aliyun: '阿里云', aws: 'AWS', tencent: '腾讯云', cloudflare: 'Cloudflare' };
      return map[platform] || platform;
    }

    async function fetchAccounts() {
      accountLoading.value = true;
      try {
        const params = new URLSearchParams();
        params.append('page', String(accountPage.value));
        params.append('size', String(accountSize.value));
        if (accountFilters.keyword) params.append('keyword', accountFilters.keyword);
        if (accountFilters.platform) params.append('platform', accountFilters.platform);
        if (accountFilters.site) params.append('site', accountFilters.site);

        const res = await authFetch('/api/account/?' + params.toString());
        const json = await res.json();
        if (json.code === 401) { logout(); return; }
        if (json.code === 0) {
          accountList2.value = json.data.list || [];
          accountTotal.value = json.data.total || 0;
        } else {
          showToast(json.msg || '查询失败', 'error');
        }
      } catch (e) {
        showToast('网络请求失败', 'error');
      } finally {
        accountLoading.value = false;
      }
    }

    function openAccountModal(item) {
      if (item) {
        accountEditing.value = true;
        accountEditingId.value = item.id;
        accountForm.platform = item.platform || 'aliyun';
        accountForm.site = item.site || 'cn';
        accountForm.name = item.name || '';
        // 编辑时不预填密钥（留空即沿用原值，避免被校验当成"只改了其中一项"而误拦截）
        accountForm.access_key = '';
        accountForm.access_secret = '';
        accountForm.enabled = item.enabled ? 1 : 0;
      } else {
        accountEditing.value = false;
        accountEditingId.value = null;
        accountForm.platform = 'aliyun';
        accountForm.site = 'cn';
        accountForm.name = '';
        accountForm.access_key = '';
        accountForm.access_secret = '';
        accountForm.enabled = 1;
      }
      accountModalVisible.value = true;
    }

    async function saveAccount() {
      if (!accountForm.name) { showToast('请输入账号名称', 'error'); return; }
      // 创建模式才强制要求 AccessKey/Secret；编辑模式未改则留空，由后端沿用现有值
      if (!accountEditing.value) {
        if (!accountForm.access_key) { showToast('请输入AccessKey', 'error'); return; }
        if (!accountForm.access_secret) { showToast('请输入AccessSecret', 'error'); return; }
      }
      // 编辑模式下 AccessKey 与 AccessSecret 必须成对修改：云厂商密钥是成对校验的，
      // 只改其一会导致与云端不匹配（如旧AK+新SK）→ resolveAccountID 返回 400；
      // 两者都留空则保持原值不变。
      if (accountEditing.value) {
        const hasAK = !!(accountForm.access_key && accountForm.access_key !== '***');
        const hasSK = !!(accountForm.access_secret && accountForm.access_secret !== '***');
        if (hasAK !== hasSK) {
          showToast('AccessKey 与 AccessSecret 须成对更换（只改一个会导致校验失败）', 'error');
          return;
        }
      }

      accountSaving.value = true;
      try {
        const body = {
          name: accountForm.name,
          platform: accountForm.platform,
          site: accountForm.site,
          enabled: accountForm.enabled,
        };
        // 仅当为真实值（非掩码 "***"、非空）时才提交，避免把掩码当新值覆盖真实 key
        if (accountForm.access_key && accountForm.access_key !== '***') body.access_key = accountForm.access_key;
        if (accountForm.access_secret && accountForm.access_secret !== '***') body.access_secret = accountForm.access_secret;

        let res;
        if (accountEditing.value) {
          res = await authFetch('/api/account/' + accountEditingId.value, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
          });
        } else {
          res = await authFetch('/api/account/', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
          });
        }
        const json = await res.json();
        if (json.code === 0) {
          showToast(accountEditing.value ? '更新成功' : '创建成功');
          accountModalVisible.value = false;
          fetchAccounts();
        } else {
          showToast(json.msg || '保存失败', 'error');
        }
      } catch (e) {
        showToast('网络请求失败', 'error');
      } finally {
        accountSaving.value = false;
      }
    }

    async function deleteAccount(item) {
      if (!confirm('确定删除账号 "' + item.name + '" 吗？此操作不可撤销。')) return;
      try {
        const res = await authFetch('/api/account/' + item.id, { method: 'DELETE' });
        const json = await res.json();
        if (json.code === 0) {
          showToast('删除成功');
          fetchAccounts();
        } else {
          showToast(json.msg || '删除失败', 'error');
        }
      } catch (e) {
        showToast('网络请求失败', 'error');
      }
    }

    function accountSearch() {
      accountPage.value = 1;
      accountPageInput.value = 1;
      fetchAccounts();
    }

    function accountResetFilters() {
      accountFilters.keyword = '';
      accountFilters.platform = '';
      accountFilters.site = '';
      accountPage.value = 1;
      accountPageInput.value = 1;
      fetchAccounts();
    }

    function accountOnSizeChange() {
      accountPage.value = 1;
      accountPageInput.value = 1;
      nextTick(() => fetchAccounts());
    }

    function accountGoPage(p) {
      accountPage.value = p;
      accountPageInput.value = p;
      fetchAccounts();
    }

    function accountJumpPage() {
      const p = accountPageInput.value;
      if (p >= 1 && p <= accountTotalPages.value) accountGoPage(p);
    }

    // 路由导航（支持浏览器前进后退 / Ctrl+单击新窗口）
    function navigate(arg1, arg2) {
      let ev = null, path;
      if (typeof arg1 === 'string') { path = arg1; }
      else { ev = arg1; path = arg2; }
      // Ctrl / ⌘ / 中键：统一在新标签打开。不少入口是 <a> 却没有 href，
      // 只放行给浏览器会出现「按住 Ctrl 点了没反应」。
      if ((ev && (ev.ctrlKey || ev.metaKey || ev.button === 1)) || navMods.ctrl || navMods.meta || navMods.middle) {
        if (ev) ev.preventDefault();
        window.open(path, '_blank', 'noopener');
        return;
      }
      if (ev) ev.preventDefault();
      if (path.startsWith('/bastion') && !bastionEnabled.value) return;
      if (path === '/bastion') {
        window.history.pushState({}, '', '/bastion');
        switchTab('bastionAssets');
      } else if (path === '/bastion/groups') {
        if (!isAdmin.value) { showToast('仅管理员可访问', 'error'); return; }
        window.history.pushState({}, '', '/bastion/groups');
        switchTab('bastionGroups');
      } else if (path === '/bastion/terminal') {
        switchTab('bastionTerminal');
      } else if (path === '/bastion/credentials') {
        if (!isAdmin.value) { showToast('仅管理员可访问', 'error'); return; }
        window.history.pushState({}, '', '/bastion/credentials');
        switchTab('bastionCredentials');
      } else if (path === '/ecs') {
        window.history.pushState({}, '', '/ecs');
        switchTab('ecs');
      } else if (path === '/domains') {
        window.history.pushState({}, '', '/domains');
        switchTab('domains');
      } else if (path === '/dns') {
        window.history.pushState({}, '', '/dns');
        switchTab('dnsDomains');
      } else if (path === '/cdn') {
        window.history.pushState({}, '', '/cdn');
        switchTab('cdnDomains');
      } else if (path === '/resource-packages') {
        window.history.pushState({}, '', '/resource-packages');
        switchTab('resourcePackages');
        fetchResourcePackages();
      } else if (path === '/regions') {
        window.history.pushState({}, '', '/regions');
        switchTab('regions');
      } else if (path === '/users') {
        if (!isAdmin.value) { showToast('仅管理员可访问', 'error'); return; }
        window.history.pushState({}, '', '/users');
        switchTab('users');
      } else if (path === '/accounts') {
        if (!isAdmin.value) { showToast('仅管理员可访问', 'error'); return; }
        window.history.pushState({}, '', '/accounts');
        switchTab('accounts');
      } else if (path === '/logs') { window.location.href = '/assets/pages/logs.html?v=20261108i'; return;
      } else if (path.startsWith('/dns/')) {
        // DNS解析页面：从路径提取域名，从 query 提取 platform+account（站点），路由到对应平台解析界面
        const qIndex = path.indexOf('?');
        const pathPart = qIndex >= 0 ? path.slice(0, qIndex) : path;
        const queryPart = qIndex >= 0 ? path.slice(qIndex + 1) : '';
        const domainName = decodeURIComponent(pathPart.replace('/dns/', ''));
        const qp = new URLSearchParams(queryPart);
        dnsCurrentDomain.value = domainName;
        dnsCurrentPlatform.value = qp.get('platform') || '';
        dnsCurrentAccount.value = qp.get('account') || '';
        dnsCurrentAccountId.value = qp.get('accountId') || '';
        window.history.pushState({}, '', path);
        switchTab('dns');
        showDnsRecords({ domain_name: domainName });
      } else {
        // '/' 首页
        window.history.pushState({}, '', '/');
        switchTab('home');
      }
    }

    // 根据当前 pathname 初始化 tab
    function initRoute() {
      // 独立页顶栏带参数进来时：展开产品目录 / 打开个人资料 / 打开修改密码
      const qs = new URLSearchParams(window.location.search);
      if (qs.get('drawer') === '1') drawerOpen.value = true;
      if (qs.get('profile') === '1') openProfile();
      else if (qs.get('password') === '1') openChangePassword();
      if (window.location.pathname === '/bastion') {
        switchTab('bastionAssets');
      } else if (window.location.pathname === '/bastion/groups') {
        if (!isAdmin.value) { switchTab('home'); return; }
        switchTab('bastionGroups');
      } else if (window.location.pathname === '/bastion/terminal') {
        initBastionTerminalFromUrl();
      } else if (window.location.pathname === '/bastion/credentials') {
        if (!isAdmin.value) { switchTab('home'); return; }
        switchTab('bastionCredentials');
      } else if (window.location.pathname === '/ecs') {
        switchTab('ecs');
      } else if (window.location.pathname === '/domains') {
        switchTab('domains');
      } else if (window.location.pathname === '/dns') {
        switchTab('dnsDomains');
      } else if (window.location.pathname === '/cdn') {
        switchTab('cdnDomains');
      } else if (window.location.pathname === '/resource-packages') {
        switchTab('resourcePackages');
        fetchResourcePackages();
      } else if (window.location.pathname === '/regions') {
        switchTab('regions');
      } else if (window.location.pathname === '/users') {
        if (!isAdmin.value) { switchTab('home'); return; }
        switchTab('users');
      } else if (window.location.pathname === '/accounts') {
        if (!isAdmin.value) { switchTab('home'); return; }
        switchTab('accounts');
      } else if (window.location.pathname === '/logs' || window.location.pathname === '/logs.html') { window.location.replace('/assets/pages/logs.html?v=20261108i'); return;
      } else if (window.location.pathname.startsWith('/dns/')) {
        const domainName = decodeURIComponent(window.location.pathname.replace('/dns/', ''));
        const qp = new URLSearchParams(window.location.search);
        dnsCurrentDomain.value = domainName;
        dnsCurrentPlatform.value = qp.get('platform') || '';
        dnsCurrentAccount.value = qp.get('account') || '';
        dnsCurrentAccountId.value = qp.get('accountId') || '';
        switchTab('dns');
        showDnsRecords({ domain_name: domainName });
      } else {
        // '/' 首页
        switchTab('home');
      }
    }

    // 切换 tab（内部逻辑）
    function switchTab(tab) {
      // 离开终端页面时清理连接
      if (currentTab.value === 'bastionTerminal' && tab !== 'bastionTerminal') {
        clearInterval(bastionHeartbeat);
        if (bastionWs) { bastionWs.close(); bastionWs = null; }
        if (bastionXterm) { try { bastionXterm.dispose(); } catch(e) {} bastionXterm = null; }
      }
      currentTab.value = tab;
      // 切换页面后回到顶部：main-content 是滚动容器，v-if 换页不会自动重置 scrollTop，
      // 否则会沿用上一页的滚动位置，导致新页面“内容被顶下去/上方大片空白/底部显示不全”
      nextTick(() => {
        const mc = document.querySelector('.main-content');
        if (mc) mc.scrollTop = 0;
        window.scrollTo(0, 0);
      });
      if (tab === 'bastionAssets') {
        fetchBastionAssets();
      } else if (tab === 'bastionGroups') {
        fetchBastionGroups();
      } else if (tab === 'bastionCredentials') {
        fetchBastionCredentials();
      } else if (tab === 'ecs') {
        fetchEcsFilterOptions('', '');
        fetchEcsInstances();
        fetchEcsStats();
      } else if (tab === 'domains') {
        fetchDomains();
      } else if (tab === 'dnsDomains') {
        fetchDnsDomainFilterOptions();
        fetchDnsDomains();
      } else if (tab === 'cdnDomains') {
        fetchCdnDomainFilterOptions();
        fetchCdnDomains();
      } else if (tab === 'regions') {
        fetchRegions();
      } else if (tab === 'users') {
        fetchUsers();
      } else if (tab === 'accounts') {
        fetchAccounts();
      } else if (tab === 'home') {
        fetchHomeStats();
      }
    }

    // 顶栏页面标题（随当前页变化）；键名必须与 navigate/switchTab 使用的 tab 值一致
    const PAGE_TITLES = {
      home: '工作台',
      domains: '域名管理', dnsDomains: 'DNS解析管理', dns: 'DNS 解析记录', cdnDomains: 'CDN域名',
      ecs: 'ECS实例', regions: '实例地域开通',
      resourcePackages: '资源包',
      bastionAssets: '堡垒机资产', bastionGroups: '用户组管理', bastionCredentials: '凭证管理', bastionTerminal: '堡垒机终端',
      users: '用户管理', accounts: '云平台账号管理',
    };
    const currentPageTitle = computed(() => PAGE_TITLES[currentTab.value] || '控制台');

    // 浏览器标签页标题跟随当前页面（便于多标签区分，风格同独立页「日志排查 - 云管平台」）
    watch(currentPageTitle, (t) => {
      document.title = (t ? t + ' - ' : '') + '云管平台';
    }, { immediate: true });

    // ============ 生命周期 ============
    // 监听浏览器前进后退
    if (typeof window !== 'undefined') {
      window.addEventListener('popstate', initRoute);
    }

    onMounted(async () => {
      refreshCaptcha();
      checkLogin();
      if (isLoggedIn.value) {
        // 验证 token + 读取堡垒机配置
        try { const r = await authFetch('/api/config'); if (!r.ok) { logout(); return; }
        const j = await r.json(); if (j.code === 0) bastionEnabled.value = j.data.bastion_enabled !== false; } catch (e) { logout(); return; }
        loadColPrefs();
        fetchAccountList();
        fetchStats();
        initRoute();
      }
    });

    // ============ 地域管理（实例地域开通） ============
    const sidebarDomainOpen = ref(false);  // 侧边栏「域名」目录默认收起
    const sidebarServerOpen = ref(false); // 侧边栏「服务器」目录默认收起
    const regionLoading = ref(false);
    const regionPlatform = ref('aliyun');
    const regionList = ref([]);

    // ============ 资源包（阿里云，实时查询计费服务 BssOpenApi） ============
    const sidebarResourceOpen = ref(false);   // 侧边栏「资源」目录默认收起
    const rpLoading = ref(false);
    const rpList = ref([]);
    const rpAccounts = ref([]);
    const rpErrors = ref([]);
    const rpFilters = ref({ account: '', keyword: '', onlyRemaining: true });

    const filteredResourcePackages = computed(() => {
      const kw = (rpFilters.value.keyword || '').trim().toLowerCase();
      return rpList.value.filter(it => {
        if (rpFilters.value.account && it.account_id !== rpFilters.value.account) return false;
        // 默认只显示剩余量 > 0 的资源包（剩余量为空或非数字也视为已耗尽，过滤掉）
        if (rpFilters.value.onlyRemaining) {
          const rem = parseFloat(it.remaining_amount);
          if (isNaN(rem) || rem <= 0) return false;
        }
        if (!kw) return true;
        return (it.instance_id || '').toLowerCase().includes(kw) ||
          (it.product_code || '').toLowerCase().includes(kw) ||
          (it.remark || '').toLowerCase().includes(kw) ||
          (it.account_name || '').toLowerCase().includes(kw);
      });
    });

    function rpSelectAccount(accountId) {
      rpFilters.value.account = accountId;
    }

    function rpStatusLabel(s) {
      const map = { Available: '有效', Expired: '已过期', Closed: '已关闭', UsedUp: '已用完' };
      return map[s] || s || '-';
    }

    // ISO8601 UTC → 本地时间字符串（YYYY-MM-DD HH:mm）
    function rpTime(iso) {
      if (!iso) return '-';
      const d = new Date(iso);
      if (isNaN(d.getTime())) return iso;
      const p = n => String(n).padStart(2, '0');
      return d.getFullYear() + '-' + p(d.getMonth() + 1) + '-' + p(d.getDate()) + ' ' + p(d.getHours()) + ':' + p(d.getMinutes());
    }

    function productLabel(code) {
      const map = {
        ossbag: 'OSS存储包', cdnbag: 'CDN流量包', flowbag: '全站加速包', livebag: '直播流量包',
        vodbag: '视频点播包', dcdn: '全站加速', slbbag: 'SLB包', nlb: 'NLB包',
        rds: 'RDS包', redis: 'Redis包', mongodb: 'MongoDB包', nasbag: 'NAS包',
        fc: '函数计算包', ecs: 'ECS包', wafbag: 'WAF包', ddosbag: 'DDoS包', cdn: 'CDN包',
      };
      return map[code] || code;
    }

    async function fetchResourcePackages() {
      rpLoading.value = true;
      try {
        const res = await authFetch('/api/resource-packages');
        const json = await res.json();
        if (json.code === 401) { logout(); return; }
        if (json.code === 0) {
          rpList.value = json.data.list || [];
          if (json.data.accounts) rpAccounts.value = json.data.accounts;
          rpErrors.value = json.data.errors || [];
          if (rpErrors.value.length) {
            showToast('有 ' + rpErrors.value.length + ' 个账号获取资源包失败，详见页面底部', 'error');
          }
        } else {
          showToast(json.msg || '查询失败', 'error');
        }
      } catch (e) {
        showToast('网络请求失败: ' + e.message, 'error');
      } finally {
        rpLoading.value = false;
      }
    }

    async function fetchRegions() {
      regionLoading.value = true;
      try {
        const res = await authFetch('/api/regions?platform=' + regionPlatform.value);
        const json = await res.json();
        if (json.code === 0) {
          regionList.value = json.data || [];
        }
      } catch (e) {
        showToast('获取地域列表失败', 'error');
      } finally {
        regionLoading.value = false;
      }
    }

    async function toggleRegion(item, enabled) {
      try {
        const res = await authFetch('/api/regions/' + item.id, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ enabled: enabled }),
        });
        const json = await res.json();
        if (json.code === 0) {
          item.enabled = enabled;
          showToast(enabled ? '已开通 ' + item.region_name : '已关闭 ' + item.region_name, 'success');
        } else {
          showToast(json.msg || '更新失败', 'error');
        }
      } catch (e) {
        showToast('请求失败', 'error');
      }
    }

    function onAccountPlatformChange() {
      // Cloudflare / AWS 无国内/国际站之分，固定国际站
      if (accountForm.platform === 'cloudflare' || accountForm.platform === 'aws') {
        accountForm.site = 'intl';
      }
    }

    // ============ ECS手动管理 ============
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
        if (json.code === 0) { showToast(ecsManualEditing.value ? '更新成功' : '创建成功'); ecsManualModalVisible.value = false; fetchEcsInstances(); }
        else showToast(json.msg || '操作失败', 'error');
      } catch (e) { showToast('请求失败', 'error'); } finally { ecsManualSaving.value = false; }
    }
    async function deleteEcsManual(item) {
      if (!confirm('确定删除实例 "' + (item.instance_name || item.instance_id) + '" 吗？')) return;
      try {
        const res = await authFetch('/api/ecs/' + item.id, { method: 'DELETE' });
        const json = await res.json();
        if (json.code === 0) { showToast('删除成功'); fetchEcsInstances(); }
        else showToast(json.msg || '删除失败', 'error');
      } catch (e) { showToast('请求失败', 'error'); }
    }

    // ============ ECS列表行内编辑（双击实例名称/SSH端口/备注直接改，均为本地维护，不调用云端） ============
    const ecsInlineEdit = ref({ id: null, field: '' });   // 正在编辑的行+字段
    const ecsInlineValue = ref('');                        // 编辑中的值
    const ecsInlineSaving = ref(false);                    // 保存中（防止重复提交）

    function startEcsInlineEdit(item, field) {
      if (!isAdmin.value || ecsInlineSaving.value) return;
      ecsInlineEdit.value = { id: item.id, field: field };
      if (field === 'ssh_port') {
        ecsInlineValue.value = String(item.ssh_port || 22);
      } else if (field === 'description') {
        ecsInlineValue.value = item.description || '';
      } else {
        ecsInlineValue.value = item.instance_name || '';
      }
      nextTick(() => {
        const el = document.querySelector('.ecs-inline-input');
        if (el) { el.focus(); try { el.select(); } catch (e) {} }
      });
    }

    function cancelEcsInlineEdit() {
      ecsInlineEdit.value = { id: null, field: '' };
      ecsInlineValue.value = '';
    }

    async function saveEcsInlineEdit(item) {
      // 防重复触发（Enter 与 blur 连续触发、保存中再触发，直接忽略）
      if (ecsInlineSaving.value) return;
      const edit = ecsInlineEdit.value;
      if (!edit.id || edit.id !== item.id) return;

      const field = edit.field;
      let value;
      if (field === 'ssh_port') {
        value = parseInt(ecsInlineValue.value, 10);
        if (isNaN(value) || value < 1 || value > 65535) { cancelEcsInlineEdit(); return; } // 空/非法 → 不改，不报错
        if (value === (item.ssh_port || 22)) { cancelEcsInlineEdit(); return; }            // 没改动 → 不改
      } else if (field === 'name') {
        value = (ecsInlineValue.value || '').trim();
        if (!value) { cancelEcsInlineEdit(); return; }     // 空 → 不改（不再报"实例名称不能为空"）
        if (value === item.instance_name) { cancelEcsInlineEdit(); return; } // 没改动 → 不改
      } else { // description 备注
        value = (ecsInlineValue.value || '').trim();
        if (value === (item.description || '')) { cancelEcsInlineEdit(); return; } // 空/没改动 → 不改
      }

      ecsInlineSaving.value = true;
      try {
        let url, body;
        if (field === 'ssh_port') { url = '/api/ecs/' + item.id + '/ssh-port'; body = { ssh_port: value }; }
        else if (field === 'name') { url = '/api/ecs/' + item.id + '/name'; body = { instance_name: value }; }
        else { url = '/api/ecs/' + item.id + '/description'; body = { description: value }; }
        const res = await authFetch(url, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
        const json = await res.json();
        if (json.code === 0) {
          if (field === 'ssh_port') { item.ssh_port = value; }
          else if (field === 'name') { item.instance_name = value; }
          else { item.description = value; item.description_locked = true; }
          showToast('已保存');
        } else {
          showToast(json.msg || '保存失败', 'error');
        }
      } catch (e) {
        showToast('保存失败: ' + e.message, 'error');
      } finally {
        ecsInlineSaving.value = false;
        cancelEcsInlineEdit();
      }
    }

    // ============ CDN域名（阿里云CDN / 阿里云DCDN / AWS CloudFront） ============
    const cdnDomainLoading = ref(false);
    const cdnDomainList = ref([]);
    const cdnDomainFilters = reactive({ keyword: '', platform: '', account: '', status: '运行中' });
    const cdnDomainPlatformList = ref([]);   // 平台列表（用于左侧抽屉分组）
    const cdnDomainAccountList = ref([]);    // 账号列表（含平台，{name, platform}）
    const cdnDomainPage = ref(1);
    const cdnDomainSize = ref(20);
    const cdnDomainTotal = ref(0);
    const cdnDomainPageInput = ref(1);
    const cdnDomainTotalPages = computed(() => Math.ceil(cdnDomainTotal.value / cdnDomainSize.value) || 1);

    function cdnDomainPlatformLabel(p) {
      const map = { aliyun: '阿里云', aws: 'AWS' };
      return map[p] || p;
    }

    // CDN状态徽章配色（正常运行=绿，已停止/配置失败/审核未通过=红，其他=默认）
    function cdnStatusClass(s) {
      if (s === '正常运行' || s === '运行中') return 'tag-success';
      if (s === '已停止' || s === '配置失败' || s === '审核未通过') return 'tag-offline';
      return 'tag-platform';
    }

    // CDN产品类型：cdn=CDN / dcdn=全站加速（阿里云独有）
    function cdnProductLabel(p) {
      const map = { cdn: 'CDN', dcdn: '全站加速' };
      return map[p] || (p || 'CDN');
    }

    // CDN SSL 证书剩余天数（基于同步得到的 cert_expire_time 计算，null 表示无证书/无到期时间）
    function cdnSslDaysLeft(expire) {
      if (!expire) return null;
      const d = new Date(expire);
      if (isNaN(d.getTime())) return null;
      return Math.ceil((d.getTime() - Date.now()) / 86400000);
    }

    // CDN 列表排序：仅支持"SSL剩余"列；无证书(null)永远排最后，避免干扰
    const cdnDomainSortDir = ref('');   // ''=不排序, 'asc'/'desc'
    const cdnDomainSortIcon = computed(() => cdnDomainSortDir.value === 'asc' ? '▲' : cdnDomainSortDir.value === 'desc' ? '▼' : '⇅');
    const cdnDomainSortedList = computed(() => {
      const list = cdnDomainList.value;
      if (!cdnDomainSortDir.value) return list;
      const dir = cdnDomainSortDir.value === 'asc' ? 1 : -1;
      return [...list].sort((a, b) => {
        const da = cdnSslDaysLeft(a.cert_expire_time);
        const db = cdnSslDaysLeft(b.cert_expire_time);
        if (da === null && db === null) return 0;
        if (da === null) return 1;
        if (db === null) return -1;
        return (da - db) * dir;
      });
    });
    function cdnDomainToggleSslSort() {
      cdnDomainSortDir.value = cdnDomainSortDir.value === 'asc' ? 'desc' : 'asc';
    }

    // 产品徽章配色（CDN=蓝，全站加速=紫）
    function cdnProductClass(p) {
      if (p === 'dcdn') return 'tag-purple';
      return 'tag-platform';
    }

    // 拉取筛选选项（平台 + 账号[含平台]，用于左侧抽屉分组）
    async function fetchCdnDomainFilterOptions() {
      try {
        const res = await authFetch('/api/cdn-domains/filter-options');
        const json = await res.json();
        if (json.code === 0) {
          cdnDomainPlatformList.value = json.data.platforms || [];
          cdnDomainAccountList.value = json.data.accounts || [];
        }
      } catch (e) { /* 抽屉筛选可降级为不显示 */ }
    }

    // 左侧抽屉：按平台/账号过滤
    function cdnSelectFilter(platform, account) {
      cdnDomainFilters.platform = platform;
      cdnDomainFilters.account = account;
      cdnDomainPage.value = 1;
      fetchCdnDomains();
    }

    // 某平台下的账号列表
    function cdnAccountsOfPlatform(pf) {
      return cdnDomainAccountList.value.filter(a => a.platform === pf).map(a => a.name);
    }

    async function fetchCdnDomains() {
      cdnDomainLoading.value = true;
      try {
        const params = new URLSearchParams();
        params.append('page', String(cdnDomainPage.value));
        params.append('size', String(cdnDomainSize.value));
        if (cdnDomainFilters.keyword) params.append('keyword', cdnDomainFilters.keyword);
        if (cdnDomainFilters.platform) params.append('platform', cdnDomainFilters.platform);
        if (cdnDomainFilters.account) params.append('account_name', cdnDomainFilters.account);
        if (cdnDomainFilters.status) params.append('status', cdnDomainFilters.status);
        const res = await authFetch('/api/cdn-domains?' + params.toString());
        const json = await res.json();
        if (json.code === 0) {
          cdnDomainList.value = json.data.list || [];
          cdnDomainTotal.value = json.data.total || 0;
          cdnDomainPageInput.value = cdnDomainPage.value;
        }
      } catch (e) { /* 静默失败，保持列表原状 */ }
      finally { cdnDomainLoading.value = false; }
    }

    function cdnDomainSearch() {
      cdnDomainPage.value = 1;
      fetchCdnDomains();
    }

    function cdnDomainResetFilters() {
      cdnDomainFilters.keyword = '';
      cdnDomainFilters.platform = '';
      cdnDomainFilters.account = '';
      cdnDomainFilters.status = '运行中';   // 重置回到默认：仅显示运行中
      cdnDomainPage.value = 1;
      fetchCdnDomains();
    }

    function cdnDomainGoPage(p) {
      if (p < 1 || p > cdnDomainTotalPages.value) return;
      cdnDomainPage.value = p;
      fetchCdnDomains();
    }

    function cdnDomainJumpPage() {
      cdnDomainGoPage(cdnDomainPageInput.value || 1);
    }

    function cdnDomainOnSizeChange() {
      cdnDomainPage.value = 1;
      fetchCdnDomains();
    }

    // 同步CDN域名（阿里云CDN / AWS CloudFront；腾讯云/Cloudflare 无此资源会自动跳过）
    async function syncCdnDomains() {
      if (cdnDomainSyncing.value) { showToast('CDN同步正在进行中，请稍候', 'warning'); return; }
      cdnDomainSyncing.value = true;
      try {
        const res = await authFetch('/api/sync/cdn', { method: 'POST' });
        const json = await res.json();
        if (json.code === 0) {
          showToast('CDN同步任务已触发，完成后自动刷新');
          // 静默监听 SSE：仅用于完成后刷新列表，不在页面展示日志（日志在服务端日志文件）
          await readSSELog('/api/sync/progress?task_id=' + json.task_id, (syncAbortController = new AbortController()).signal,
            () => {},
            () => { showToast('CDN同步完成'); fetchCdnDomains(); },
            () => {}
          );
        } else {
          showToast(json.msg || 'CDN同步失败', 'error');
        }
      } catch (e) {
        showToast('同步请求失败', 'error');
      } finally {
        cdnDomainSyncing.value = false;
      }
    }

    // ============ CDN同步状态（syncCdnDomains 使用） ============
    const cdnDomainSyncing = ref(false);

    // ============ 资源页「按账号同步」弹窗（CDN/DNS/ECS/域名 共用） ============
    // 打开弹窗时列出所有启用账号，勾选要同步的账号（默认全选），提交时只同步这些账号。
    const resourceSyncVisible = ref(false);
    const resourceSyncType = ref('');        // cdn / dns / ecs / domains
    const resourceSyncTypeName = ref('');
    const resourceSyncList = ref([]);        // 候选账号 [{id, name, platform, account_id, enabled}]
    const resourceSyncSelected = ref([]);    // 选中的 account_id 列表

    const resourceSyncAll = computed({
      get: () => resourceSyncList.value.length > 0 && resourceSyncList.value.every(a => resourceSyncSelected.value.includes(a.account_id)),
      set: (v) => { resourceSyncSelected.value = v ? resourceSyncList.value.map(a => a.account_id) : []; },
    });

    async function openResourceSync(type, typeName) {
      resourceSyncType.value = type;
      resourceSyncTypeName.value = typeName;
      // 拉取所有启用账号用于勾选
      try {
        const res = await authFetch('/api/account/?size=1000');
        const json = await res.json();
        if (json.code === 0) {
          resourceSyncList.value = (json.data.list || []).filter(a => a.enabled === 1 || a.enabled === true);
        }
      } catch (e) { /* 忽略，列表为空时弹窗会提示无可用账号 */ }
      // 默认全选
      resourceSyncSelected.value = resourceSyncList.value.map(a => a.account_id);
      resourceSyncVisible.value = true;
    }

    async function submitResourceSync() {
      if (resourceSyncSelected.value.length === 0) { showToast('请至少选择一个账号', 'error'); return; }
      resourceSyncVisible.value = false;
      const type = resourceSyncType.value;
      try {
        const res = await authFetch('/api/sync/' + type, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ account_ids: resourceSyncSelected.value }),
        });
        const json = await res.json();
        if (json.code === 0) {
          showToast('同步任务已触发：' + resourceSyncTypeName.value);
          await readSSELog('/api/sync/progress?task_id=' + json.task_id, (syncAbortController = new AbortController()).signal,
            () => {},
            () => {
              showToast('同步完成，已刷新列表');
              if (type === 'cdn') fetchCdnDomains();
              else if (type === 'dns') fetchDnsDomains();
              else if (type === 'ecs') fetchEcsInstances();
              else if (type === 'domains') fetchDomains();
            },
            () => {}
          );
        } else {
          showToast(json.msg || '同步失败', 'error');
        }
      } catch (e) {
        showToast('同步请求失败', 'error');
      }
    }

    // ============ 堡垒机 JS ============
    const sidebarBastionOpen = ref(false);

    // 资产列表（直接显示ECS，按用户组过滤）
    const bastionAssetLoading = ref(false);
    const bastionAssetList = ref([]);
    const bastionAssetKeyword = ref('');
    const bastionAssetPlatform = ref('');
    const bastionAssetAccount = ref('');
    const bastionAssetStatus = ref('');
    const bastionAssetRegion = ref('');
    const bastionAssetAccounts = ref([]);
    // 地域选项：根据目录树当前选中的平台/账号动态生成（该账号实际拥有的地域）
    const bastionAssetRegions = computed(() => {
      let list = bastionAssetList.value;
      if (bastionAssetPlatform.value) list = list.filter(a => a.platform === bastionAssetPlatform.value);
      if (bastionAssetAccount.value) list = list.filter(a => a.account_name === bastionAssetAccount.value);
      return [...new Set(list.map(a => a.region_name).filter(Boolean))].sort();
    });
    const bastionAssetPlatformList = computed(() => [...new Set(bastionAssetList.value.map(a => a.platform).filter(Boolean))].sort());
    const bastionAssetAccountOptions = computed(() => {
      const map = new Map();
      bastionAssetList.value.forEach(a => {
        if (a.account_name && a.platform) map.set(a.account_name, { name: a.account_name, platform: a.platform });
      });
      return [...map.values()].sort((x, y) => x.platform.localeCompare(y.platform) || x.name.localeCompare(y.name));
    });

    function bastionAssetAccountsOfPlatform(pf) {
      return bastionAssetAccountOptions.value.filter(a => a.platform === pf).map(a => a.name);
    }

    function bastionAssetSelectFilter(platform, account) {
      bastionAssetPlatform.value = platform;
      bastionAssetAccount.value = account;
      bastionAssetRegion.value = ''; // 切换平台/账号时重置地域（地域选项随之变化）
    }

    const filteredBastionAssetList = computed(() => {
      let list = bastionAssetList.value;
      if (bastionAssetPlatform.value) list = list.filter(a => a.platform === bastionAssetPlatform.value);
      if (bastionAssetAccount.value) list = list.filter(a => a.account_name === bastionAssetAccount.value);
      if (bastionAssetStatus.value) list = list.filter(a => a.status === bastionAssetStatus.value);
      if (bastionAssetRegion.value) list = list.filter(a => a.region_name === bastionAssetRegion.value);
      if (bastionAssetKeyword.value) {
        const kw = bastionAssetKeyword.value.toLowerCase();
        list = list.filter(a =>
          (a.instance_name && a.instance_name.toLowerCase().includes(kw)) ||
          (a.instance_id && a.instance_id.toLowerCase().includes(kw)) ||
          (a.public_ip_address && a.public_ip_address.includes(kw)) ||
          (a.eip_address && a.eip_address.includes(kw)) ||
          (a.private_ip_address && a.private_ip_address.includes(kw))
        );
      }
      return list;
    });

    // 资产表排序：实例ID/名称列，默认不排序，点击切换升/降
    const bastionAssetSortDir = ref('');   // ''=不排序, 'asc'/'desc'
    const bastionAssetSortIcon = computed(() => bastionAssetSortDir.value === 'asc' ? '▲' : bastionAssetSortDir.value === 'desc' ? '▼' : '⇅');
    const nameOfBastion = (a) => (a.instance_name || a.InstanceName || '').toString().toLowerCase();
    const bastionAssetSortedList = computed(() => {
      const list = filteredBastionAssetList.value;
      if (!bastionAssetSortDir.value) return list;
      const dir = bastionAssetSortDir.value === 'asc' ? 1 : -1;
      return [...list].sort((a, b) => nameOfBastion(a).localeCompare(nameOfBastion(b)) * dir);
    });
    function toggleBastionAssetNameSort() {
      bastionAssetSortDir.value = bastionAssetSortDir.value === 'asc' ? 'desc' : 'asc';
    }

    function refreshBastionAssetFilters() {
      const acctSet = new Set();
      bastionAssetList.value.forEach(a => {
        if (a.account_name) acctSet.add(a.account_name);
      });
      bastionAssetAccounts.value = [...acctSet].sort();
      // 地域选项改为由 bastionAssetRegions(computed) 根据当前平台/账号动态生成
    }
    // 凭证缓存（按资产ID）：{ assetId: [cred1, cred2, ...] }
    let bastionAssetCredCache = {};
    let bastionCredCacheLoading = false;

    async function preloadBastionAssetCredCache() {
      if (bastionCredCacheLoading) return;
      bastionCredCacheLoading = true;
      const assets = bastionAssetList.value;
      for (let i = 0; i < assets.length; i++) {
        const aid = assets[i].id;
        if (bastionAssetCredCache[aid]) continue;
        try {
          const r = await authFetch('/api/bastion/assets/' + aid + '/credentials');
          const j = await r.json();
          if (j.code === 0 && j.data.length > 0) bastionAssetCredCache[aid] = j.data;
        } catch (e) {}
      }
      bastionCredCacheLoading = false;
    }

    async function fetchBastionAssets() {
      bastionAssetLoading.value = true;
      try { const res = await authFetch('/api/bastion/assets'); const json = await res.json(); if (json.code === 0) { bastionAssetList.value = json.data || []; refreshBastionAssetFilters(); preloadBastionAssetCredCache(); } } catch (e) {} finally { bastionAssetLoading.value = false; }
    }

    // 凭证管理
    const bastionCredLoading = ref(false);
    const bastionCredSaving = ref(false);
    const bastionCredList = ref([]);
    const bastionCredShortList = ref([]);
    const bastionCredModalVisible = ref(false);
    const bastionCredEditing = ref(false);
    const bastionCredForm = reactive({ name: '', auth_type: 'password', username: '', password: '', private_key: '', passphrase: '' });
    const bastionCredEditId = ref(null);

    async function fetchBastionCredentials() {
      bastionCredLoading.value = true;
      try { const res = await authFetch('/api/bastion/credentials'); const json = await res.json(); if (json.code === 0) { bastionCredList.value = json.data || []; bastionCredShortList.value = json.data || []; } } catch (e) {} finally { bastionCredLoading.value = false; }
    }
    function openBastionCredModal(item) {
      if (item) { bastionCredEditing.value = true; bastionCredEditId.value = item.id; bastionCredForm.name = item.name; bastionCredForm.auth_type = item.auth_type; bastionCredForm.username = item.username; bastionCredForm.password = ''; bastionCredForm.private_key = ''; bastionCredForm.passphrase = ''; }
      else { bastionCredEditing.value = false; bastionCredForm.name = ''; bastionCredForm.auth_type = 'password'; bastionCredForm.username = ''; bastionCredForm.password = ''; bastionCredForm.private_key = ''; bastionCredForm.passphrase = ''; }
      bastionCredModalVisible.value = true;
    }
    async function saveBastionCred() {
      if (!bastionCredForm.name || !bastionCredForm.username) { showToast('名称和用户名必填', 'error'); return; }
      if (!bastionCredEditing.value) {
        if (bastionCredForm.auth_type === 'password' && !bastionCredForm.password) { showToast('密码不能为空', 'error'); return; }
        if (bastionCredForm.auth_type === 'private_key' && !bastionCredForm.private_key) { showToast('私钥内容不能为空', 'error'); return; }
      }
      bastionCredSaving.value = true;
      try {
        let res;
        if (bastionCredEditing.value) res = await authFetch('/api/bastion/credentials/' + bastionCredEditId.value, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(bastionCredForm) });
        else res = await authFetch('/api/bastion/credentials', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(bastionCredForm) });
        const json = await res.json();
        if (json.code === 0) { showToast(bastionCredEditing.value ? '更新成功' : '创建成功'); bastionCredModalVisible.value = false; fetchBastionCredentials(); }
        else showToast(json.msg || '操作失败', 'error');
      } catch (e) { showToast('请求失败', 'error'); } finally { bastionCredSaving.value = false; }
    }
    async function deleteBastionCred(item) {
      if (!confirm('确定删除凭证 "' + item.name + '" 吗？')) return;
      try {
        const res = await authFetch('/api/bastion/credentials/' + item.id, { method: 'DELETE' });
        const json = await res.json().catch(() => ({}));
        if (json.code === 0) { showToast('删除成功'); fetchBastionCredentials(); }
        else showToast(json.msg || '删除失败', 'error');
      } catch (e) { showToast('请求失败', 'error'); }
    }

    // 用户组管理
    const bastionGroupLoading = ref(false);
    const bastionGroupSaving = ref(false);
    const bastionGroupList = ref([]);
    const bastionGroupModalVisible = ref(false);
    const bastionGroupEditing = ref(false);
    const bastionGroupEditId = ref(null);
    const bastionGroupForm = reactive({ name: '', description: '' });
    // 组成员
    const bastionGroupMemberModalVisible = ref(false);
    const bastionGroupMemberName = ref('');
    const bastionGroupMemberList = ref([]);
    const bastionGroupMemberGroupId = ref(null);
    const bastionGroupAddUserId = ref(0);
    const bastionAllUserList = ref([]);
    // 组资产授权
    const bastionGroupAssetModalVisible = ref(false);
    const bastionGroupAssetTab = ref('assets'); // 'assets' | 'credentials'
    const bastionGroupAssetName = ref('');
    const bastionGroupAssetList = ref([]);
    const bastionGroupAssetGroupId = ref(null);
    const bastionAllEcsList = ref([]);
    const bastionGroupAssetRemoveIds = ref([]);
    const bastionGroupAssetNewIds = ref([]);
    // 组凭证授权
    const bastionGroupCredList = ref([]);
    const bastionGroupCredRemoveIds = ref([]);
    const bastionGroupCredNewIds = ref([]);
    // 过滤掉已授权的ECS
    const filteredAvailableEcsList = computed(() => {
      const boundIds = new Set(bastionGroupAssetList.value.map(a => a.ecs_id));
      return bastionAllEcsList.value.filter(e => !boundIds.has(e.id));
    });
    // 过滤掉已授权的凭证
    const filteredAvailableCreds = computed(() => {
      const boundIds = new Set(bastionGroupCredList.value.map(c => c.credential_id));
      return bastionCredShortList.value.filter(c => !boundIds.has(c.id));
    });

    async function fetchBastionGroups() {
      bastionGroupLoading.value = true;
      try { const res = await authFetch('/api/bastion/groups'); const json = await res.json(); if (json.code === 0) bastionGroupList.value = json.data || []; } catch (e) {} finally { bastionGroupLoading.value = false; }
    }
    function openBastionGroupModal(item) {
      if (item) { bastionGroupEditing.value = true; bastionGroupEditId.value = item.id; bastionGroupForm.name = item.name; bastionGroupForm.description = item.description || ''; }
      else { bastionGroupEditing.value = false; bastionGroupEditId.value = null; bastionGroupForm.name = ''; bastionGroupForm.description = ''; }
      bastionGroupModalVisible.value = true;
    }
    async function saveBastionGroup() {
      if (!bastionGroupForm.name) { showToast('名称必填', 'error'); return; }
      try {
        let res;
        if (bastionGroupEditing.value) res = await authFetch('/api/bastion/groups/' + bastionGroupEditId.value, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(bastionGroupForm) });
        else res = await authFetch('/api/bastion/groups', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(bastionGroupForm) });
        const json = await res.json();
        if (json.code === 0) { showToast('保存成功'); bastionGroupModalVisible.value = false; fetchBastionGroups(); }
        else showToast(json.msg || '失败', 'error');
      } catch (e) { showToast('请求失败', 'error'); }
    }
    async function deleteBastionGroup(item) {
      if (!confirm('确定删除 "' + item.name + '" 吗？')) return;
      try {
        const res = await authFetch('/api/bastion/groups/' + item.id, { method: 'DELETE' });
        const json = await res.json().catch(() => ({}));
        if (json.code === 0) { showToast('删除成功'); fetchBastionGroups(); }
        else showToast(json.msg || '删除失败', 'error');
      } catch (e) { showToast('请求失败', 'error'); }
    }
    // 组成员
    async function openBastionGroupMembers(group) {
      bastionGroupMemberGroupId.value = group.id;
      bastionGroupMemberName.value = group.name;
      try { const res = await authFetch('/api/bastion/groups/' + group.id + '/members'); const json = await res.json(); if (json.code === 0) bastionGroupMemberList.value = json.data || []; } catch (e) {}
      if (bastionAllUserList.value.length === 0) {
        try { const res = await authFetch('/api/user/?size=100'); const json = await res.json(); if (json.code === 0) bastionAllUserList.value = json.data.list || []; } catch (e) {}
      }
      bastionGroupMemberModalVisible.value = true;
    }
    async function addBastionGroupMember() {
      if (!bastionGroupAddUserId.value) return;
      await authFetch('/api/bastion/groups/' + bastionGroupMemberGroupId.value + '/members', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ user_id: bastionGroupAddUserId.value }) });
      openBastionGroupMembers({ id: bastionGroupMemberGroupId.value, name: bastionGroupMemberName.value });
    }
    async function removeBastionGroupMember(user) {
      await authFetch('/api/bastion/groups/' + bastionGroupMemberGroupId.value + '/members/' + user.id, { method: 'DELETE' });
      openBastionGroupMembers({ id: bastionGroupMemberGroupId.value, name: bastionGroupMemberName.value });
    }
    // 组资产
    async function openBastionGroupAssets(group) {
      bastionGroupAssetGroupId.value = group.id;
      bastionGroupAssetName.value = group.name;
      bastionGroupAssetTab.value = 'assets';
      bastionGroupAssetRemoveIds.value = [];
      bastionGroupAssetNewIds.value = [];
      bastionGroupCredRemoveIds.value = [];
      bastionGroupCredNewIds.value = [];
      // 加载已授权资产
      try { const res = await authFetch('/api/bastion/groups/' + group.id + '/assets'); const json = await res.json(); if (json.code === 0) bastionGroupAssetList.value = json.data || []; } catch (e) {}
      // 加载已授权凭证
      try { const res = await authFetch('/api/bastion/groups/' + group.id + '/credentials'); const json = await res.json(); if (json.code === 0) bastionGroupCredList.value = json.data || []; } catch (e) {}
      if (bastionCredShortList.value.length === 0) await fetchBastionCredentials();
      if (bastionAllEcsList.value.length === 0) {
        try { const res = await authFetch('/api/ecs?size=200'); const json = await res.json(); if (json.code === 0) bastionAllEcsList.value = json.data.list || []; } catch (e) {}
      }
      bastionGroupAssetModalVisible.value = true;
    }
    function onToggleAllBastionAssets(e) {
      if (e.target.checked) bastionGroupAssetRemoveIds.value = bastionGroupAssetList.value.map(a => a.id);
      else bastionGroupAssetRemoveIds.value = [];
    }
    function onToggleAllBastionEcs(e) {
      if (e.target.checked) bastionGroupAssetNewIds.value = filteredAvailableEcsList.value.map(e => e.id);
      else bastionGroupAssetNewIds.value = [];
    }
    function onToggleAllBastionCreds(e) {
      if (e.target.checked) bastionGroupCredRemoveIds.value = bastionGroupCredList.value.map(c => c.id);
      else bastionGroupCredRemoveIds.value = [];
    }
    function onToggleAllAvailableCreds(e) {
      if (e.target.checked) bastionGroupCredNewIds.value = filteredAvailableCreds.value.map(c => c.id);
      else bastionGroupCredNewIds.value = [];
    }
    // 添加资产授权
    async function addBastionGroupAsset() {
      if (bastionGroupAssetNewIds.value.length === 0) { showToast('请选择ECS', 'error'); return; }
      await authFetch('/api/bastion/groups/' + bastionGroupAssetGroupId.value + '/assets', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ecs_ids: bastionGroupAssetNewIds.value }) });
      openBastionGroupAssets({ id: bastionGroupAssetGroupId.value, name: bastionGroupAssetName.value });
    }
    // 删除单个资产授权
    async function deleteBastionGroupAsset(item) {
      await authFetch('/api/bastion/groups/' + bastionGroupAssetGroupId.value + '/assets/' + item.id, { method: 'DELETE' });
      openBastionGroupAssets({ id: bastionGroupAssetGroupId.value, name: bastionGroupAssetName.value });
    }
    // 批量删除资产授权
    async function deleteBastionGroupAssetsBatch() {
      if (bastionGroupAssetRemoveIds.value.length === 0) return;
      await authFetch('/api/bastion/groups/assets/batch-delete', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids: bastionGroupAssetRemoveIds.value }) });
      openBastionGroupAssets({ id: bastionGroupAssetGroupId.value, name: bastionGroupAssetName.value });
    }
    // 添加凭证授权
    async function addBastionGroupCred() {
      if (bastionGroupCredNewIds.value.length === 0) { showToast('请选择凭证', 'error'); return; }
      await authFetch('/api/bastion/groups/' + bastionGroupAssetGroupId.value + '/credentials', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ credential_ids: bastionGroupCredNewIds.value }) });
      openBastionGroupAssets({ id: bastionGroupAssetGroupId.value, name: bastionGroupAssetName.value });
    }
    // 删除单个凭证授权
    async function deleteBastionGroupCred(item) {
      await authFetch('/api/bastion/groups/' + bastionGroupAssetGroupId.value + '/credentials/' + item.id, { method: 'DELETE' });
      openBastionGroupAssets({ id: bastionGroupAssetGroupId.value, name: bastionGroupAssetName.value });
    }
    // 批量删除凭证授权
    async function deleteBastionGroupCredsBatch() {
      if (bastionGroupCredRemoveIds.value.length === 0) return;
      await authFetch('/api/bastion/groups/credentials/batch-delete', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids: bastionGroupCredRemoveIds.value }) });
      openBastionGroupAssets({ id: bastionGroupAssetGroupId.value, name: bastionGroupAssetName.value });
    }


    // SSH终端连接
    const bastionTerminalAsset = reactive({ id: 0, name: '', ip: '', port: 22, public_ip: '', private_ip: '', eip: '' });
    const bastionTerminalStatus = ref('');
    const bastionTerminalNetwork = ref('public');
    const bastionTerminalCredId = ref(0);
    const bastionTerminalCredList = ref([]);
    // 自定义下拉状态
    const bastionNetDrpOpen = ref(false);
    const bastionCredDrpOpen = ref(false);
    const bastionConnectCredDrpOpen = ref(false);
    // 获取凭证显示名称
    function selCredLabel(creds, cid) {
      if (!creds || !creds.length) return '无凭证';
      const c = creds.find(x => x.id === cid);
      return c ? c.name + '(' + c.username + ')' : '选择凭证';
    }
    // 终端网络选择：可读标签 + 对应 IP（弹性项有值即显示，解决"找不到弹性IP"）
    function bastionTerminalNetworkLabel(net) {
      return net === 'eip' ? '弹性' : net === 'private' ? '内网' : '公网';
    }
    function bastionTerminalNetIp(net) {
      if (net === 'eip') return bastionTerminalAsset.eip || bastionTerminalAsset.public_ip || bastionTerminalAsset.private_ip || '';
      if (net === 'private') return bastionTerminalAsset.private_ip || '';
      return bastionTerminalAsset.public_ip || '';
    }
    // 连接弹窗
    const bastionConnectVisible = ref(false);
    const bastionConnectAsset = reactive({ id: 0, name: '', public_ip: '', private_ip: '', eip: '', port: 22 });
    const bastionConnectNetwork = ref(localStorage.getItem('bastion_network_pref') || 'public');
    const bastionConnectCredId = ref(0);
    const bastionConnectCredList = ref([]);
    const bastionConnectLoading = ref(false);
    let bastionWs = null;
    let bastionXterm = null;
    let bastionHeartbeat = null;

    // 按资产ID读取上次使用的凭证ID
    function getBastionCredPref(assetId) {
      try { return parseInt(localStorage.getItem('bastion_cred_pref_' + assetId)) || 0; } catch (e) { return 0; }
    }
    function setBastionCredPref(assetId, credId) {
      try { localStorage.setItem('bastion_cred_pref_' + assetId, credId); } catch (e) {}
    }
    function pickPreferredCred(creds, assetId) {
      if (!creds || creds.length === 0) return 0;
      const pref = getBastionCredPref(assetId);
      if (pref && creds.some(function(c) { return c.id === pref; })) return pref;
      return creds[0].id;
    }

    // 点击连接按钮：数据优先，数据就绪后再开弹窗（select直接是成品，无闪烁）
    async function connectBastion(asset) {
      bastionConnectAsset.id = asset.id;
      bastionConnectAsset.name = asset.instance_name || asset.name || '';
      bastionConnectAsset.public_ip = asset.public_ip_address || asset.public_ip || '';
      bastionConnectAsset.private_ip = asset.private_ip_address || asset.private_ip || '';
      bastionConnectAsset.eip = asset.eip_address || '';
      bastionConnectAsset.port = asset.ssh_port || 22;
      // 默认网络：优先公网，无公网则弹性，再无则内网
      if (bastionConnectAsset.public_ip) bastionConnectNetwork.value = 'public';
      else if (bastionConnectAsset.eip) bastionConnectNetwork.value = 'eip';
      else if (bastionConnectAsset.private_ip) bastionConnectNetwork.value = 'private';

      // 先准备凭证数据
      let creds = bastionAssetCredCache[asset.id];
      if (!creds) {
        try {
          const res = await authFetch('/api/bastion/assets/' + asset.id + '/credentials');
          const json = await res.json();
          if (json.code === 0 && json.data.length > 0) {
            bastionAssetCredCache[asset.id] = json.data;
            creds = json.data;
          }
        } catch (e) {}
      }
      bastionConnectCredList.value = creds || [];
      bastionConnectCredId.value = creds ? pickPreferredCred(creds, asset.id) : 0;
      // 数据就绪，开弹窗（select已是成品）
      bastionConnectVisible.value = true;
    }

    // 确认连接：进入终端页面（通过URL传参，刷新不丢失）
    function goBastionTerminal() {
      const ip = bastionConnectNetwork.value === 'eip' ? bastionConnectAsset.eip : (bastionConnectNetwork.value === 'public' ? bastionConnectAsset.public_ip : bastionConnectAsset.private_ip);
      const params = new URLSearchParams({
        id: bastionConnectAsset.id, network: bastionConnectNetwork.value,
        credential_id: bastionConnectCredId.value, name: bastionConnectAsset.name,
        ip: ip, public_ip: bastionConnectAsset.public_ip || '', private_ip: bastionConnectAsset.private_ip || '', eip: bastionConnectAsset.eip || '',
        ssh_port: bastionConnectAsset.port,
      });
      window.history.pushState({}, '', '/bastion/terminal?' + params.toString());
      initBastionTerminalFromUrl();
    }

    function doConnectBastion() {
      if (!bastionConnectCredId.value) return;
      localStorage.setItem('bastion_network_pref', bastionConnectNetwork.value);
      setBastionCredPref(bastionConnectAsset.id, bastionConnectCredId.value);
      bastionConnectVisible.value = false;
      const ip = bastionConnectNetwork.value === 'eip' ? bastionConnectAsset.eip : (bastionConnectNetwork.value === 'public' ? bastionConnectAsset.public_ip : bastionConnectAsset.private_ip);
      const params = new URLSearchParams({
        id: bastionConnectAsset.id, network: bastionConnectNetwork.value,
        credential_id: bastionConnectCredId.value, name: bastionConnectAsset.name,
        ip: ip, public_ip: bastionConnectAsset.public_ip || '', private_ip: bastionConnectAsset.private_ip || '', eip: bastionConnectAsset.eip || '',
        ssh_port: bastionConnectAsset.port,
      });
      window.open('/terminal.html?v=' + Date.now() + '&' + params.toString(), '_blank');
    }

    // 从URL参数初始化终端（支持刷新恢复）
    function initBastionTerminalFromUrl() {
      const p = new URLSearchParams(window.location.search);
      const id = parseInt(p.get('id')) || 0;
      if (!id) return;
      bastionTerminalAsset.id = id;
      bastionTerminalAsset.name = p.get('name') || '';
      bastionTerminalAsset.port = 22;
      bastionTerminalAsset.public_ip = p.get('public_ip') || '';
      bastionTerminalAsset.private_ip = p.get('private_ip') || '';
      bastionTerminalAsset.eip = p.get('eip') || '';
      const net = p.get('network') || 'public';
      bastionTerminalNetwork.value = net;
      bastionTerminalAsset.ip = net === 'eip' ? (bastionTerminalAsset.eip || bastionTerminalAsset.public_ip || bastionTerminalAsset.private_ip) : (net === 'private' && bastionTerminalAsset.private_ip ? bastionTerminalAsset.private_ip : bastionTerminalAsset.public_ip);
      bastionTerminalCredId.value = parseInt(p.get('credential_id')) || 0;
      bastionTerminalStatus.value = '就绪';
      // 加载可用凭证列表
      authFetch('/api/bastion/assets/' + id + '/credentials').then(r => r.json()).then(j => {
        if (j.code === 0) bastionTerminalCredList.value = j.data || [];
      });
      switchTab('bastionTerminal');
      nextTick(() => startBastionTerminal());
    }

    function startBastionTerminal() {
      const el = document.getElementById('bastion-terminal');
      if (!el || bastionXterm) return;
      const term = new Terminal({ cursorStyle: 'bar', cursorBlink: true, fontSize: 14, fontFamily: 'Menlo, Monaco, "Courier New", monospace', theme: { background: '#1e1e1e', foreground: '#d4d4d4' }, rows: 40, cols: 120, scrollback: 2000 });
      const fitAddon = new FitAddon.FitAddon();
      term.loadAddon(fitAddon);
      term.open(el);
      setTimeout(() => { try { fitAddon.fit(); } catch (e) {} }, 100);
      term.onResize(({ cols, rows }) => { if (bastionWs && bastionWs.readyState === WebSocket.OPEN) bastionWs.send(JSON.stringify({ type: 'resize', cols, rows })); });
      window.addEventListener('resize', () => { try { fitAddon.fit(); } catch (e) {} });
      bastionXterm = term;
      connectBastionWs();
      term.onData(data => { if (bastionWs && bastionWs.readyState === WebSocket.OPEN) { bastionWs.send(data); trackBastionCdInput(data); } });
    }

    function connectBastionWs() {
      if (bastionWs) { bastionWs.close(); bastionWs = null; }
      bastionCwd = ''; bastionHomeDir = ''; bastionPrevCwd = ''; bastionInputBuf = '';
      bastionTerminalStatus.value = '连接中...';
      // nginx 代理在 80 端口，走同源 ws 连接
      const proto = window.location.protocol === 'https:' ? 'wss' : 'ws';
      const token = localStorage.getItem(TOKEN_KEY) || '';
      const wsUrl = proto + '://' + window.location.host + '/api/bastion/ws/' + bastionTerminalAsset.id + '?network=' + bastionTerminalNetwork.value + '&credential_id=' + bastionTerminalCredId.value + '&token=' + encodeURIComponent(token);
      const ws = new WebSocket(wsUrl);
      ws.binaryType = 'arraybuffer';
      ws.onopen = () => {
        bastionTerminalStatus.value = '已连接';
        clearInterval(bastionHeartbeat);
        bastionHeartbeat = setInterval(() => {
          if (bastionWs && bastionWs.readyState === WebSocket.OPEN) {
            bastionWs.send(JSON.stringify({ type: 'ping' }));
          }
        }, 30000);
      };
      ws.onmessage = (e) => {
        if (typeof e.data === 'string') {
          try { const d = JSON.parse(e.data); if (d.type === 'connected') { bastionFileSessionID = d.session_id || ''; initBastionCwd(); } else if (d.type === 'error') { bastionTerminalStatus.value = '错误: ' + d.msg; bastionXterm && bastionXterm.write('\r\n\x1b[31m' + d.msg + '\x1b[0m\r\n'); } } catch (_) {}
        } else { bastionXterm && bastionXterm.write(new Uint8Array(e.data)); }
      };
      ws.onclose = () => {
        clearInterval(bastionHeartbeat);
        bastionTerminalStatus.value = '⚠ 已断开';
        if (bastionXterm) bastionXterm.write('\r\n\x1b[31m[连接已断开]\x1b[0m\r\n');
      };
      ws.onerror = () => { bastionTerminalStatus.value = '连接错误'; };
      bastionWs = ws;
    }

    function onBastionNetworkChange() {
      if (bastionTerminalNetwork.value === 'eip') {
        bastionTerminalAsset.ip = bastionTerminalAsset.eip || bastionTerminalAsset.public_ip || bastionTerminalAsset.private_ip;
      } else if (bastionTerminalNetwork.value === 'private') {
        bastionTerminalAsset.ip = bastionTerminalAsset.private_ip || bastionTerminalAsset.public_ip;
      } else {
        bastionTerminalAsset.ip = bastionTerminalAsset.public_ip || bastionTerminalAsset.private_ip;
      }
      reconnectBastion();
    }
    function reconnectBastion() { bastionXterm && bastionXterm.clear(); connectBastionWs(); }
    function disconnectBastion() {
      clearInterval(bastionHeartbeat);
      if (bastionWs) { bastionWs.close(); bastionWs = null; }
      if (bastionXterm) { bastionXterm.dispose(); bastionXterm = null; }
      // 资产列表已是独立页，断开后整页返回
      window.location.href = '/assets/pages/bastion.html';
    }

    function bastionToggleConnection() {
      if (bastionTerminalStatus.value.includes('断开')) {
        // 重连：清理旧实例后重新启动终端
        if (bastionXterm) { try { bastionXterm.dispose(); } catch(e) {} bastionXterm = null; }
        if (bastionWs) { bastionWs.close(); bastionWs = null; }
        bastionTerminalStatus.value = '重连中...';
        nextTick(() => startBastionTerminal());
      } else {
        disconnectBastion();
      }
    }

    // ============ 文件浏览器 ============
    const bastionFileVisible = ref(false);
    const bastionFilePath = ref('/');
    const bastionFileWidth = ref(300);
    const bastionFileList = ref([]);
    const bastionFileLoading = ref(false);
    const bastionFileSelected = ref('');
    const bastionFileSelectedIsDir = ref(false);
    const bastionFileInput = ref('/');
    const bastionFileDragover = ref(false);
    const bastionFileUploading = ref(false);
    const bastionFileUploadProgress = ref(0);
    const bastionFileUploadName = ref('');
    const bastionFileUploadQueue = ref(0);
    let bastionFileSessionID = '';
    // 终端当前目录跟踪：前端解析 cd，让文件面板跟随 cmd 的目录
    let bastionCwd = '', bastionHomeDir = '', bastionPrevCwd = '', bastionInputBuf = '';

    async function fetchBastionFiles(dir) {
      if (!bastionWs || bastionWs.readyState !== WebSocket.OPEN) { showToast('SSH 未连接', 'error'); return false; }
      bastionFileLoading.value = true;
      bastionFileSelected.value = '';
      const queryPath = dir !== undefined ? dir : bastionFilePath.value;
      try {
        const res = await authFetch('/api/bastion/ls/' + bastionTerminalAsset.id + '?session_id=' + bastionFileSessionID + '&path=' + encodeURIComponent(queryPath));
        const json = await res.json();
        if (json.code === 0) {
          const data = json.data || {};
          bastionFileList.value = data.files || [];
          const realPath = data.path || (dir !== undefined ? dir : bastionFilePath.value);
          bastionFilePath.value = realPath;
          bastionFileInput.value = realPath;
          return true;
        }
        bastionFileList.value = [];
        showToast(json.msg || '列目录失败', 'error');
        return false;
      } catch (e) { bastionFileList.value = []; showToast('请求失败', 'error'); return false; }
      finally { bastionFileLoading.value = false; }
    }

    // ===== 文件面板跟随终端当前目录（前端解析 cd） =====
    function initBastionCwd() {
      if (!bastionFileSessionID) return;
      authFetch('/api/bastion/ls/' + bastionTerminalAsset.id + '?session_id=' + encodeURIComponent(bastionFileSessionID) + '&path=')
        .then(r => r.json())
        .then(j => {
          if (j.code !== 0 || !j.data || !j.data.path) return;
          bastionHomeDir = j.data.path;
          if (bastionCwd) return; // 期间用户已 cd，别覆盖
          bastionCwd = j.data.path;
          if (bastionFileVisible.value) fetchBastionFiles(bastionCwd);
        }).catch(() => {});
    }
    function probeBastionDir(path) {
      if (!bastionFileSessionID || !path) return Promise.resolve(true);
      return authFetch('/api/bastion/ls/' + bastionTerminalAsset.id + '?session_id=' + encodeURIComponent(bastionFileSessionID) + '&path=' + encodeURIComponent(path))
        .then(r => r.json()).then(j => j.code === 0).catch(() => true);
    }
    function normalizeBastionCdPath(p) {
      const parts = String(p || '/').split('/'), out = [];
      for (const s of parts) {
        if (!s || s === '.') continue;
        if (s === '..') { if (out.length) out.pop(); continue; }
        out.push(s);
      }
      return '/' + out.join('/');
    }
    function resolveBastionCdPath(cwd, arg, home, prev) {
      if (!arg || arg === '~') return home || cwd || '/';
      if (arg === '-') return prev || cwd || '/';
      if (arg.charAt(0) === '~') return normalizeBastionCdPath((home || '/') + '/' + arg.slice(1));
      if (arg.charAt(0) === '/') return normalizeBastionCdPath(arg);
      return normalizeBastionCdPath((cwd || home || '/') + '/' + arg);
    }
    function trackBastionCdInput(data) {
      if (typeof data !== 'string') return;
      for (let i = 0; i < data.length; i++) {
        const ch = data.charAt(i), code = data.charCodeAt(i);
        if (ch === '\u001b') {
          if (data.charAt(i + 1) === '[') { i += 2; while (i < data.length && !/[A-Za-z~]/.test(data.charAt(i))) i++; }
          else if (data.charAt(i + 1) === 'O') { i += 2; }
          continue;
        }
        if (ch === '\r' || ch === '\n') { const line = bastionInputBuf.trim(); bastionInputBuf = ''; if (line) handleBastionCdCommand(line); continue; }
        if (ch === '\u0003' || ch === '\u000c') { bastionInputBuf = ''; continue; }
        if (ch === '\u007f' || ch === '\b') { bastionInputBuf = bastionInputBuf.slice(0, -1); continue; }
        if (code >= 32 && code !== 127) bastionInputBuf += ch;
      }
    }
    function handleBastionCdCommand(line) {
      const m = /^\s*cd(?:\s+(.*))?\s*$/.exec(line);
      if (!m) return;
      const arg = (m[1] || '').split(/\s*(?:&&|\|\||;|\|)\s*/)[0].trim().replace(/^["'](.*)["']$/, '$1');
      const old = bastionCwd || '';
      const target = resolveBastionCdPath(old, arg, bastionHomeDir || '', bastionPrevCwd || '');
      if (!target || target === old) return;
      bastionPrevCwd = old; bastionCwd = target;
      // 等 shell 执行完再校验，cd 失败时回滚
      setTimeout(() => { syncBastionFilesToCwd(target, old); }, 180);
    }
    async function syncBastionFilesToCwd(target, old) {
      if (bastionCwd !== target) return; // 期间又 cd 了
      const ok = bastionFileVisible.value ? await fetchBastionFiles(target) : await probeBastionDir(target);
      if (!ok) {
        bastionCwd = old; bastionPrevCwd = old;
        if (bastionFileVisible.value) fetchBastionFiles(old || '');
      }
    }

    function toggleBastionFiles() {
      if (!bastionWs || bastionWs.readyState !== WebSocket.OPEN) { showToast('请先连接终端', 'error'); return; }
      if (!bastionFileSessionID) { showToast('正在获取会话...', 'warning'); return; }
      bastionFileVisible.value = !bastionFileVisible.value;
      if (bastionFileVisible.value) {
        bastionFileSelected.value = '';
        fetchBastionFiles(bastionCwd || ''); // 打开面板时直接落在终端当前目录
      }
    }

    function selectBastionFile(f) {
      if (f.is_dir) { bastionFileEnter(f.name); return; }
      bastionFileSelected.value = f.name;
    }

    function bastionFileEnter(dir) {
      const newPath = bastionFilePath.value === '/' ? '/' + dir : bastionFilePath.value + '/' + dir;
      fetchBastionFiles(newPath);
    }

    function bastionFileUp() {
      if (bastionFilePath.value === '/') return;
      const parts = bastionFilePath.value.split('/').filter(Boolean);
      parts.pop();
      fetchBastionFiles('/' + parts.join('/'));
    }

    function gotoBastionFilePath() {
      const path = bastionFileInput.value.trim() || '/';
      bastionFileInput.value = path.startsWith('/') ? path : '/' + path;
      fetchBastionFiles(bastionFileInput.value);
    }

    function downloadSelectedBastionFile() {
      const fullPath = bastionFilePath.value === '/' ? '/' + bastionFileSelected.value : bastionFilePath.value + '/' + bastionFileSelected.value;
      const token = localStorage.getItem(TOKEN_KEY) || '';
      const url = '/api/bastion/download/' + bastionTerminalAsset.id + '?session_id=' + bastionFileSessionID + '&path=' + encodeURIComponent(fullPath) + '&token=' + encodeURIComponent(token);
      fetch(url, { headers: { 'Authorization': token } }).then(r => {
        if (!r.ok) { showToast('下载失败', 'error'); return; }
        return r.blob();
      }).then(blob => {
        if (!blob) return;
        const a = document.createElement('a');
        a.href = URL.createObjectURL(blob);
        a.download = bastionFileSelected.value;
        a.click();
        URL.revokeObjectURL(a.href);
      }).catch(() => showToast('下载失败', 'error'));
    }

    function uploadBastionFile(e) {
      const file = e.target.files[0];
      if (!file) return;
      bastionUploadFileXhr(file);
      e.target.value = '';
    }

    function bastionUploadFileXhr(file) {
      const token = localStorage.getItem(TOKEN_KEY) || '';
      const url = '/api/bastion/upload/' + bastionTerminalAsset.id
        + '?session_id=' + bastionFileSessionID
        + '&path=' + encodeURIComponent(bastionFilePath.value);

      bastionFileUploading.value = true;
      bastionFileUploadName.value = file.name;
      bastionFileUploadProgress.value = 0;

      const xhr = new XMLHttpRequest();
      xhr.open('POST', url);
      xhr.setRequestHeader('Authorization', token);

      xhr.upload.onprogress = function(ev) {
        if (ev.lengthComputable) {
          bastionFileUploadProgress.value = Math.round((ev.loaded / ev.total) * 100);
        }
      };

      xhr.onload = function() {
        bastionFileUploading.value = false;
        bastionFileUploadQueue.value = Math.max(0, bastionFileUploadQueue.value - 1);
        try {
          const j = JSON.parse(xhr.responseText);
          if (j.code === 0) {
            showToast('上传成功');
            fetchBastionFiles();
          } else {
            showToast(j.msg || '上传失败', 'error');
          }
        } catch (e) {
          showToast('上传失败', 'error');
        }
      };

      xhr.onerror = function() {
        bastionFileUploading.value = false;
        bastionFileUploadQueue.value = Math.max(0, bastionFileUploadQueue.value - 1);
        showToast('上传失败', 'error');
      };

      const formData = new FormData();
      formData.append('file', file);
      xhr.send(formData);
    }

    function formatFileSize(bytes) {
      if (!bytes || bytes < 0) return '-';
      if (bytes < 1024) return bytes + ' B';
      if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
      return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
    }

    // 文件抽屉拖拽调整宽度（向左拖=抽屉变小，向右拖=抽屉变大）
    // 文件抽屉拖拽调整宽度（直接操作 DOM + requestAnimationFrame，绕过 Vue 响应式延迟）
    function onFileResizeStart(e) {
      e.preventDefault();
      const drawer = document.querySelector('.bastion-file-drawer');
      if (!drawer) return;
      const startX = e.clientX;
      const startW = drawer.offsetWidth;
      let raf = null;
      function onMove(ev) {
        if (raf) return;
        raf = requestAnimationFrame(() => {
          raf = null;
          const w = Math.max(200, Math.min(600, startW - (ev.clientX - startX)));
          drawer.style.width = w + 'px';
          drawer.style.transition = 'none';
        });
      }
      function onUp() {
        if (raf) { cancelAnimationFrame(raf); raf = null; }
        document.removeEventListener('mousemove', onMove);
        document.removeEventListener('mouseup', onUp);
        drawer.style.transition = '';
        bastionFileWidth.value = drawer.offsetWidth;
      }
      document.addEventListener('mousemove', onMove);
      document.addEventListener('mouseup', onUp);
    }

    // 拖拽文件行到桌面下载
    // 拖拽文件行到桌面下载
    let bastionFileDragState = { x: 0, y: 0 };
    function onFileMouseDown(e) {
      bastionFileDragState.x = e.clientX;
      bastionFileDragState.y = e.clientY;
    }
    function onFileDragStart(e, f) {
      if (f.is_dir) { e.preventDefault(); return; }
      // 防误触：移动 < 6px 视为点击
      if (Math.abs(e.clientX - bastionFileDragState.x) < 6 && Math.abs(e.clientY - bastionFileDragState.y) < 6) {
        e.preventDefault(); return;
      }
      // 浏览器原生拖拽（默认幽灵图），fetch 异步发起下载
      e.dataTransfer.setData('text/plain', f.name);
      e.dataTransfer.effectAllowed = 'copy';
      const fullPath = bastionFilePath.value === '/' ? '/' + f.name : bastionFilePath.value + '/' + f.name;
      const token = localStorage.getItem(TOKEN_KEY) || '';
      const url = '/api/bastion/download/' + bastionTerminalAsset.id + '?session_id=' + bastionFileSessionID + '&path=' + encodeURIComponent(fullPath) + '&token=' + encodeURIComponent(token);
      showToast('📥 ' + f.name + ' 下载中…', 'success');
      fetch(url, { headers: { 'Authorization': token } }).then(r => r.blob()).then(blob => {
        const a = document.createElement('a');
        a.href = URL.createObjectURL(blob);
        a.download = f.name;
        a.click();
        URL.revokeObjectURL(a.href);
        showToast('✅ ' + f.name + ' 下载完成');
      }).catch(() => showToast('下载失败', 'error'));
    }

    // 拖拽文件到抽屉上传
    function onFileDrop(e) {
      bastionFileDragover.value = false;
      const files = e.dataTransfer.files;
      if (!files.length) return;
      bastionFileUploadQueue.value = files.length;
      for (let i = 0; i < files.length; i++) {
        bastionUploadFileXhr(files[i]);
      }
    }

    return {
      // 顶栏账号下拉
      navDirOpen, navDirKeyword, hasNavDir, toggleNavDir, navDirLabel,
      // 顶栏页面标题（此前漏导出，导致标题一直是空的）
      currentPageIcon, currentPageTitle,
      // 顶栏全局搜索
      globalSearchInput, globalSearchKeyword, globalSearchOpen, globalSearchActive,
      globalSearchResults, openGlobalSearch, closeGlobalSearch, clearGlobalSearch,
      goGlobalSearch, onGlobalSearchKey,
      // 阿里云式导航抽屉 / 工作台
      drawerOpen, drawerCategory, drawerCats, drawerTitle, recentVisits, favorites,
      workbenchGroups, filteredWorkbenchGroups, goTab,
      // 登录
      isLoggedIn, username, userRole, isAdmin, bastionEnabled, loginForm, loginLoading, loginError,
      profileMenuOpen, profileVisible, profileSaving, profile, passwordVisible, passwordSaving, passwordForm,
      avatarText, openProfile, saveProfile, openChangePassword, savePassword,
      handleLogin, logout, captchaImg, refreshCaptcha,
      usernameInput, passwordInput, captchaInput,
      // 首页
      homeStats,
      // 域名
      currentTab, loading, syncing,
      page, size, total, totalPages,
      domains, filters, stats,
      detailVisible, detail, toast,
      domainRemarkSaving, saveDomainRemark,
      showColPicker,
      allColumns, visibleColumns,
      colVisible, saveColPrefs,
      sortKey, sortDir, toggleSort, getSortIcon,
      colFilterOpen, colFilters,
      domainPlatformList, domainPlatformLabel,
      domainAccountOptions, domainAccountsOfPlatform, domainSelectFilter,
      toggleColFilter, toggleColFilterOption, isColFilterSelected, toggleColFilterAll, isColFilterAllSelected, applyColFilter,
      search, resetFilters, goPage, jumpPage, pageInput, showDetail, syncDomains, onSizeChange,
      syncDnsDomains,
      formatDate, formatDateTime, getDaysClass,
      // ECS
      navigate, switchTab,
      ecsLoading, ecsList, ecsPage, ecsSize, ecsTotal,
      ecsSortDir, ecsSortIcon, ecsSortedList, toggleEcsNameSort,
      ecsFilters, ecsColFilterOpen, ecsColFilters,
      ecsAccountList, ecsRegionList, ecsPlatformList, ecsAccountOptions, ecsPlatformLabel,
      ecsStats, ecsTotalPages,
      toggleEcsColFilter, ecsToggleColFilterOption, ecsIsColFilterSelected, ecsToggleColFilterAll, ecsIsColFilterAllSelected, ecsApplyColFilter,
      ecsStatusClass, ecsStatusLabel, ecsDotClass, ecsSelectFilter, ecsAccountsOfPlatform,
      fetchEcsInstances, fetchEcsStats, fetchEcsFilterOptions,
      ecsSearch, ecsResetFilters, ecsGoPage, ecsJumpPage, ecsPageInput, ecsOnSizeChange,
      ecsSyncing, syncEcsInstances,
      // 同步日志面板
      syncPanelVisible, syncPanelFullscreen, syncLogs,
      closeSyncPanel, clearSyncLogs, showSyncLog,
      // 域名同步独立状态
      domainSyncRunning, domainSyncError, domainSyncLabel,
      // ECS同步独立状态
      ecsSyncRunning, ecsSyncError, ecsSyncLabel,
      // DNS可管理域名列表
      dnsDomainLoading, dnsDomainList, dnsDomainPage, dnsDomainSize, dnsDomainTotal, dnsDomainPageInput,
      dnsDomainFilters, dnsDomainPlatformList, dnsDomainAccountOptions, dnsDomainTotalPages,
      dnsDomainPlatformLabel, dnsDomainSearch, dnsDomainResetFilters,
      dnsDomainSelectFilter, dnsDomainAccountsOfPlatform,
      dnsDomainGoPage, dnsDomainJumpPage, dnsDomainOnSizeChange,
      // 域名（注册列表）手动管理
      domainManualModalVisible, domainManualEditing, domainManualSaving, domainManualForm,
      openDomainManualModal, saveDomainManual, deleteDomainManual,
      // CDN域名（阿里云CDN / 阿里云DCDN / AWS CloudFront）
      cdnDomainLoading, cdnDomainList, cdnDomainPage, cdnDomainSize, cdnDomainTotal, cdnDomainPageInput,
      cdnDomainFilters, cdnDomainTotalPages, cdnDomainSyncing,
      cdnDomainPlatformList, cdnDomainAccountList,
      cdnDomainPlatformLabel, cdnStatusClass, cdnProductLabel, cdnProductClass, cdnSslDaysLeft,
      cdnDomainSortDir, cdnDomainSortIcon, cdnDomainSortedList, cdnDomainToggleSslSort,
      fetchCdnDomains, fetchCdnDomainFilterOptions, cdnSelectFilter, cdnAccountsOfPlatform,
      cdnDomainSearch, cdnDomainResetFilters,
      cdnDomainGoPage, cdnDomainJumpPage, cdnDomainOnSizeChange, syncCdnDomains,
      // 资源页按账号同步
      resourceSyncVisible, resourceSyncType, resourceSyncTypeName, resourceSyncList, resourceSyncSelected, resourceSyncAll,
      openResourceSync, submitResourceSync,
      // DNS解析记录（全屏页面）
      dnsModalLoading, dnsModalError, dnsModalList, dnsCurrentDomain, dnsCurrentPlatform, dnsCurrentAccount, dnsCurrentAccountId,
      dnsApi, dnsPlatformTitle, openDnsResolve,
      dnsRecordKeyword, dnsRecordTypeFilter, filteredDnsModalList, dnsRecordSearch,
      dnsFormVisible, dnsFormEditing, dnsFormSaving, dnsCollapseOpen, dnsForm,
      dnsValueValid, getDnsValueHint, getDnsValuePlaceholder, getDnsValueError, validateDnsValue, dnsValidateRoundRobinValue,
      roundRobinValues, addRoundRobinValue, removeRoundRobinValue,
      showDnsRecords, retryDnsRecords, openDnsAddForm, openDnsEditForm, saveDnsRecord, toggleDnsRecordStatus, deleteDnsRecord, dnsRecordLineLabel, dnsRecordStrategyLabel, dnsRecordWeightNum,
      // 用户管理
      userLoading, userSaving, userList, userPage, userSize, userTotal, userPageInput, userTotalPages,
      userModalVisible, userEditing, userRoleDrpOpen, userForm, userFilters,
      userRoleClass, userRoleLabel,
      openUserModal, saveUser, deleteUser, unlockUser,
      userSearch, userResetFilters, userOnSizeChange, userGoPage, userJumpPage,
      // 云平台账号管理
      accountLoading, accountSaving, accountList2, accountPage, accountSize, accountTotal, accountPageInput, accountTotalPages,
      accountModalVisible, accountEditing, accountForm, accountFilters,
      accountPlatformLabel, accountSiteLabel,
      openAccountModal, saveAccount, deleteAccount,
      accountSearch, accountResetFilters, accountOnSizeChange, accountGoPage, accountJumpPage,
      // 地域管理（实例地域开通）
      sidebarDomainOpen, sidebarServerOpen, sidebarCollapsed, regionLoading, regionPlatform, regionList,
      fetchRegions, toggleRegion,
      // 资源包（阿里云，实时查询计费服务）
      sidebarResourceOpen, rpLoading, rpList, rpAccounts, rpErrors, rpFilters, filteredResourcePackages, productLabel, rpSelectAccount, rpStatusLabel, rpTime, fetchResourcePackages,
      onAccountPlatformChange,
      // ECS手动管理
      ecsManualModalVisible, ecsManualEditing, ecsManualSaving, ecsManualForm, ecsManualEditId,
      openEcsManualModal, saveEcsManual, deleteEcsManual,
      ecsInlineEdit, ecsInlineValue, startEcsInlineEdit, saveEcsInlineEdit, cancelEcsInlineEdit,
      // 堡垒机
      sidebarBastionOpen,
      bastionAssetLoading, bastionAssetList, bastionAssetKeyword, bastionAssetPlatform, bastionAssetAccount, bastionAssetStatus, bastionAssetRegion, bastionAssetAccounts, bastionAssetRegions,
      bastionAssetPlatformList, bastionAssetAccountOptions, bastionAssetSelectFilter, bastionAssetAccountsOfPlatform,
      filteredBastionAssetList,
      bastionAssetSortDir, bastionAssetSortIcon, bastionAssetSortedList, toggleBastionAssetNameSort,
      copyText,
      bastionCredLoading, bastionCredSaving, bastionCredList, bastionCredShortList, bastionCredModalVisible, bastionCredEditing, bastionCredForm, bastionCredEditId,
      bastionGroupLoading, bastionGroupSaving, bastionGroupList, bastionGroupModalVisible, bastionGroupEditing, bastionGroupForm,
      bastionGroupMemberModalVisible, bastionGroupMemberName, bastionGroupMemberList, bastionGroupAddUserId, bastionAllUserList,
      bastionGroupAssetModalVisible, bastionGroupAssetTab, bastionGroupAssetName, bastionGroupAssetList, bastionAllEcsList,
      bastionGroupAssetRemoveIds, bastionGroupAssetNewIds, filteredAvailableEcsList,
      bastionGroupCredList, bastionGroupCredRemoveIds, bastionGroupCredNewIds, filteredAvailableCreds,
      fetchBastionAssets,
      fetchBastionCredentials, openBastionCredModal, saveBastionCred, deleteBastionCred,
      fetchBastionGroups, openBastionGroupModal, saveBastionGroup, deleteBastionGroup,
      openBastionGroupMembers, addBastionGroupMember, removeBastionGroupMember,
      openBastionGroupAssets, addBastionGroupAsset, deleteBastionGroupAsset, deleteBastionGroupAssetsBatch,
      addBastionGroupCred, deleteBastionGroupCred, deleteBastionGroupCredsBatch,
      onToggleAllBastionAssets, onToggleAllBastionEcs, onToggleAllBastionCreds, onToggleAllAvailableCreds,
      // 堡垒机连接/终端
      connectBastion, doConnectBastion, initBastionTerminalFromUrl,
      bastionConnectVisible, bastionConnectAsset, bastionConnectNetwork, bastionConnectCredId, bastionConnectCredList, bastionConnectLoading,
      bastionTerminalAsset, bastionTerminalStatus, bastionTerminalNetwork, bastionTerminalNetIp, bastionTerminalNetworkLabel, bastionTerminalCredId, bastionTerminalCredList,
      bastionNetDrpOpen, bastionCredDrpOpen, bastionConnectCredDrpOpen, selCredLabel,
      reconnectBastion, disconnectBastion, bastionToggleConnection, onBastionNetworkChange,
      // 文件浏览器
      bastionFileVisible, bastionFilePath, bastionFileList, bastionFileLoading, bastionFileSelected, bastionFileSelectedIsDir, bastionFileWidth,
      bastionFileUploading, bastionFileUploadProgress, bastionFileUploadName, bastionFileUploadQueue,
      toggleBastionFiles, selectBastionFile, bastionFileEnter, bastionFileUp, gotoBastionFilePath, downloadSelectedBastionFile, uploadBastionFile, formatFileSize, onFileResizeStart, onFileDrop, onFileDragStart, onFileMouseDown, bastionFileDragover,
      bastionFileInput,
    };
  },
});

app.mount('#app');

/* 单元格被省略号截断时，鼠标悬停用原生 title 显示完整内容（窗口变化后不截断则自动移除） */
(function () {
  function autoTitle(el) {
    if (el.scrollWidth > el.clientWidth + 1) {
      if (!el.title) {
        var text = (el.innerText || el.textContent || '').replace(/\s+\n/g, '\n').trim();
        if (text) { el.title = text; el.dataset.autoTitle = '1'; }
      }
    } else if (el.dataset.autoTitle === '1') {
      el.removeAttribute('title');
      delete el.dataset.autoTitle;
    }
  }
  document.addEventListener('mouseover', function (e) {
    var el = e.target;
    if (!el || !el.closest) return;
    var cell = el.closest('td, th, .domain-name, .domain-remark');
    if (cell) autoTitle(cell);
  }, true);
})();
