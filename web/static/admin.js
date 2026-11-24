// 后台管理逻辑
const $ = (id) => document.getElementById(id);
const esc = (s) => String(s == null ? '' : s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

let typesCache = [];
let recordPage = 1;
let recordPages = 1;
const REC_SIZE = 15;
let currentUser = null;

async function api(path, opts = {}) {
  const r = await fetch('/api/admin/' + path, {
    headers: { 'Content-Type': 'application/json' },
    ...opts
  });
  if (r.status === 401) { location.href = '/admin/login'; return null; }
  const d = await r.json().catch(() => ({}));
  if (!r.ok) { toast(d.error || '操作失败'); return null; }
  return d;
}

document.addEventListener('DOMContentLoaded', async () => {
  const auth = await fetch('/api/auth/check').then(r => r.json()).catch(() => null);
  if (!auth || !auth.authenticated) { location.href = '/admin/login'; return; }
  currentUser = auth.user;
  $('welcome').textContent = `${currentUser.nickname || currentUser.username}，发文字号领取情况总览`;

  bindTabs();
  // 支持 #records 这类直链：先同步切页签，再异步取数据
  const hash = (location.hash || '').replace('#', '');
  const startTab = hash && $('tab-' + hash) ? hash : 'dashboard';
  setTabClasses(startTab);
  await activateTab(startTab);
});

function bindTabs() {
  document.querySelectorAll('.nav a[data-tab]').forEach(a => {
    a.addEventListener('click', async (e) => {
      e.preventDefault();
      location.hash = a.dataset.tab;
      await activateTab(a.dataset.tab);
    });
  });

  document.querySelectorAll('#trendSeg button').forEach(b => {
    b.addEventListener('click', () => {
      document.querySelectorAll('#trendSeg button').forEach(x => x.classList.remove('on'));
      b.classList.add('on');
      loadTrend(b.dataset.range);
    });
  });

  let t1 = null;
  $('recSearch').addEventListener('input', () => { clearTimeout(t1); t1 = setTimeout(() => loadAdminRecords(1), 300); });
  $('recArch').addEventListener('change', () => loadAdminRecords(1));
}

// 只切换页签样式，不取数据
function setTabClasses(name) {
  if (!name || !$('tab-' + name)) return;
  document.querySelectorAll('.nav a[data-tab]').forEach(x => x.classList.toggle('on', x.dataset.tab === name));
  document.querySelectorAll('.tab').forEach(t => t.classList.remove('on'));
  $('tab-' + name).classList.add('on');
}

// 切换标签页并加载数据，支持 #records 这类直链
async function activateTab(name) {
  if (!name || !$('tab-' + name)) return;
  setTabClasses(name);
  if (name === 'dashboard') await loadDashboard();
  if (name === 'types') await loadTypes();
  if (name === 'users') await loadUsers();
  if (name === 'records') await loadAdminRecords(1);
  if (name === 'import') await loadImportTypes();
  if (name === 'settings') await loadSettings();
}

// ---------- 控制台 ----------
async function loadDashboard() {
  const s = await api('stats');
  if (!s) return;
  $('stRecords').textContent = s.records;
  $('stArchived').textContent = s.archived;
  $('stUnarchived').textContent = s.unarchived;
  $('stMonth').textContent = s.month;
  loadTrend('daily');
}

async function loadTrend(range) {
  const d = await api('trend?range=' + range);
  if (!d) return;
  const max = Math.max(1, ...d.values);
  $('chart').innerHTML = d.labels.map((lb, i) => {
    const h = Math.round((d.values[i] / max) * 130);
    return `<div class="bar"><div class="col" style="height:${Math.max(4, h)}px"><span>${d.values[i]}</span></div><div class="lb">${esc(lb)}</div></div>`;
  }).join('');
  $('trendHint').textContent = range === 'daily' ? '最近 7 天' : range === 'weekly' ? '最近 8 周' : '最近 6 个月';
}

// ---------- 字号类型 ----------
async function loadTypes() {
  const list = await api('types');
  if (!list) return;
  typesCache = list;
  $('typeBody').innerHTML = list.length ? list.map(t => `
    <tr>
      <td style="color:#86868b">${t.id}</td>
      <td class="mono">${esc(t.prefix)}<span style="color:#86868b">…</span>${esc(t.suffix)}</td>
      <td class="mono">${t.current_num}</td>
      <td style="color:#6e6e73">${esc(t.description || '—')}</td>
      <td>${t.is_active ? '<span class="badge badge-green">启用</span>' : '<span class="badge badge-gray">停用</span>'}</td>
      <td>${t.receive_count}</td>
      <td>
        <div class="row-actions">
          <button class="btn btn-secondary btn-sm" onclick="openTypeModal(${t.id})">编辑</button>
          <button class="btn btn-danger btn-sm" onclick="delType(${t.id})">删除</button>
        </div>
      </td>
    </tr>`).join('') : '<tr><td colspan="7" class="empty">暂无类型</td></tr>';
}

function openTypeModal(id) {
  const t = typesCache.find(x => x.id === id) || { prefix: '', suffix: '号', current_num: 1, description: '', is_active: true };
  $('modalCard').classList.remove('wide');
  $('modalTitle').textContent = id ? '编辑字号类型' : '添加字号类型';
  $('modalBody').innerHTML = `
    <div class="grid-2">
      <div class="field"><label>前部分<span class="req">*</span></label><input class="input" id="fPrefix" value="${esc(t.prefix)}" placeholder="国发〔2026〕"></div>
      <div class="field"><label>后部分<span class="req">*</span></label><input class="input" id="fSuffix" value="${esc(t.suffix)}" placeholder="号"></div>
    </div>
    <div class="field"><label>当前编号</label><input class="input" id="fNum" type="number" min="1" value="${t.current_num || 1}"></div>
    <div class="field"><label>描述</label><input class="input" id="fDesc" value="${esc(t.description || '')}" placeholder="选填"></div>
    <div class="field" style="margin-bottom:0"><label class="switch"><input type="checkbox" id="fActive" ${t.is_active ? 'checked' : ''}><span class="track"></span><span class="label">启用</span></label></div>`;
  $('modalFoot').innerHTML = `<button class="btn btn-secondary" onclick="closeModal()">取消</button><button class="btn btn-primary" onclick="saveType(${id || 0})">保存</button>`;
  $('modal').classList.add('open');
}

async function saveType(id) {
  const body = {
    prefix: $('fPrefix').value.trim(),
    suffix: $('fSuffix').value.trim(),
    current_num: Number($('fNum').value) || 1,
    description: $('fDesc').value.trim(),
    is_active: $('fActive').checked
  };
  if (!body.prefix || !body.suffix) return toast('前后部分不能为空');
  const r = await api(id ? 'types/' + id : 'types', { method: id ? 'PUT' : 'POST', body: JSON.stringify(body) });
  if (!r) return;
  closeModal(); toast('已保存'); loadTypes();
}

async function delType(id) {
  if (!confirm('确定删除该发文字号类型？')) return;
  const r = await api('types/' + id, { method: 'DELETE' });
  if (!r) return;
  toast('已删除'); loadTypes();
}

// ---------- 用户 ----------
async function loadUsers() {
  const list = await api('users');
  if (!list) return;
  window.__users = list;
  $('userBody').innerHTML = list.length ? list.map(u => `
    <tr>
      <td style="color:#86868b">${u.id}</td>
      <td class="mono">${esc(u.username)}</td>
      <td>${esc(u.nickname || '—')}</td>
      <td>${u.is_admin ? '<span class="badge badge-blue">管理员</span>' : '<span class="badge badge-gray">普通</span>'}</td>
      <td>${u.auth_source === 'dzz' ? '<span class="badge badge-blue">DzzOffice</span>' : '<span class="badge badge-gray">本地</span>'}</td>
      <td style="color:#6e6e73">${esc(u.remark || '—')}</td>
      <td>
        <div class="row-actions">
          <button class="btn btn-secondary btn-sm" onclick="openUserModal(${u.id})">编辑</button>
          ${u.username !== 'admin' ? `<button class="btn btn-danger btn-sm" onclick="delUser(${u.id})">删除</button>` : ''}
        </div>
      </td>
    </tr>`).join('') : '<tr><td colspan="7" class="empty">暂无用户</td></tr>';
}

function openUserModal(id) {
  const u = (id ? (window.__users || []).find(x => x.id === id) : null) || { username: '', nickname: '', is_admin: false, remark: '', auth_source: 'local' };
  $('modalCard').classList.remove('wide');
  $('modalTitle').textContent = id ? '编辑用户' : '添加用户';
  $('modalBody').innerHTML = `
    <div class="grid-2">
      <div class="field"><label>账号${id ? '' : '<span class="req">*</span>'}</label>
        <input class="input" id="uName" value="${esc(u.username)}" ${id ? 'disabled' : ''} placeholder="登录账号"></div>
      <div class="field"><label>${id ? '重置密码' : '密码<span class="req">*</span>'}</label>
        <input class="input" id="uPass" type="password" placeholder="${id ? '留空则不修改' : '至少 6 位'}"></div>
    </div>
    <div class="field"><label>姓名</label><input class="input" id="uNick" value="${esc(u.nickname || '')}"></div>
    <div class="field" style="margin-bottom:0"><label class="switch"><input type="checkbox" id="uAdmin" ${u.is_admin ? 'checked' : ''}><span class="track"></span><span class="label">管理员权限</span></label></div>`;
  $('modalFoot').innerHTML = `<button class="btn btn-secondary" onclick="closeModal()">取消</button><button class="btn btn-primary" onclick="saveUser(${id || 0})">保存</button>`;
  $('modal').classList.add('open');
}

async function saveUser(id) {
  const password = $('uPass').value;
  const body = {
    username: $('uName').value.trim(),
    password,
    nickname: $('uNick').value.trim(),
    is_admin: $('uAdmin').checked,
    remark: ''
  };
  if (!id && (!body.username || password.length < 6)) return toast('账号不能为空，密码至少 6 位');
  const r = await api(id ? 'users/' + id : 'users', { method: id ? 'PUT' : 'POST', body: JSON.stringify(body) });
  if (!r) return;
  closeModal(); toast('已保存'); loadUsers();
}

async function delUser(id) {
  if (!confirm('确定删除该用户？')) return;
  const r = await api('users/' + id, { method: 'DELETE' });
  if (!r) return;
  toast('已删除'); loadUsers();
}

// ---------- 领取记录 ----------
function recordQuery() {
  const q = encodeURIComponent($('recSearch').value.trim());
  const arch = $('recArch').value;
  return `q=${q}&archived=${arch}`;
}

async function loadAdminRecords(p) {
  recordPage = p || 1;
  const d = await api(`records?page=${recordPage}&size=${REC_SIZE}&` + recordQuery());
  if (!d) return;
  recordPages = d.pages || 1;
  const list = d.records || [];
  window.__records = list;
  $('recBody').innerHTML = list.length ? list.map((r, i) => `
    <tr>
      <td style="color:#86868b">${(recordPage - 1) * REC_SIZE + i + 1}</td>
      <td class="mono">${esc(r.doc_number)}</td>
      <td>${esc(r.title)}</td>
      <td>${esc(r.receive_person || '—')}</td>
      <td>${r.is_archived ? '<span class="badge badge-green">已归档</span>' : '<span class="badge badge-gray">未归档</span>'}</td>
      <td style="color:#86868b;font-variant-numeric:tabular-nums">${esc(r.received_at)}</td>
      <td>
        <div class="row-actions">
          <button class="btn btn-secondary btn-sm" onclick="openRecordModal(${r.id})">编辑</button>
          <button class="btn btn-danger btn-sm" onclick="delRecord(${r.id})">删除</button>
        </div>
      </td>
    </tr>`).join('') : '<tr><td colspan="7" class="empty">暂无记录</td></tr>';
  renderRecPager(d.total, recordPages);
}

function renderRecPager(total, pages) {
  const el = $('recPager');
  if (!total) { el.innerHTML = ''; return; }
  const totalPages = pages || 1;
  let h = `<span class="info">共 ${total} 条 · 第 ${recordPage} / ${totalPages} 页</span>`;
  h += `<button ${recordPage <= 1 ? 'disabled' : ''} onclick="loadAdminRecords(${recordPage - 1})">上一页</button>`;
  const from = Math.max(1, recordPage - 2), to = Math.min(totalPages, recordPage + 2);
  if (from > 1) h += `<button onclick="loadAdminRecords(1)">1</button>${from > 2 ? '<span class="info">…</span>' : ''}`;
  for (let i = from; i <= to; i++) h += `<button class="${i === recordPage ? 'on' : ''}" onclick="loadAdminRecords(${i})">${i}</button>`;
  if (to < totalPages) h += `${to < totalPages - 1 ? '<span class="info">…</span>' : ''}<button onclick="loadAdminRecords(${totalPages})">${totalPages}</button>`;
  h += `<button ${recordPage >= totalPages ? 'disabled' : ''} onclick="loadAdminRecords(${recordPage + 1})">下一页</button>`;
  h += `<span class="info">跳至</span><input id="recJump" type="number" min="1" max="${totalPages}" value="${recordPage}"><span class="info">页</span>`;
  el.innerHTML = h;
  const jump = $('recJump');
  if (jump) jump.addEventListener('keydown', (e) => {
    if (e.key !== 'Enter') return;
    loadAdminRecords(Math.min(totalPages, Math.max(1, Number(e.target.value) || 1)));
  });
}

function openRecordModal(id) {
  const r = (window.__records || []).find(x => x.id === id);
  if (!r) return toast('记录不存在，请刷新');
  $('modalCard').classList.add('wide');
  $('modalTitle').textContent = '编辑领取记录';
  $('modalBody').innerHTML = `
    <div class="field"><label>发文字号</label><input class="input" value="${esc(r.doc_number)}" disabled></div>
    <div class="field"><label>公文标题</label><input class="input" id="rTitle" value="${esc(r.title)}"></div>
    <div class="grid-2">
      <div class="field"><label>领取人</label><input class="input" id="rPerson" value="${esc(r.receive_person || '')}"></div>
      <div class="field"><label>领取时间</label><input class="input" value="${esc(r.received_at)}" disabled></div>
    </div>
    <div class="field"><label>备注</label><input class="input" id="rRemark" value="${esc(r.remark || '')}"></div>
    <div class="field"><label>归档状态</label>
      <div class="segmented" id="rArchSeg">
        <button type="button" data-v="0" class="${r.is_archived ? '' : 'on'}">未归档</button>
        <button type="button" data-v="1" class="${r.is_archived ? 'on' : ''}">已归档</button>
      </div>
    </div>
    ${r.archive_file ? `<div class="field-tip" style="margin-top:-6px">关联文件：${esc(r.archive_file)}</div>` : ''}`;
  $('modalFoot').innerHTML = `<button class="btn btn-secondary" onclick="closeModal()">取消</button><button class="btn btn-primary" onclick="saveRecord(${r.id})">保存</button>`;
  $('modal').classList.add('open');
  document.querySelectorAll('#rArchSeg button').forEach(b => {
    b.addEventListener('click', () => {
      document.querySelectorAll('#rArchSeg button').forEach(x => x.classList.toggle('on', x === b));
    });
  });
}

async function saveRecord(id) {
  const body = {
    title: $('rTitle').value.trim(),
    receive_person: $('rPerson').value.trim(),
    is_archived: document.querySelector('#rArchSeg button[data-v="1"]').classList.contains('on'),
    remark: $('rRemark').value.trim()
  };
  if (!body.title) return toast('标题不能为空');
  const r = await api('records/' + id, { method: 'PUT', body: JSON.stringify(body) });
  if (!r) return;
  closeModal(); toast('已保存'); loadAdminRecords(recordPage); loadDashboard();
}

async function delRecord(id) {
  if (!confirm('确定删除该领取记录？删除后编号不会回退。')) return;
  const r = await api('records/' + id, { method: 'DELETE' });
  if (!r) return;
  toast('已删除'); loadAdminRecords(recordPage); loadDashboard();
}

// 导出当前筛选结果
function exportRecords() {
  location.href = '/api/admin/records/export?' + recordQuery();
}

// 按归档目录自动匹配
async function autoMatch() {
  if (!confirm('将按系统设置里的归档目录重新判定所有记录：\n文件名匹配到的置为「已归档」，匹配不到的置为「未归档」。\n\n继续吗？')) return;
  const btn = document.querySelector('#tab-records .btn-match');
  if (btn) { btn.disabled = true; btn.textContent = '匹配中…'; }
  const d = await api('archive/match', { method: 'POST', body: JSON.stringify({ only_mark: false }) });
  if (btn) { btn.disabled = false; btn.textContent = '自动匹配归档'; }
  if (!d) return;
  let html = `<b>${esc(d.message)}</b>`;
  html += `<div style="color:#86868b;margin-top:4px">扫描目录：${d.dirs.map(esc).join('、')} · 条数 ${d.records} · 命中 ${d.matched} · 未命中 ${d.unmatched}</div>`;
  if (d.details && d.details.length) {
    html += '<div style="margin-top:8px;max-height:220px;overflow:auto">' +
      d.details.slice(0, 30).map(x => `· ${esc(x.doc_number)} → ${esc(x.file)}（按${esc(x.by)}）`).join('<br>') +
      (d.details.length > 30 ? `<br>… 共 ${d.details.length} 条` : '') + '</div>';
  }
  $('matchResult').innerHTML = html;
  toast('匹配完成');
  await loadAdminRecords(1);
  await loadDashboard();
}

// ---------- 导入 ----------
async function loadImportTypes() {
  if (!typesCache.length) { typesCache = await api('types') || []; }
  $('importType').innerHTML = typesCache.map(t =>
    `<option value="${t.id}">${esc(t.prefix)}…${esc(t.suffix)}（当前编号 ${t.current_num}）</option>`).join('') || '<option value="">暂无类型</option>';
}

async function doImport() {
  const typeId = Number($('importType').value);
  const text = $('importText').value;
  if (!typeId) return toast('请选择字号类型');
  if (!text.trim()) return toast('请粘贴要导入的内容');
  const d = await api('import', { method: 'POST', body: JSON.stringify({ type_id: typeId, text }) });
  if (!d) return;
  let html = `成功导入 <b>${d.inserted}</b> 条，跳过 <b>${d.skipped}</b> 条，当前编号已接续到 <b>${d.next_num}</b>。`;
  if (d.reasons && d.reasons.length) {
    html += '<div style="margin-top:8px;color:#b3261e">' + d.reasons.map(x => '· ' + esc(x)).join('<br>') + '</div>';
  }
  $('importResult').innerHTML = html;
  if (d.inserted > 0) { $('importText').value = ''; await loadTypes(); await loadImportTypes(); await loadAdminRecords(1); }
}

// ---------- 设置 ----------
async function loadSettings() {
  const d = await api('settings');
  if (!d) return;
  $('setSiteName').value = d.site_name || '';
  $('setSiteSub').value = d.site_sub || '';
  $('setArchiveDir').value = d.archive_dir || '';
  $('setUploadEnabled').checked = d.upload_enabled !== '0';
  $('setUploadRule').value = d.upload_rule || '';
  $('setUploadExt').value = d.upload_ext || '';
  $('setUploadMaxMb').value = d.upload_max_mb || '';
  $('setDzzEnabled').checked = d.dzz_enabled === '1';
  $('setDzzHost').value = d.dzz_host || '';
  $('setDzzPort').value = d.dzz_port || '';
  $('setDzzUser').value = d.dzz_user || '';
  $('setDzzPass').value = d.dzz_pass || '';
  $('setDzzDb').value = d.dzz_db || '';
  $('setDzzPrefix').value = d.dzz_prefix || '';
}

async function saveSettings() {
  const body = {
    site_name: $('setSiteName').value.trim(),
    site_sub: $('setSiteSub').value.trim(),
    archive_dir: $('setArchiveDir').value.trim(),
    upload_enabled: $('setUploadEnabled').checked ? '1' : '0',
    upload_rule: $('setUploadRule').value.trim(),
    upload_ext: $('setUploadExt').value.trim(),
    upload_max_mb: $('setUploadMaxMb').value.trim(),
    dzz_enabled: $('setDzzEnabled').checked ? '1' : '0',
    dzz_host: $('setDzzHost').value.trim(),
    dzz_port: $('setDzzPort').value.trim(),
    dzz_user: $('setDzzUser').value.trim(),
    dzz_pass: $('setDzzPass').value,
    dzz_db: $('setDzzDb').value.trim(),
    dzz_prefix: $('setDzzPrefix').value.trim()
  };
  const d = await api('settings', { method: 'PUT', body: JSON.stringify(body) });
  if (!d) return;
  toast('设置已保存');
}

async function checkArchiveDir() {
  await saveSettings();
  const d = await api('archive/check', { method: 'POST', body: '{}' });
  if (!d) return;
  $('archiveDirResult').innerHTML = (d.dirs || []).map(x =>
    `${x.ok ? '✅' : '⚠️'} <span class="mono">${esc(x.dir)}</span> — ${esc(x.msg)}`).join('<br>');
}

async function testRule() {
  const name = $('ruleTestName').value.trim();
  if (!name) return toast('请输入示例文件名');
  const d = await api('settings/test-rule', {
    method: 'POST',
    body: JSON.stringify({ rule: $('setUploadRule').value.trim(), name })
  });
  if (!d) return;
  $('ruleResult').innerHTML = d.ok
    ? '<span style="color:#1f7a48">✅ 符合命名规则</span>'
    : `<span style="color:#b3261e">⚠️ ${esc(d.message || '不符合命名规则')}</span>`;
}

async function testDzz() {
  const d = await api('settings/test-dzz', { method: 'POST', body: '{}' });
  if (!d) return;
  $('dzzResult').textContent = d.message;
}

// ---------- 通用 ----------
function closeModal() { $('modal').classList.remove('open'); }
$('modal').addEventListener('click', e => { if (e.target === $('modal')) closeModal(); });

async function logout() {
  await fetch('/api/auth/logout', { method: 'POST' });
  location.href = '/admin/login';
}

let toastTimer = null;
function toast(msg) {
  const t = $('toast');
  t.textContent = msg;
  t.classList.add('show');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => t.classList.remove('show'), 2400);
}
