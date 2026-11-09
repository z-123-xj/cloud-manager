/* 资源包 · 独立页面业务脚本（只依赖 CloudCore） */
(function () {
  'use strict';

  const { ref, computed, watch, onMounted, nextTick } = CloudCore.Vue;
  const { authFetch, showToast, logout } = CloudCore;

  CloudCore.mountPage({
    key: 'resourcePackages',
    title: '资源包',
    icon: 'fa-cubes',
    hasNavDir: true,
    template: document.getElementById('page-template').innerHTML,
    setup() {
      const rpLoading = ref(false);
      const rpList = ref([]);
      const rpAccounts = ref([]);
      const rpErrors = ref([]);
      const rpFilters = ref({ account: '', keyword: '', onlyRemaining: true });
      const navDirKeyword = ref('');

      const filteredResourcePackages = computed(() => {
        const kw = (rpFilters.value.keyword || '').trim().toLowerCase();
        return rpList.value.filter(it => {
          if (rpFilters.value.account && it.account_id !== rpFilters.value.account) return false;
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

      function rpSelectAccount(accountId) { rpFilters.value.account = accountId; }
      function rpStatusLabel(s) {
        const map = { Available: '有效', Expired: '已过期', Closed: '已关闭', UsedUp: '已用完' };
        return map[s] || s || '-';
      }
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

      // 账号下拉的输入筛选：只处理本页的筛选树，不影响其它页面
      function applyNavDirFilter() {
        const drawer = document.querySelector('.cdn-drawer.nav-dir.open');
        if (!drawer) return;
        const kw = navDirKeyword.value.trim().toLowerCase();
        let matched = 0;
        Array.from(drawer.children).forEach(el => {
          if (el.classList.contains('nav-dir-search') ||
              el.classList.contains('cdn-drawer-title') ||
              el.classList.contains('cdn-drawer-group') ||
              el.classList.contains('nav-dir-empty')) {
            el.style.display = '';
            return;
          }
          if (!el.classList.contains('cdn-drawer-item')) return;
          const isTotal = !el.classList.contains('sub-2');
          const text = (el.textContent || '').trim().toLowerCase();
          const show = !kw || isTotal || text.includes(kw);
          el.style.display = show ? '' : 'none';
          if (show && !isTotal) matched++;
        });
        let empty = drawer.querySelector('.nav-dir-empty');
        if (!empty) {
          empty = document.createElement('div');
          empty.className = 'nav-dir-empty';
          empty.textContent = '无匹配账号';
          drawer.appendChild(empty);
        }
        empty.style.display = (kw && matched === 0) ? '' : 'none';
      }
      watch(navDirKeyword, () => nextTick(applyNavDirFilter));

      onMounted(() => { if (CloudCore.checkSessionValid()) fetchResourcePackages(); });

      const navDirLabel = computed(() => {
        const acc = rpAccounts.value.find(a => a.account_id === rpFilters.value.account);
        if (acc) return acc.account_name || acc.account_id;
        return rpFilters.value.account ? '已选账号' : '全部';
      });

      return {
        rpLoading, rpList, rpAccounts, rpErrors, rpFilters, navDirKeyword, navDirLabel,
        filteredResourcePackages, rpSelectAccount, rpStatusLabel, rpTime, productLabel,
        fetchResourcePackages, applyNavDirFilter,
      };
    },
  });
})();
