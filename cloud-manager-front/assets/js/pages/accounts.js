/* 云平台账号管理 · 独立页面业务脚本（只依赖 CloudCore） */
(function () {
  'use strict';

  const { ref, reactive, computed, onMounted, nextTick } = CloudCore.Vue;
  const { authFetch, showToast, formatDate, logout } = CloudCore;

  CloudCore.mountPage({
    key: 'accounts',
    title: '云平台账号管理',
    icon: 'fa-address-card',
    hasNavDir: false,
    template: document.getElementById('page-template').innerHTML,
    setup() {
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

      const accountFilters = reactive({ keyword: '', platform: '', site: '' });
      const accountForm = reactive({
        platform: 'aliyun', site: 'cn', name: '',
        access_key: '', access_secret: '', enabled: 1,
      });

      const accountTotalPages = computed(() => Math.ceil(accountTotal.value / accountSize.value) || 1);

      function accountPlatformLabel(p) {
        const map = { aliyun: '阿里云', aws: 'AWS', tencent: '腾讯云', cloudflare: 'Cloudflare' };
        return map[p] || p;
      }
      function accountSiteLabel(s) {
        const map = { cn: '国内站', intl: '国际站' };
        return map[s] || s || '-';
      }
      function onAccountPlatformChange() {
        // Cloudflare / AWS 无国内国际站之分，固定国际站
        if (accountForm.platform === 'cloudflare' || accountForm.platform === 'aws') {
          accountForm.site = 'intl';
        }
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
        if (!accountEditing.value) {
          if (!accountForm.access_key) { showToast('请输入AccessKey', 'error'); return; }
          if (!accountForm.access_secret) { showToast('请输入AccessSecret', 'error'); return; }
        }
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
          if (accountForm.access_key && accountForm.access_key !== '***') body.access_key = accountForm.access_key;
          if (accountForm.access_secret && accountForm.access_secret !== '***') body.access_secret = accountForm.access_secret;

          const res = accountEditing.value
            ? await authFetch('/api/account/' + accountEditingId.value, {
                method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
              })
            : await authFetch('/api/account/', {
                method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
              });
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
          if (json.code === 0) { showToast('删除成功'); fetchAccounts(); }
          else { showToast(json.msg || '删除失败', 'error'); }
        } catch (e) { showToast('网络请求失败', 'error'); }
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

      onMounted(() => { if (CloudCore.checkSessionValid()) fetchAccounts(); });

      return {
        accountLoading, accountSaving, accountList2, accountPage, accountSize, accountTotal,
        accountPageInput, accountModalVisible, accountEditing, accountFilters, accountForm,
        accountTotalPages, formatDate, accountPlatformLabel, accountSiteLabel,
        onAccountPlatformChange, openAccountModal, saveAccount, deleteAccount,
        accountSearch, accountResetFilters, accountOnSizeChange, accountGoPage, accountJumpPage,
      };
    },
  });
})();
