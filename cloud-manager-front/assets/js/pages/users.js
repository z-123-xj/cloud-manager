/* ============================================================================
 * 用户管理 · 独立页面业务脚本
 *
 * 只依赖 CloudCore 公共内核，不加载 app.js。本文件的任何语法 / 运行时错误
 * 只会影响 /assets/pages/users.html，不会波及其它页面。
 * 业务逻辑从旧 app.js「用户管理」分节原样迁出，接口路径保持不变。
 * ==========================================================================*/
(function () {
  'use strict';

  const { ref, reactive, computed, onMounted, nextTick } = CloudCore.Vue;
  const { authFetch, showToast, formatDate, logout } = CloudCore;

  CloudCore.mountPage({
    key: 'users',
    title: '用户管理',
    hasNavDir: false,
    template: document.getElementById('page-template').innerHTML,
    setup() {
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

      const userFilters = reactive({ keyword: '' });
      const userForm = reactive({
        username: '', password: '', email: '', real_name: '', role: 'user',
      });

      const userTotalPages = computed(() => Math.ceil(userTotal.value / userSize.value) || 1);

      function userRoleClass(role) {
        const map = { admin: 'tag tag-danger', readonly: 'tag tag-default', user: 'tag tag-success' };
        return map[role] || 'tag tag-default';
      }
      function userRoleText(role) {
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
        userRoleDrpOpen.value = false;
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

          const res = userEditing.value
            ? await authFetch('/api/user/' + userEditingId.value, {
                method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
              })
            : await authFetch('/api/user/', {
                method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
              });
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
          if (json.code === 0) { showToast('删除成功'); fetchUsers(); }
          else { showToast(json.msg || '删除失败', 'error'); }
        } catch (e) { showToast('网络请求失败', 'error'); }
      }

      async function unlockUser(item) {
        if (!confirm('确定解锁用户 "' + item.username + '" 吗？')) return;
        try {
          const res = await authFetch('/api/user/' + item.id + '/unlock', { method: 'PUT' });
          const json = await res.json();
          if (json.code === 0) { showToast('解锁成功'); fetchUsers(); }
          else { showToast(json.msg || '解锁失败', 'error'); }
        } catch (e) { showToast('网络请求失败', 'error'); }
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

      // 未登录时 core 会显示「登录已失效」卡片，此时不发起业务请求
      onMounted(() => { if (CloudCore.checkSessionValid()) fetchUsers(); });

      return {
        userLoading, userSaving, userList, userPage, userSize, userTotal, userPageInput,
        userModalVisible, userEditing, userRoleDrpOpen, userFilters, userForm, userTotalPages,
        userRoleClass, userRoleText, formatDate,
        openUserModal, saveUser, deleteUser, unlockUser,
        userSearch, userResetFilters, userOnSizeChange, userGoPage, userJumpPage,
      };
    },
  });
})();
