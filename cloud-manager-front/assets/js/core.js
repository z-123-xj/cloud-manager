/* ============================================================================
 * 云管平台 · 独立页面公共内核
 *
 * 只放稳定的公共能力：会话 / 请求 / toast / 顶栏 / 导航抽屉 / 全局搜索 /
 * 统一路径表 / 通用工具。不含任何业务状态与业务请求。
 *
 * 每个页面 = 一个独立 HTML + 一个独立业务 JS。业务 JS 出错只会影响那一页，
 * 不会像旧 index.html + app.js 那样让全站白屏。
 *
 * 用法：
 *   CloudCore.mountPage({ key, title, hasNavDir, template, setup })
 *   setup(api) 返回本页要暴露给模板的状态与方法；api 提供
 *   authFetch / showToast / formatDate / ROUTES / Vue 响应式 API 等。
 * ==========================================================================*/
(function (global) {
  'use strict';

  const VueApi = {
    createApp: Vue.createApp, ref: Vue.ref, reactive: Vue.reactive,
    computed: Vue.computed, watch: Vue.watch, watchEffect: Vue.watchEffect,
    onMounted: Vue.onMounted, onUnmounted: Vue.onUnmounted, nextTick: Vue.nextTick,
  };
  const { createApp, ref, reactive, computed, onMounted, onUnmounted } = VueApi;

  const VERSION = '20261108i';

  // 统一路径表：新增页面 / 把旧页迁成独立页，只改这一处。
  // 尚未迁移的页面仍是主站 SPA 伪路由（后端 NoRoute 回退 index.html）。
  const ROUTES = Object.freeze({
    home: '/',
    domains: '/assets/pages/domains.html',
    dnsDomains: '/assets/pages/dns.html',
    dns: '/assets/pages/dns.html',
    cdnDomains: '/assets/pages/cdn.html',
    ecs: '/assets/pages/ecs.html',
    regions: '/assets/pages/regions.html',
    resourcePackages: '/assets/pages/resource-packages.html',
    bastionAssets: '/assets/pages/ecs.html', // 堡垒机资产页已合并进 ECS 实例页
    batchExec: '/assets/pages/batch-exec.html',
    bastionGroups: '/bastion/groups',
    bastionCredentials: '/bastion/credentials',
    bastionTerminal: '/bastion/terminal',
    users: '/assets/pages/users.html',
    accounts: '/assets/pages/accounts.html',
    logs: '/assets/pages/logs.html?v=' + VERSION,
    terminal: '/terminal.html?v=' + VERSION,
    profile: '/?profile=1',
    password: '/?password=1',
    drawer: '/?drawer=1',
  });

  // 导航抽屉 / 工作台 / 全局搜索共用的唯一入口源
  const NAV_GROUPS = [
    { key: 'domains', title: '域名与网站', icon: 'fas fa-globe', items: [
      { label: '域名管理', path: ROUTES.domains, count: '域名资产' },
      { label: 'DNS解析', path: ROUTES.dnsDomains, count: '解析管理' },
      { label: 'CDN域名', path: ROUTES.cdnDomains, count: '加速域名' },
    ] },
    { key: 'compute', title: '计算', icon: 'fas fa-server', items: [
      { label: '云服务器 ECS', path: ROUTES.ecs, count: '实例管理' },
      { label: '实例地域开通', path: ROUTES.regions, count: '地域管理' },
    ] },
    { key: 'security', title: '安全运维', icon: 'fas fa-shield-alt', items: [
      { label: '凭证管理', path: ROUTES.bastionCredentials, count: 'SSH 凭证' },
      { label: '用户组管理', path: ROUTES.bastionGroups, count: '账号管理 / 授权' },
      { label: '批量执行', path: ROUTES.batchExec, count: '命令/脚本下发' },
      { label: '日志排查', path: ROUTES.logs, count: 'Nginx 日志分析' },
    ] },
    { key: 'resource', title: '资源', icon: 'fas fa-cubes', items: [
      { label: '资源包', path: ROUTES.resourcePackages, count: '资源包管理' },
    ] },
    { key: 'accounts', title: '账号与用户', icon: 'fas fa-users', items: [
      { label: '云平台账号', path: ROUTES.accounts, count: '账号管理' },
      { label: '用户管理', path: ROUTES.users, count: '权限管理' },
    ] },
  ];
  const RECENT_VISITS = [
    { name: '域名管理', path: ROUTES.domains },
    { name: '云服务器 ECS', path: ROUTES.ecs },
    { name: 'DNS解析', path: ROUTES.dnsDomains },
    { name: '日志排查', path: ROUTES.logs },
  ];
  const FAVORITES = [
    { name: 'CDN域名', path: ROUTES.cdnDomains },
    { name: '云平台账号', path: ROUTES.accounts },
    { name: '资源包', path: ROUTES.resourcePackages },
  ];

  // ============ 会话 ============
  const TOKEN_KEY = 'cloud_token';
  const USER_KEY = 'cloud_user';
  const ROLE_KEY = 'cloud_role';
  const TOKEN_EXP_KEY = 'cloud_token_exp';
  const IDLE_TIMEOUT_MS = 12 * 60 * 60 * 1000;
  const ACTIVITY_EVENTS = ['mousemove', 'keydown', 'click', 'scroll', 'touchstart'];
  let _lastActivity = Date.now();
  let _tokenCheckTimer = null;

  function recordActivity() { _lastActivity = Date.now(); }
  function clearAuthStorage() {
    [TOKEN_KEY, USER_KEY, ROLE_KEY, TOKEN_EXP_KEY].forEach(k => localStorage.removeItem(k));
  }
  function checkSessionValid() {
    if (!localStorage.getItem(TOKEN_KEY)) return false;
    const exp = parseInt(localStorage.getItem(TOKEN_EXP_KEY) || '0', 10);
    if (exp && Date.now() > exp) return false;
    return Date.now() - _lastActivity <= IDLE_TIMEOUT_MS;
  }
  function logout() {
    clearAuthStorage();
    global.location.href = ROUTES.home;
  }
  function authFetch(url, options) {
    const opts = options || {};
    if (!opts.headers) opts.headers = {};
    const token = localStorage.getItem(TOKEN_KEY);
    if (token) opts.headers['Authorization'] = token;
    return fetch(url, opts).then(res => {
      if (res.status === 401) {
        clearAuthStorage();
        global.dispatchEvent(new CustomEvent('auth:unauthorized'));
      }
      return res;
    });
  }
  function startSessionGuard() {
    stopSessionGuard();
    recordActivity();
    ACTIVITY_EVENTS.forEach(ev => document.addEventListener(ev, recordActivity, true));
    _tokenCheckTimer = setInterval(() => {
      if (!checkSessionValid() && localStorage.getItem(TOKEN_KEY)) {
        global.dispatchEvent(new CustomEvent('auth:unauthorized'));
      }
    }, 30 * 1000);
  }
  function stopSessionGuard() {
    if (_tokenCheckTimer) { clearInterval(_tokenCheckTimer); _tokenCheckTimer = null; }
    ACTIVITY_EVENTS.forEach(ev => document.removeEventListener(ev, recordActivity, true));
  }

  // ============ toast / 通用工具 ============
  let activeToast = null;
  function showToast(msg, type) {
    if (!activeToast) return;
    activeToast.msg = msg;
    activeToast.type = type || 'success';
    activeToast.show = true;
    setTimeout(() => { if (activeToast) activeToast.show = false; }, 2500);
  }
  function formatDate(dateStr) {
    if (!dateStr) return '-';
    const d = new Date(dateStr);
    if (!isNaN(d.getTime())) {
      return d.getFullYear() + '-' + String(d.getMonth() + 1).padStart(2, '0') + '-' + String(d.getDate()).padStart(2, '0');
    }
    const m = String(dateStr).match(/^(\d{4})[-/](\d{1,2})[-/](\d{1,2})/);
    if (m) return m[1] + '-' + String(m[2]).padStart(2, '0') + '-' + String(m[3]).padStart(2, '0');
    return dateStr;
  }
  function formatDateTime(dateStr) {
    if (!dateStr) return '-';
    const d = new Date(dateStr);
    if (isNaN(d.getTime())) return dateStr;
    try {
      return d.toLocaleString('zh-CN', {
        timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit',
        hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false,
      }).replace(/\//g, '-');
    } catch (e) { return formatDate(dateStr); }
  }
  async function copyText(text) {
    if (text == null || text === '') return;
    try {
      if (navigator.clipboard && navigator.clipboard.writeText) {
        await navigator.clipboard.writeText(String(text));
      } else {
        const ta = document.createElement('textarea');
        ta.value = String(text);
        ta.style.position = 'fixed';
        ta.style.opacity = '0';
        document.body.appendChild(ta);
        ta.select();
        document.execCommand('copy');
        document.body.removeChild(ta);
      }
      showToast('已复制: ' + text, 'success');
    } catch (e) { showToast('复制失败', 'error'); }
  }

  // ============ 公共骨架模板（页面内容插入 <!--PAGE_SLOT-->）============
  const LAYOUT = `
<div class="page-root">
  <div class="layout" v-if="isLoggedIn">
    <header class="topbar">
      <div class="topbar-left">
        <button class="ali-menu-btn" @click="drawerOpen = !drawerOpen" title="展开 / 关闭导航抽屉"><span v-if="drawerOpen">✕</span><span v-else>☰</span></button>
        <span class="ali-brand">云管平台</span>
        <a class="ali-workbench-btn" :href="ROUTES.home">⚙ 工作台</a>
        <span class="page-title"><i class="fas" :class="pageIcon"></i> {{ pageTitle }}</span>
      </div>
      <div class="topbar-center">
        <div class="ali-global-search" :class="{ open: globalSearchOpen }" @click.stop>
          <div class="ali-global-search-box">
            <i class="fas fa-search"></i>
            <input ref="globalSearchInput" class="ali-global-search-input" type="text"
                   v-model="globalSearchKeyword" placeholder="搜索产品、服务或功能"
                   autocomplete="off" spellcheck="false"
                   @focus="openGlobalSearch" @input="openGlobalSearch" @keydown="onGlobalSearchKey" />
            <button v-if="globalSearchKeyword" class="ali-global-search-clear" type="button" title="清空" @click="clearGlobalSearch">✕</button>
            <span v-else class="ali-global-search-kbd">Ctrl K</span>
          </div>
          <div class="ali-global-search-panel" v-if="globalSearchOpen">
            <div class="ali-global-search-panel-head">{{ globalSearchKeyword.trim() ? '搜索结果' : '常用入口' }}</div>
            <a class="ali-global-search-item" v-for="(item, idx) in globalSearchResults" :key="item.path"
               :class="{ active: globalSearchActive === idx }"
               @click="goGlobalSearch(item)" @mouseenter="globalSearchActive = idx">
              <i class="fas fa-external-link-alt"></i>
              <span>{{ item.label }}</span>
              <span class="group">{{ item.group }}</span>
            </a>
            <div class="ali-global-search-empty" v-if="!globalSearchResults.length">没有匹配的产品或服务</div>
          </div>
        </div>
      </div>
      <div class="topbar-right">
        <a class="topbar-link" :href="ROUTES.users" v-if="isAdmin" title="用户管理">用户管理</a>
        <a class="topbar-link" :href="ROUTES.accounts" v-if="isAdmin" title="云平台账号">云平台账号</a>
        <div class="topbar-avatar-wrap" @click.stop="profileMenuOpen = !profileMenuOpen" title="个人中心">
          <div class="topbar-avatar">{{ avatarText }}</div>
          <span class="topbar-avatar-arrow" :class="{ open: profileMenuOpen }">▾</span>
          <div class="topbar-dropdown" v-if="profileMenuOpen" @click.stop>
            <div class="topbar-dropdown-head">
              <div class="topbar-dropdown-name">{{ username }}</div>
              <div class="topbar-dropdown-role">{{ userRoleLabel(userRole) }}</div>
            </div>
            <a class="topbar-dropdown-item" :href="ROUTES.profile">个人资料</a>
            <a class="topbar-dropdown-item" :href="ROUTES.password">修改密码</a>
            <div class="topbar-dropdown-divider"></div>
            <a class="topbar-dropdown-item danger" @click="logout">退出登录</a>
          </div>
        </div>
      </div>
    </header>

    <main class="main-content">
      <!--PAGE_SLOT-->
    </main>

    <button class="ali-edge-triangle" v-if="!drawerOpen" @click="drawerOpen = true" title="展开导航抽屉">▶</button>
    <div class="ali-mask" v-if="drawerOpen" @click="drawerOpen = false"></div>
    <aside class="ali-drawer" :class="{ open: drawerOpen }">
      <div class="ali-drawer-rail">
        <div class="ali-drawer-rail-head">我的资源</div>
        <div class="ali-drawer-cat" :class="{ active: drawerCategory === 'favorites' }" @click="drawerCategory = 'favorites'">★ 我的收藏</div>
        <div class="ali-drawer-rail-title">产品与服务</div>
        <div class="ali-drawer-cat" v-for="cat in drawerCats" :key="cat.key" :class="{ active: drawerCategory === cat.key }" @click="drawerCategory = cat.key">{{ cat.name }}</div>
      </div>
      <div class="ali-drawer-content">
        <div class="ali-drawer-content-head">
          <h2>{{ drawerTitle }}</h2>
          <button class="ali-drawer-close" @click="drawerOpen = false">✕</button>
        </div>
        <div class="ali-drawer-panel">
          <template v-if="drawerCategory === 'favorites'">
            <div class="ali-block">
              <div class="ali-block-title">我的收藏</div>
              <div class="ali-chips">
                <a class="ali-chip" v-for="v in favorites" :key="v.path" @click="goTab(v.path)">{{ v.name }}</a>
              </div>
            </div>
          </template>
          <template v-else>
            <div class="ali-block" v-if="drawerCategory === 'all'">
              <div class="ali-block-title">最近访问</div>
              <div class="ali-chips">
                <a class="ali-chip" v-for="v in recentVisits" :key="v.path" @click="goTab(v.path)">{{ v.name }}</a>
              </div>
            </div>
            <div class="ali-group" v-for="g in filteredWorkbenchGroups" :key="g.key">
              <div class="ali-group-head"><i class="ali-group-icon" :class="g.icon"></i>{{ g.title }}</div>
              <div class="ali-group-grid">
                <a class="ali-res-card" v-for="item in g.items" :key="g.key + item.label" @click="goTab(item.path)">
                  <span class="ali-res-title">{{ item.label }} <span class="ali-arrow">›</span></span>
                  <span class="ali-res-meta">{{ item.count }}</span>
                </a>
              </div>
            </div>
          </template>
        </div>
      </div>
    </aside>
  </div>

  <div v-else class="login-page">
    <header class="login-header"><div class="login-brand">☁️ 云管平台</div></header>
    <div class="login-main">
      <div class="login-card">
        <h2 class="login-card-title">登录状态已失效</h2>
        <p class="text-gray" style="font-size:13px;line-height:1.7">请返回主站重新登录后再打开此页面。</p>
        <a class="btn btn-primary btn-full" :href="ROUTES.home">返回主站登录</a>
      </div>
    </div>
  </div>

  <div class="toast" :class="{ show: toast.show, 'toast-success': toast.type === 'success', 'toast-error': toast.type === 'error' }">{{ toast.msg }}</div>
</div>`;

  // ============ 每页共用的状态与方法 ============
  function createCoreState(opts) {
    const isLoggedIn = ref(checkSessionValid());
    const username = ref(localStorage.getItem(USER_KEY) || '');
    const userRole = ref(localStorage.getItem(ROLE_KEY) || '');
    const isAdmin = computed(() => userRole.value === 'admin');
    const avatarText = computed(() => {
      const n = (username.value || '').trim();
      return n ? n[0].toUpperCase() : 'U';
    });
    const pageTitle = ref(opts.title || '');
    const pageIcon = ref(opts.icon || 'fa-cube');
    const profileMenuOpen = ref(false);
    const toast = reactive({ show: false, msg: '', type: 'success' });
    activeToast = toast;

    function userRoleLabel(role) {
      const map = { admin: '管理员', readonly: '只读', user: '普通用户' };
      return map[role] || role || '未登录';
    }

    const drawerOpen = ref(false);
    const drawerCategory = ref('all');
    const drawerCats = computed(() => [
      { key: 'all', name: '全部' },
      ...NAV_GROUPS.map(g => ({ key: g.key, name: g.title })),
    ]);
    const drawerTitle = computed(() => {
      if (drawerCategory.value === 'favorites') return '我的收藏';
      const g = NAV_GROUPS.find(x => x.key === drawerCategory.value);
      return g ? g.title : '我的资源';
    });
    const filteredWorkbenchGroups = computed(() => {
      if (drawerCategory.value === 'favorites') return [];
      if (drawerCategory.value === 'all') return NAV_GROUPS;
      return NAV_GROUPS.filter(g => g.key === drawerCategory.value);
    });

    const navDirOpen = ref(false);
    const hasNavDir = ref(!!opts.hasNavDir);
    function toggleNavDir() {
      navDirOpen.value = !navDirOpen.value;
      VueApi.nextTick(() => {
        // 账号筛选已改为左侧抽屉，固定贴左，不再按按钮位置写 inline 坐标
        const drawer = document.querySelector('.cdn-drawer.nav-dir.open');
        if (!drawer) return;
        drawer.style.left = '';
        drawer.style.top = '';
      });
    }

    /* 左侧「选择账号」常驻栏改成可收起抽屉：状态挂在 <html> 上，
       这样 CSS 能同时调整侧栏和 .main-content 的左内边距，无需改各页模板。 */
    const NAV_DIR_COLLAPSED_KEY = 'cloudNavDirCollapsed';
    const navDirCollapsed = ref(false);
    function applyNavDirCollapsed() {
      document.documentElement.classList.toggle('nav-dir-collapsed', navDirCollapsed.value);
    }
    function toggleNavDirCollapsed() {
      navDirCollapsed.value = !navDirCollapsed.value;
      applyNavDirCollapsed();
      try { localStorage.setItem(NAV_DIR_COLLAPSED_KEY, navDirCollapsed.value ? '1' : '0'); } catch (e) { /* ignore */ }
    }
    function initNavDirCollapse() {
      try { navDirCollapsed.value = localStorage.getItem(NAV_DIR_COLLAPSED_KEY) === '1'; } catch (e) { /* ignore */ }
      applyNavDirCollapsed();
      VueApi.nextTick(() => {
        const head = document.querySelector('.cdn-drawer.nav-dir .nav-dir-head');
        if (head && !head.querySelector('.nav-dir-collapse-btn')) {
          const btn = document.createElement('button');
          btn.type = 'button';
          btn.className = 'nav-dir-collapse-btn';
          btn.title = '收起账号目录';
          btn.setAttribute('aria-label', '收起账号目录');
          btn.innerHTML = '<i class="fas fa-angle-double-left"></i>';
          btn.addEventListener('click', function (e) { e.stopPropagation(); toggleNavDirCollapsed(); });
          head.appendChild(btn);
        }
        if (!document.querySelector('.nav-dir-expand-btn')) {
          const expand = document.createElement('button');
          expand.type = 'button';
          expand.className = 'nav-dir-expand-btn';
          expand.title = '展开账号目录';
          expand.setAttribute('aria-label', '展开账号目录');
          expand.innerHTML = '<i class="fas fa-angle-double-right"></i><span>选择账号</span>';
          expand.addEventListener('click', function (e) { e.stopPropagation(); toggleNavDirCollapsed(); });
          document.body.appendChild(expand);
        }
      });
    }

    // Ctrl/⌘/中键：新标签打开；普通单击整页跳转（独立页之间不再共用 SPA 实例）
    let navMods = { ctrl: false, meta: false, middle: false };
    function onCaptureClick(e) {
      navMods = { ctrl: !!e.ctrlKey, meta: !!e.metaKey, middle: e.button === 1 };
    }
    global.addEventListener('click', onCaptureClick, true);

    function openPath(path) {
      if (navMods.ctrl || navMods.meta || navMods.middle) {
        global.open(path, '_blank', 'noopener');
      } else {
        global.location.href = path;
      }
    }
    function goTab(path) {
      drawerOpen.value = false;
      openPath(path);
    }

    const globalSearchInput = ref(null);
    const globalSearchKeyword = ref('');
    const globalSearchOpen = ref(false);
    const globalSearchActive = ref(-1);
    const globalSearchIndex = (() => {
      const list = [], seen = new Set();
      NAV_GROUPS.forEach(g => g.items.forEach(item => {
        if (seen.has(item.path)) return;
        seen.add(item.path);
        list.push({ label: item.label, path: item.path, group: g.title, hint: item.count || '' });
      }));
      return list;
    })();
    const globalSearchResults = computed(() => {
      const kw = globalSearchKeyword.value.trim().toLowerCase();
      if (!kw) return globalSearchIndex.slice(0, 8);
      return globalSearchIndex.filter(i =>
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
      globalSearchKeyword.value = '';
      closeGlobalSearch();
      openPath(item.path);
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
      if ((e.ctrlKey || e.metaKey) && String(e.key || '').toLowerCase() === 'k') {
        e.preventDefault();
        if (!globalSearchInput.value) return;
        globalSearchInput.value.focus();
        openGlobalSearch();
      }
    }
    function closeAllPopups() {
      profileMenuOpen.value = false;
      closeGlobalSearch();
      navDirOpen.value = false;
    }
    function onUnauthorized() {
      isLoggedIn.value = false;
      closeAllPopups();
    }

    global.addEventListener('click', closeAllPopups);
    global.addEventListener('keydown', onGlobalSearchShortcut);
    global.addEventListener('auth:unauthorized', onUnauthorized);

    onMounted(() => {
      if (isLoggedIn.value) startSessionGuard();
      initNavDirCollapse();
    });
    onUnmounted(() => {
      stopSessionGuard();
      global.removeEventListener('click', onCaptureClick, true);
      global.removeEventListener('click', closeAllPopups);
      global.removeEventListener('keydown', onGlobalSearchShortcut);
      global.removeEventListener('auth:unauthorized', onUnauthorized);
      document.documentElement.classList.remove('nav-dir-collapsed');
      const expandBtn = document.querySelector('.nav-dir-expand-btn');
      if (expandBtn) expandBtn.remove();
      activeToast = null;
    });

    return {
      ROUTES, VERSION, isLoggedIn, username, userRole, isAdmin, avatarText,
      userRoleLabel, pageTitle, pageIcon, profileMenuOpen, toast, showToast, logout,
      drawerOpen, drawerCategory, drawerCats, drawerTitle,
      recentVisits: RECENT_VISITS, favorites: FAVORITES,
      filteredWorkbenchGroups, goTab,
      navDirOpen, hasNavDir, toggleNavDir,
      navDirCollapsed, toggleNavDirCollapsed,
      globalSearchInput, globalSearchKeyword, globalSearchOpen, globalSearchActive,
      globalSearchResults, openGlobalSearch, closeGlobalSearch, clearGlobalSearch,
      goGlobalSearch, onGlobalSearchKey,
      authFetch, formatDate, formatDateTime, copyText,
    };
  }

  function mountPage(opts) {
    const config = opts || {};
    const pageTemplate = config.template || '';
    const api = {
      Vue: VueApi, ROUTES, VERSION,
      authFetch, showToast, logout, checkSessionValid,
      formatDate, formatDateTime, copyText,
    };
    const app = createApp({
      setup() {
        const core = createCoreState(config);
        const page = config.setup ? config.setup(api) : {};
        return Object.assign({}, core, page || {});
      },
      template: LAYOUT.replace('<!--PAGE_SLOT-->', () => pageTemplate),
    });
    app.mount('#app');
    return app;
  }

  global.CloudCore = {
    Vue: VueApi, ROUTES, VERSION, mountPage,
    authFetch, showToast, logout, checkSessionValid,
    formatDate, formatDateTime, copyText,
  };
})(window);

/* 单元格被省略号截断时，鼠标悬停用原生 title 显示完整内容（不截断则自动移除） */
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
