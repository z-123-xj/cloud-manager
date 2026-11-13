/* 脚本库 · 独立页面业务脚本（只依赖 CloudCore） */
(function () {
  'use strict';

  const { ref, reactive, computed, onMounted, onUnmounted, nextTick } = CloudCore.Vue;
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
      const editorSnapshot = ref('');
      const editorTextarea = ref(null);
      const uploadOpen = ref(false);
      const uploading = ref(false);
      const uploadFile = ref(null);
      const uploadCategory = ref('');
      const previewOpen = ref(false);
      const previewTitle = ref('');
      const previewContent = ref('');

      const editor = reactive({
        id: '', filename: '', category: '', name: '', description: '',
        shell: 'bash', timeout_sec: 300, concurrency: 5, content: '', params: [],
      });

      const dirCategories = ref([]); // 脚本库里实际存在的分类目录（含还没有脚本的空目录）
      const categories = computed(() =>
        Array.from(new Set(items.value.map(s => s.category))).sort());
      // 新建/上传时可选：目录里已有的（含空目录） + 已有脚本用到的分类
      const categoryOptions = computed(() =>
        Array.from(new Set([
          ...dirCategories.value,
          ...categories.value.filter(c => c && c !== '未分类'),
        ])).sort());
      const catComboOpen = ref(false);
      const catComboEl = ref(null);
      const uploadCatOpen = ref(false);
      const uploadCatComboEl = ref(null);
      function pickCategory(c) {
        editor.category = c;
        catComboOpen.value = false;
      }
      function pickUploadCategory(c) {
        uploadCategory.value = c;
        uploadCatOpen.value = false;
      }
      function onCatOutside(e) {
        const a = catComboEl.value;
        const b = uploadCatComboEl.value;
        if (a && !a.contains(e.target)) catComboOpen.value = false;
        if (b && !b.contains(e.target)) uploadCatOpen.value = false;
      }
      const filtered = computed(() => {
        const kw = keyword.value.trim().toLowerCase();
        return items.value.filter(s => {
          if (categoryFilter.value && s.category !== categoryFilter.value) return false;
          if (!kw) return true;
          return [s.name, s.id, s.category, s.description].join(' ').toLowerCase().includes(kw);
        });
      });

      // 打开弹窗时记一份快照，用于判断有没有未保存的修改，避免误点遮罩丢内容。
      function editorState() {
        return JSON.stringify({
          filename: editor.filename, category: editor.category,
          name: editor.name, description: editor.description, shell: editor.shell,
          timeout_sec: editor.timeout_sec, concurrency: editor.concurrency,
          content: editor.content,
          params: editor.params.map(p => ({
            key: p.key, label: p.label, type: p.type, default: p.default,
            optionsText: p.optionsText, regex: p.regex,
            required: p.required, min: p.min, max: p.max,
          })),
        });
      }
      const editorDirty = computed(() =>
        !!editorSnapshot.value && editorState() !== editorSnapshot.value);

      // 已定义参数生成可点击的 {{KEY}}，避免手工写成 ${KEY} / $1
      const placeholders = computed(() => editor.params
        .filter(p => (p.key || '').trim())
        .map(p => ({ key: p.key.trim(), text: '{{' + p.key.trim() + '}}' })));

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
        dirCategories.value = Array.isArray(d.categories) ? d.categories : [];
      }
      // 拖动右下角把手拉高正文框时，弹窗到达高度上限后不再变高，
      // 内容区是独立滚动容器，新露出的底部与下方提示条会被挤出视野，
      // 看起来像「下摆没跟着扩大」。这里在尺寸变化时把它们带进视野。
      let editorResizeObserver = null;
      function followEditorResize() {
        const el = editorTextarea.value;
        if (!el || editorResizeObserver || typeof ResizeObserver === 'undefined') return;
        let lastH = 0;
        editorResizeObserver = new ResizeObserver(entries => {
          const h = entries && entries[0] ? entries[0].contentRect.height : el.clientHeight;
          const grew = lastH > 0 && h > lastH;
          lastH = h;
          // 只在用户真的把编辑框拉高时跟随，避免打开弹窗就滚到底部
          if (!editorOpen.value || !grew) return;
          const body = el.closest('.modal-body');
          if (!body) return;
          const tail = el.parentElement ? el.parentElement.querySelector('.sl-editor-bar') : null;
          const node = tail || el;
          const view = body.getBoundingClientRect();
          const box = node.getBoundingClientRect();
          if (box.bottom > view.bottom - 8) {
            body.scrollTop += box.bottom - view.bottom + 16;
          }
        });
        editorResizeObserver.observe(el);
      }

      onMounted(() => {
        load();
        nextTick(followEditorResize);
        window.addEventListener('mousedown', onCatOutside);
      });
      onUnmounted(() => {
        if (editorResizeObserver) { editorResizeObserver.disconnect(); editorResizeObserver = null; }
        window.removeEventListener('mousedown', onCatOutside);
      });

      // 从批量执行页等站内页面进来时回上一页；直接打开 / 外部来源则回工作台。
      function goBack() {
        let sameSite = false;
        try {
          sameSite = !!document.referrer && new URL(document.referrer).origin === window.location.origin;
        } catch (e) {
          sameSite = false;
        }
        if (sameSite && window.history.length > 1) {
          window.history.back();
        } else {
          window.location.href = CloudCore.ROUTES.home;
        }
      }

      function snapshotEditor() {
        editorSnapshot.value = editorState();
      }

      function closeEditor() {
        if (saving.value) return;
        if (editorDirty.value && !window.confirm('脚本还没保存，确定关闭吗？未保存的内容会丢失。')) return;
        editorOpen.value = false;
        editorSnapshot.value = '';
        catComboOpen.value = false;
      }

      function closeUpload() {
        if (uploading.value) return;
        if (uploadFile.value && !window.confirm('已选择文件但还没上传，确定关闭吗？')) return;
        uploadOpen.value = false;
        uploadCatOpen.value = false;
      }

      // 记录正文框最近一次光标位置：点参数 chip 会让 textarea 失焦，插入时要按这个位置
      const editorCaret = { start: 0, end: 0 };
      let editAnchor = null; // 最近一次编辑的起始位置，撤销/重做时把光标和滚动拉回这一行
      function rememberCaret(e) {
        const el = e && e.target;
        if (!el || el !== editorTextarea.value || el.selectionStart == null) return;
        editorCaret.start = el.selectionStart;
        editorCaret.end = el.selectionEnd == null ? editorCaret.start : el.selectionEnd;
      }

      function scrollCaretIntoView(el, pos) {
        const cs = window.getComputedStyle(el);
        const lineHeight = parseFloat(cs.lineHeight) || 20;
        const padTop = parseFloat(cs.paddingTop) || 0;
        const line = el.value.slice(0, pos).split('\n').length - 1;
        const y = padTop + line * lineHeight;
        const top = el.scrollTop;
        const bottom = top + el.clientHeight;
        if (y < top + lineHeight || y > bottom - lineHeight * 2) {
          el.scrollTop = Math.max(0, y - el.clientHeight / 2);
        }
      }

      // Ctrl+Z / Ctrl+Y 时浏览器只还原文本，焦点和可视位置不一定回得来；
      // 这里把光标放回本次编辑的起点、滚动到那一行，并同步 v-model。
      function onEditorInput(e) {
        const el = e && e.target;
        if (!el || el !== editorTextarea.value) return;
        const isHistory = e.inputType === 'historyUndo' || e.inputType === 'historyRedo';
        if (isHistory) {
          const pos = Math.min(editAnchor == null ? editorCaret.start : editAnchor, el.value.length);
          nextTick(() => {
            el.focus();
            el.setSelectionRange(pos, pos);
            scrollCaretIntoView(el, pos);
            rememberCaret({ target: el });
            if (el.value !== editor.content) editor.content = el.value;
          });
        } else {
          editAnchor = editorCaret.start;
        }
        rememberCaret(e);
      }

      // 把 {{KEY}} 插到正文光标处。必须用浏览器原生编辑命令：
      // 直接改 editor.content 会让 Vue 重设 textarea.value，光标跳到末尾且 Ctrl+Z 撤销栈被清空。
      function insertPlaceholder(key) {
        const ph = '{{' + key + '}}';
        const el = editorTextarea.value;
        if (!el) {
          editor.content += ph;
          return;
        }
        const start = Math.min(editorCaret.start, el.value.length);
        const end = Math.min(Math.max(editorCaret.end, start), el.value.length);
        el.focus();
        el.setSelectionRange(start, end);
        rememberCaret({ target: el }); // 让 input 事件记下的撤销锚点就是本次插入点
        editAnchor = start;
        let done = false;
        try {
          done = typeof document.execCommand === 'function' &&
            document.execCommand('insertText', false, ph);
        } catch (err) { done = false; }
        if (done) {
          // execCommand 会派发 input 事件同步 v-model；这里只做兜底核对
          if (el.value !== editor.content) editor.content = el.value;
        } else {
          el.setRangeText(ph, start, end, 'end');
          editor.content = el.value;
          const pos = start + ph.length;
          nextTick(() => { el.focus(); el.setSelectionRange(pos, pos); });
        }
        rememberCaret({ target: el }); // 支持连续插入多个占位符
      }

      function resetEditor() {
        editor.id = ''; editor.filename = ''; editor.category = '';
        editor.name = ''; editor.description = ''; editor.shell = 'bash';
        editor.timeout_sec = 300; editor.concurrency = 5;
        editor.content = ''; editor.params = [];
      }

      function openNew() {
        resetEditor();
        editing.value = false;
        snapshotEditor();
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
        editor.shell = d.shell || 'bash';
        editor.timeout_sec = d.timeout_sec || 300;
        editor.concurrency = d.concurrency || 5;
        editor.content = d.content || '';
        editor.params = (d.params || []).map(p => ({
          key: p.key || '', label: p.label || '', type: p.type || 'text',
          default: p.default == null ? '' : String(p.default),
          optionsText: (p.options || []).join(','), regex: p.regex || '',
          required: !!p.required,
          min: p.min == null ? '' : p.min, max: p.max == null ? '' : p.max,
        }));
        snapshotEditor();
        editorOpen.value = true;
      }

      function addParam() {
        editor.params.push({
          key: '', label: '', type: 'text', default: '',
          optionsText: '', regex: '', required: false, min: '', max: '',
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
        // 编辑时也按当前文件名/分类组装 ID，改名由后端搬文件并清理旧文件
        const id = cat ? cat + '/' + filename : filename;
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

        // 常见误写：把 {{KEY}} 写成 ${KEY}，而正文里又没有对应的占位符
        const misused = params.filter(p => p.key &&
          editor.content.indexOf('${' + p.key + '}') >= 0 &&
          editor.content.indexOf('{{' + p.key + '}}') < 0);
        if (misused.length && !window.confirm(
          '脚本里用了 ${' + misused.map(p => p.key).join(', ') + '}，但参数必须用 {{KEY}} 才会被替换。确定仍要保存吗？')) {
          return;
        }

        saving.value = true;
        const d = await api('/api/bastion/scripts', {
          method: 'POST', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            id, old_id: editing.value ? editor.id : '',
            content: editor.content, name: editor.name, category: cat,
            description: editor.description, shell: editor.shell,
            timeout_sec: Number(editor.timeout_sec) || 300,
            concurrency: Number(editor.concurrency) || 5, params,
          }),
        });
        saving.value = false;
        if (!d) return;
        editorSnapshot.value = '';
        editorOpen.value = false;
        if (d.warn) {
          showToast(d.warn, 'error');
        } else {
          showToast(d.renamed ? '已改名并保存，旧脚本已清理' : '已保存到脚本目录', 'success');
        }
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
        if (d.prune_warn) {
          showToast('脚本已删除，但分类目录未清理：' + d.prune_warn, 'error');
        } else {
          showToast(d.pruned ? '已删除脚本，空的分类目录也已清理' : '已删除', 'success');
        }
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

      // 默认值用单引号包成 shell 字面量，含引号 / $ / 反引号也安全
      function shellQuote(v) {
        return "'" + String(v == null ? '' : v).replace(/\r?\n/g, ' ').replace(/'/g, "'\\''") + "'";
      }

      // 变量定义插在 shebang 之后，避免破坏 #!/bin/bash
      function insertAfterShebang(body, header) {
        if (!header) return body;
        if (body.indexOf('#!') === 0) {
          const i = body.indexOf('\n');
          return i < 0 ? body + '\n' + header : body.slice(0, i + 1) + header + body.slice(i + 1);
        }
        return header + body;
      }

      // 导出为不依赖平台渲染的独立脚本：默认值定义到顶部，正文 {{KEY}} 改成 ${KEY} 引用
      async function exportScript(s) {
        const d = await api('/api/bastion/scripts/detail?id=' + encodeURIComponent(s.id));
        if (!d) return;
        const params = d.params || s.params || [];
        const body = String(d.content || '').replace(/\r\n?/g, '\n');
        const keys = params.map(p => String(p.key || '').trim()).filter(Boolean);

        const bad = keys.find(k => !/^[A-Za-z_][A-Za-z0-9_]*$/.test(k));
        if (bad) {
          showToast('参数 KEY ' + bad + ' 不能作为 shell 变量名，请改成字母/下划线开头', 'error');
          return;
        }
        const defined = new Set(keys);
        const missing = new Set();
        const rendered = body.replace(/\{\{([A-Za-z0-9_-]+)\}\}/g, (m, k) => {
          if (!defined.has(k)) { missing.add(k); return m; }
          return '${' + k + '}';
        });
        if (missing.size) {
          showToast('脚本中存在未定义的占位符: ' + Array.from(missing).join(', '), 'error');
          return;
        }

        let header = '';
        if (params.length) {
          const lines = params.filter(p => String(p.key || '').trim()).map(p => {
            const k = String(p.key || '').trim();
            const note = [p.label, p.required ? '必填' : ''].filter(Boolean).join(' · ');
            return k + '=' + shellQuote(p.default) + (note ? '  # ' + note : '');
          });
          header = '# ===== 参数默认值（云管平台脚本库导出，可按需修改）=====\n' +
            lines.join('\n') + '\n' +
            '# ===== 若正文里再次给这些变量赋值，会覆盖上面的默认值 =====\n\n';
        }
        const out = insertAfterShebang(rendered, header);
        const filename = String(s.id).split('/').pop() || 'script.sh';
        const blob = new Blob([out], { type: 'text/x-shellscript;charset=utf-8' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = filename;
        document.body.appendChild(a);
        a.click();
        a.remove();
        setTimeout(() => URL.revokeObjectURL(url), 0);
        showToast('已导出 ' + filename, 'success');
      }

      function deploy(s) {
        window.location.href = '/assets/pages/batch-exec.html?script=' + encodeURIComponent(s.id);
      }

      return {
        loading, dir, items, keyword, categoryFilter, categories, filtered,
        editorOpen, editing, saving, editor, editorDirty, editorTextarea,
        placeholders, insertPlaceholder, rememberCaret, onEditorInput, addParam,
        categoryOptions, catComboOpen, catComboEl, pickCategory,
        uploadCatOpen, uploadCatComboEl, pickUploadCategory,
        openNew, openEdit, closeEditor, closeUpload, save,
        uploadOpen, uploading, uploadFile, uploadCategory, onFileChange, openUpload, upload,
        previewOpen, previewTitle, previewContent, preview,
        removeScript, deploy, exportScript, goBack, load, formatDateTime,
      };
    },
  });
})();
