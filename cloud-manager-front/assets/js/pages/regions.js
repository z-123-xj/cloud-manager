/* 实例地域开通 · 独立页面业务脚本（只依赖 CloudCore） */
(function () {
  'use strict';

  const { ref, onMounted } = CloudCore.Vue;
  const { authFetch, showToast } = CloudCore;

  CloudCore.mountPage({
    key: 'regions',
    title: '实例地域开通',
    icon: 'fa-map-location-dot',
    hasNavDir: false,
    template: document.getElementById('page-template').innerHTML,
    setup() {
      const regionLoading = ref(false);
      const regionPlatform = ref('aliyun');
      const regionList = ref([]);

      async function fetchRegions() {
        regionLoading.value = true;
        try {
          const res = await authFetch('/api/regions?platform=' + regionPlatform.value);
          const json = await res.json();
          if (json.code === 0) regionList.value = json.data || [];
          else showToast(json.msg || '获取地域列表失败', 'error');
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

      onMounted(() => { if (CloudCore.checkSessionValid()) fetchRegions(); });

      return { regionLoading, regionPlatform, regionList, fetchRegions, toggleRegion };
    },
  });
})();
