/**
 * 「按账号同步」弹窗（域名 / DNS / CDN / ECS 共用）
 *
 * 独立页面与 SPA 都可以直接用，不依赖 app.js：
 *   CloudSyncModal.open({ type: 'domains', typeName: '域名注册列表', onDone: fn })
 *   type      —— 后端 /api/sync/{type} 的类型：domains / dns / cdn / ecs
 *   typeName  —— 弹窗标题里显示的中文名
 *   onDone    —— 同步完成后的回调（一般是刷新列表）
 */
(function () {
  if (window.CloudSyncModal) return;

  // 与 core.js 保持一致：登录态存在 cloud_token，请求头直接传裸 token
  // （后端同时兼容 "Bearer xxx" 与裸 token）。旧 key 仅作兼容回退。
  var TOKEN_KEY = 'cloud_token';
  var PLATFORM_LABELS = {
    aliyun: '阿里云',
    aws: 'AWS',
    tencent: '腾讯云',
    huawei: '华为云',
    azure: 'Azure',
    gcp: 'Google Cloud',
  };

  function token() {
    return localStorage.getItem(TOKEN_KEY)
      || localStorage.getItem('token')
      || localStorage.getItem('access_token')
      || '';
  }

  // 优先复用 CloudCore.authFetch：它读 cloud_token、处理 401 会话失效；
  // 未加载 core.js 的环境（如旧 SPA 内联调用）再回退到本地实现。
  function authFetch(url, options) {
    if (window.CloudCore && typeof window.CloudCore.authFetch === 'function') {
      return window.CloudCore.authFetch(url, options);
    }
    var opts = options || {};
    opts.headers = Object.assign({}, opts.headers, { Authorization: token() });
    return fetch(url, opts);
  }

  function platformLabel(p) {
    return PLATFORM_LABELS[p] || p || '-';
  }

  function toast(msg, kind) {
    if (window.CloudCore) {
      if (typeof window.CloudCore.showToast === 'function') {
        window.CloudCore.showToast(msg, kind);
        return;
      }
      if (typeof window.CloudCore.toast === 'function') {
        window.CloudCore.toast(msg, kind);
        return;
      }
    }
    var el = document.createElement('div');
    el.textContent = msg;
    el.style.cssText = [
      'position:fixed', 'left:50%', 'top:72px', 'transform:translateX(-50%)', 'z-index:9999',
      'padding:10px 16px', 'border-radius:6px', 'font-size:13px', 'color:#fff',
      'background:' + (kind === 'error' ? '#e34d59' : '#1e5fa8'),
      'box-shadow:0 6px 20px rgba(0,0,0,.18)', 'transition:opacity .3s',
    ].join(';');
    document.body.appendChild(el);
    setTimeout(function () { el.style.opacity = '0'; }, 2000);
    setTimeout(function () { el.remove(); }, 2400);
  }

  // 读取同步进度 SSE
  function readProgress(taskId, onDone, onError) {
    var url = '/api/sync/progress?task_id=' + encodeURIComponent(taskId);
    authFetch(url)
      .then(function (response) {
        var reader = response.body.getReader();
        var decoder = new TextDecoder();
        var buffer = '';
        function pump() {
          return reader.read().then(function (res) {
            if (res.done) { onDone(); return; }
            buffer += decoder.decode(res.value, { stream: true });
            var lines = buffer.split('\n');
            buffer = lines.pop() || '';
            for (var i = 0; i < lines.length; i++) {
              var line = lines[i];
              if (line.indexOf('event: done') === 0) { onDone(); return; }
              if (line.indexOf('event: error') === 0) { onError(); return; }
            }
            return pump();
          });
        }
        return pump();
      })
      .catch(function () { onError(); });
  }

  var host = null;
  var app = null;
  var vm = null;

  function ensureMounted() {
    if (app) return;
    var creator = (window.Vue && window.Vue.createApp)
      || (window.CloudCore && window.CloudCore.Vue && window.CloudCore.Vue.createApp);
    if (!creator) throw new Error('Vue 未加载');

    host = document.createElement('div');
    host.id = 'shared-sync-modal';
    document.body.appendChild(host);

    app = creator({
      data: function () {
        return {
          visible: false,
          type: '',
          typeName: '',
          list: [],
          selected: [],
          running: false,
          onDone: null,
        };
      },
      computed: {
        allSelected: function () {
          return this.list.length > 0 && this.list.every(function (a) {
            return this.selected.indexOf(a.account_id) !== -1;
          }, this);
        },
      },
      methods: {
        platformLabel: platformLabel,
        toggleAll: function (e) {
          this.selected = e.target.checked ? this.list.map(function (a) { return a.account_id; }) : [];
        },
        close: function () {
          if (this.running) return;
          this.visible = false;
        },
        submit: async function () {
          if (!this.selected.length) { toast('请至少选择一个账号', 'error'); return; }
          var self = this;
          self.running = true;
          try {
            var res = await authFetch('/api/sync/' + self.type, {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({ account_ids: self.selected }),
            });
            var json = await res.json();
            if (json.code !== 0) {
              toast(json.msg || '同步失败', 'error');
              self.running = false;
              return;
            }
            self.visible = false;
            toast('同步任务已触发：' + self.typeName);
            readProgress(json.task_id, function () {
              self.running = false;
              toast('同步完成，已刷新列表');
              if (typeof self.onDone === 'function') self.onDone();
            }, function () {
              self.running = false;
              toast('同步失败，请查看服务端日志', 'error');
            });
          } catch (e) {
            self.running = false;
            toast('同步请求失败', 'error');
          }
        },
      },
      template: [
        '<div class="modal-overlay" v-if="visible" @click.self="close">',
        '  <div class="modal" style="max-width:480px;">',
        '    <div class="modal-header">',
        '      <h2>选择账号同步{{ typeName }}</h2>',
        '      <button class="btn-close" @click="close">&times;</button>',
        '    </div>',
        '    <div class="modal-body">',
        '      <p class="text-gray text-sm" style="margin:0 0 14px;">勾选要同步的账号（默认全选）；未勾选的不会同步。云平台不支持某资源时会自动跳过。</p>',
        '      <label style="display:flex; align-items:center; gap:8px; padding:10px 12px; border:1px solid #d0d7de; border-radius:8px; margin-bottom:8px; cursor:pointer; font-weight:600;">',
        '        <input type="checkbox" :checked="allSelected" @change="toggleAll" style="width:16px;height:16px;" />',
        '        <span>全选（{{ selected.length }}/{{ list.length }}）</span>',
        '      </label>',
        '      <div style="max-height:320px; overflow-y:auto;">',
        '        <label v-for="a in list" :key="a.id" style="display:flex; align-items:center; gap:8px; padding:8px 12px; border:1px solid #e8e8e8; border-radius:8px; margin-bottom:6px; cursor:pointer; font-weight:normal;">',
        '          <input type="checkbox" :value="a.account_id" v-model="selected" style="width:16px;height:16px;" />',
        '          <span>{{ a.name }}</span>',
        '          <span class="tag tag-platform" style="margin-left:auto;">{{ platformLabel(a.platform) }}</span>',
        '        </label>',
        '        <p v-if="list.length === 0" class="text-gray text-sm text-center py-20">没有可用的启用账号</p>',
        '      </div>',
        '    </div>',
        '    <div class="modal-footer">',
        '      <button class="btn btn-default" @click="close">取消</button>',
        '      <button class="btn btn-primary" :disabled="selected.length === 0 || running" @click="submit">{{ running ? \'同步中…\' : \'开始同步（\' + selected.length + \'）\' }}</button>',
        '    </div>',
        '  </div>',
        '</div>',
      ].join(''),
    });
    vm = app.mount(host);
  }

  window.CloudSyncModal = {
    open: function (options) {
      var opts = options || {};
      try {
        ensureMounted();
      } catch (e) {
        toast('同步弹窗初始化失败：' + (e && e.message ? e.message : e), 'error');
        return;
      }
      if (!vm) return;
      vm.type = opts.type || '';
      vm.typeName = opts.typeName || '';
      vm.onDone = opts.onDone || null;
      vm.list = [];
      vm.selected = [];
      vm.running = false;
      vm.visible = true;
      // 后端 GetAccountList 的 size 上限为 100，超过会被重置成 10
      authFetch('/api/account/?size=100')
        .then(function (r) { return r.json(); })
        .then(function (json) {
          if (json.code !== 0) {
            toast(json.msg || '账号列表加载失败，请重新登录后再试', 'error');
            return;
          }
          vm.list = (json.data.list || []).filter(function (a) {
            return a.enabled === 1 || a.enabled === true;
          });
          vm.selected = vm.list.map(function (a) { return a.account_id; });
        })
        .catch(function () { toast('账号列表加载失败，请检查网络或登录态', 'error'); });
    },
  };
})();
