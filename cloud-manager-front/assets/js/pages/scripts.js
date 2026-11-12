/* 脚本库 · 独立页面业务脚本（只依赖 CloudCore） */
(function () {
  'use strict';

  const { ref, reactive, computed, onMounted } = CloudCore.Vue;
  const { authFetch, showToast, logout, formatDateTime } = CloudCore;

  CloudCore.mountPage({
    key: 'scripts',
    title: '脚本库',
    icon: 'fa-code',
    hasNavDir: false,
    template: document.getElementById('page-template').innerHTML,
    setup() {
      const loading = ref(false);
      const dir = ref('');
      const items = ref([]);
      const keyword = ref('');
      const categoryFilter = ref('');
      const editorOpen = ref(false);
      const editing = ref(false);
      const saving = ref(false);
      const uploadOpen = ref(false);
      const uploading = ref(false);
      const uploadFile = ref(null);
      const uploadCategory = ref('');
      const previewOpen = ref(false);
      const previewTitle = ref('');
      const previewContent = ref('');

      const editor = reactive({
        id: '', filename: '', category: '', name: '', description: '',
        shell: 'sh', timeout_sec: 300, concurrency: 5, content: '', params: [],
      });

      const categories = computed(() =>
        Array.from(new Set(items.value.map(s => s.category))).sort());
      const filtered = computed(() => {
        const kw = keyword.value.trim().toLowerCase();
        return items.value.filter(s => {
          if (categoryFilter.value && s.category !== categoryFilter.value) return false;
          if (!kw) return true;
          return [s.name, s.id, s.category, s.description].join(' ').toLowerCase().includes(kw);
        });
      });

      async function api(url, opts) {
        try {
          const res = await authFetch(url, opts || {});
          const json = await res.json();
          if (json.code === 401) { logout(); return null; }
          if (json.code !== 0) { showToast(json.msg || '请求失败', 'error'); return null; }
          return json.data;
        } catch (e) {
          showToast('网络请求失败', 'error');
          return null;
        }
      }

      async function load() {
        loading.value = true;
        const d = await api('/api/bastion/scripts');
        loading.value = false;
        if (!d) return;
        dir.value = d.dir || '';
        items.value = (d.items || []).map(s => ({ ...s, params: s.params || [] }));
      }
      onMounted(load);

      function resetEditor() {
        editor.id = ''; editor.filename = ''; editor.category = '';
        editor.name = ''; editor.description = ''; editor.shell = 'sh';
        editor.timeout_sec = 300; editor.concurrency = 5;
        editor.content = ''; editor.params = [];
      }

      function openNew() {
        resetEditor();
        editing.value = false;
        editorOpen.value = true;
      }

      async function openEdit(s) {
        const d = await api('/api/bastion/scripts/detail?id=' + encodeURIComponent(s.id));
        if (!d) return;
        resetEditor();
        editing.value = true;
        editor.id = s.id;
        const parts = String(s.id).split('/');
        editor.filename = parts.pop();
        editor.category = parts.join('/');
        editor.name = d.name || '';
        editor.description = d.description || '';
        editor.shell = d.shell || 'sh';
        editor.timeout_sec = d.timeout_sec || 300;
        editor.concurrency = d.concurrency || 5;
        editor.content = d.content || '';
        editor.params = (d.params || []).map(p => ({
          key: p.key || '', label: p.label || '', type: p.type || 'text',
          default: p.default == null ? '' : String(p.default),
          optionsText: (p.options || []).join(','), regex: p.regex || '',
          required: !!p.required, min: p.min, max: p.max,
        }));
        editorOpen.value = true;
      }

      function addParam() {
        editor.params.push({
          key: '', label: '', type: 'text', default: '',
          optionsText: '', regex: '', required: false,
        });
      }

      async function save() {
        if (saving.value) return;
        const filename = (editor.filename || '').trim();
        if (!/^[A-Za-z0-9_-]+\.sh$/i.test(filename)) {
          showToast('文件名只能用字母、数字、下划线、短横线，且以 .sh 结尾', 'error'); return;
        }
        const cat = (editor.category || '').trim();
        if (cat && !/^[A-Za-z0-9_-]+$/.test(cat)) {
          showToast('分类目录只能用字母、数字、下划线、短横线', 'error'); return;
        }
        if (!editor.content.trim()) { showToast('请填写脚本正文', 'error'); return; }
        const id = editing.value ? editor.id : (cat ? cat + '/' + filename : filename);
        const params = editor.params.map(p => {
          const o = {
            key: (p.key || '').trim(), label: (p.label || '').trim(),
            type: p.type || 'text', default: String(p.default || ''), required: !!p.required,
          };
          if (p.type === 'select') {
            o.options = (p.optionsText || '').split(',').map(x => x.trim()).filter(Boolean);
          }
          if (p.regex) o.regex = String(p.regex).trim();
          if (p.min !== undefined && p.min !== null && p.min !== '') o.min = Number(p.min);
          if (p.max !== undefined && p.max !== null && p.max !== '') o.max = Number(p.max);
          return o;
        });
        if (params.some(p => !p.key)) { showToast('参数 KEY 不能为空', 'error'); return; }

        saving.value = true;
        const d = await api('/api/bastion/scripts', {
          method: 'POST', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            id, content: editor.content, name: editor.name, category: cat,
            description: editor.description, shell: editor.shell,
            timeout_sec: Number(editor.timeout_sec) || 300,
            concurrency: Number(editor.concurrency) || 5, params,
          }),
        });
        saving.value = false;
        if (!d) return;
        editorOpen.value = false;
        showToast('已保存到脚本目录', 'success');
        load();
      }

      function onFileChange(e) {
        uploadFile.value = (e.target.files && e.target.files[0]) ? e.target.files[0] : null;
      }
      function openUpload() {
        uploadFile.value = null; uploadCategory.value = ''; uploadOpen.value = true;
      }
      async function upload() {
        if (!uploadFile.value) { showToast('请选择 .sh 文件', 'error'); return; }
        const fd = new FormData();
        fd.append('file', uploadFile.value);
        fd.append('category', (uploadCategory.value || '').trim());
        uploading.value = true;
        const d = await api('/api/bastion/scripts/upload', { method: 'POST', body: fd });
        uploading.value = false;
        if (!d) return;
        uploadOpen.value = false;
        showToast('上传成功，已自动发现', 'success');
        load();
      }

      async function removeScript(s) {
        if (!window.confirm('确定删除脚本 ' + s.id + ' 吗？同名 .json 元数据也会一并删除。')) return;
        const d = await api('/api/bastion/scripts?id=' + encodeURIComponent(s.id), { method: 'DELETE' });
        if (!d) return;
        showToast('已删除', 'success');
        load();
      }

      async function preview(s) {
        const params = {};
        (s.params || []).forEach(p => { if (p.default) params[p.key] = String(p.default); });
        const d = await api('/api/bastion/scripts/preview?id=' + encodeURIComponent(s.id) +
          '&params=' + encodeURIComponent(JSON.stringify(params)));
        if (!d) return;
        previewTitle.value = s.name;
        previewContent.value = d.content || '';
        previewOpen.value = true;
      }

      function deploy(s) {
        window.location.href = '/assets/pages/batch-exec.html?script=' + encodeURIComponent(s.id);
      }

      return {
        loading, dir, items, keyword, categoryFilter, categories, filtered,
        editorOpen, editing, saving, editor, addParam, openNew, openEdit, save,
        uploadOpen, uploading, uploadFile, uploadCategory, onFileChange, openUpload, upload,
        previewOpen, previewTitle, previewContent, preview,
        removeScript, deploy, load, formatDateTime,
      };
    },
  });
})();
