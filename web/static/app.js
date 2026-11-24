// 前台：免登录取号
const API = '/api';
let page = 1;
const PAGE_SIZE = 12;
let searchTimer = null;
let archiveValue = '0';

const $ = (id) => document.getElementById(id);

document.addEventListener('DOMContentLoaded', async () => {
  await loadSite();
  await loadTypes();
  await loadRecords(1);
  await loadUploadCfg();

  $('typeSelect').addEventListener('change', onTypeChange);
  $('takeBtn').addEventListener('click', takeNumber);
  document.querySelectorAll('#archSeg button').forEach(b => {
    b.addEventListener('click', () => {
      archiveValue = b.dataset.v;
      document.querySelectorAll('#archSeg button').forEach(x => x.classList.toggle('on', x === b));
    });
  });
  $('refreshBtn').addEventListener('click', () => { loadTypes(); loadRecords(page); });
  $('archiveFilter').addEventListener('change', () => loadRecords(1));
  $('searchInput').addEventListener('input', () => {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(() => loadRecords(1), 300);
  });
});

function setArchiveValue(v) {
  archiveValue = v;
  document.querySelectorAll('#archSeg button').forEach(x => x.classList.toggle('on', x.dataset.v === v));
}

async function loadSite() {
  try {
    const r = await fetch(`${API}/public/site`);
    const d = await r.json();
    if (d.site_name) { $('siteName').textContent = d.site_name; document.title = d.site_name; }
    if (d.site_sub) $('siteSub').textContent = d.site_sub;
  } catch (e) {}
}

async function loadTypes() {
  try {
    const r = await fetch(`${API}/public/types`);
    const list = await r.json();
    const sel = $('typeSelect');
    const keep = sel.value;
    sel.innerHTML = '<option value="">请选择发文字号类型</option>';
    (list || []).forEach(t => {
      if (!t.is_active) return;
      const o = document.createElement('option');
      o.value = t.id;
      o.textContent = `${t.prefix}${t.suffix}`;
      sel.appendChild(o);
    });
    if (keep) { sel.value = keep; onTypeChange(); }
  } catch (e) {
    toast('加载字号类型失败');
  }
}

async function onTypeChange() {
  const id = $('typeSelect').value;
  const box = $('preview');
  if (!id) {
    box.innerHTML = '<span class="placeholder">选择类型后显示</span>';
    return;
  }
  try {
    const r = await fetch(`${API}/public/next?type_id=${encodeURIComponent(id)}`);
    const d = await r.json();
    if (!r.ok) { box.innerHTML = `<span class="placeholder">${esc(d.error || '不可用')}</span>`; return; }
    box.textContent = d.next_number;
  } catch (e) {
    box.innerHTML = '<span class="placeholder">获取失败</span>';
  }
}

async function takeNumber() {
  const typeId = $('typeSelect').value;
  const title = $('titleInput').value.trim();
  const receiver = $('receiverInput').value.trim();
  const remark = $('remarkInput').value.trim();
  const archived = archiveValue === '1';

  if (!typeId) return toast('请选择发文字号类型');
  if (!title) return toast('请填写公文标题');
  if (!receiver) return toast('请填写领取人');

  const btn = $('takeBtn');
  btn.disabled = true;
  btn.textContent = '提交中…';
  try {
    const r = await fetch(`${API}/public/take`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ type_id: Number(typeId), title, receiver, archived, remark })
    });
    const d = await r.json();
    if (!r.ok) { toast(d.error || '领取失败'); return; }

    $('successNum').textContent = d.doc_number;
    $('successMeta').textContent = `领取人 ${d.receiver} · ${d.archived ? '已归档' : '未归档'}`;
    $('successModal').classList.add('open');

    $('titleInput').value = '';
    $('receiverInput').value = '';
    $('remarkInput').value = '';
    setArchiveValue('0');
    $('preview').innerHTML = '<span class="placeholder">选择类型后显示</span>';
    $('typeSelect').value = '';

    await loadTypes();
    await loadRecords(1);
  } catch (e) {
    toast('网络错误，请稍后重试');
  } finally {
    btn.disabled = false;
    btn.textContent = '领取发文字号';
  }
}

async function loadRecords(p) {
  page = p || 1;
  const q = encodeURIComponent($('searchInput').value.trim());
  const arch = $('archiveFilter').value;
  const box = $('recordBody');
  try {
    const r = await fetch(`${API}/public/records?page=${page}&size=${PAGE_SIZE}&q=${q}&archived=${arch}`);
    const d = await r.json();
    const list = d.records || [];
    if (!list.length) {
      box.innerHTML = '<tr><td colspan="6" class="empty">暂无记录</td></tr>';
      $('pager').innerHTML = '';
      return;
    }
    const start = (page - 1) * PAGE_SIZE;
    box.innerHTML = list.map((it, i) => `
      <tr>
        <td style="color:#86868b">${start + i + 1}</td>
        <td class="mono">${esc(it.doc_number)}</td>
        <td>${esc(it.title)}</td>
        <td>${esc(it.receive_person || '—')}</td>
        <td>${it.is_archived ? '<span class="badge badge-green">已归档</span>' : '<span class="badge badge-gray">未归档</span>'}</td>
        <td style="color:#86868b;font-variant-numeric:tabular-nums">${esc(it.received_at)}</td>
      </tr>`).join('');
    renderPager(d.total, d.pages);
  } catch (e) {
    box.innerHTML = '<tr><td colspan="6" class="empty">加载失败</td></tr>';
  }
}

function renderPager(total, pageCount) {
  const el = $('pager');
  if (!total) { el.innerHTML = ''; return; }
  const pages = pageCount || Math.max(1, Math.ceil(total / PAGE_SIZE));
  let h = `<span class="info">共 ${total} 条 · 第 ${page} / ${pages} 页</span>`;
  h += `<button ${page <= 1 ? 'disabled' : ''} onclick="loadRecords(${page - 1})">上一页</button>`;
  const from = Math.max(1, page - 2), to = Math.min(pages, page + 2);
  if (from > 1) h += `<button onclick="loadRecords(1)">1</button>${from > 2 ? '<span class="info">…</span>' : ''}`;
  for (let i = from; i <= to; i++) h += `<button class="${i === page ? 'on' : ''}" onclick="loadRecords(${i})">${i}</button>`;
  if (to < pages) h += `${to < pages - 1 ? '<span class="info">…</span>' : ''}<button onclick="loadRecords(${pages})">${pages}</button>`;
  h += `<button ${page >= pages ? 'disabled' : ''} onclick="loadRecords(${page + 1})">下一页</button>`;
  h += `<span class="info">跳至</span><input id="pagerJump" type="number" min="1" max="${pages}" value="${page}"><span class="info">页</span>`;
  el.innerHTML = h;
  const jump = $('pagerJump');
  if (jump) {
    jump.addEventListener('keydown', (e) => {
      if (e.key !== 'Enter') return;
      const v = Math.min(pages, Math.max(1, Number(e.target.value) || 1));
      loadRecords(v);
    });
  }
}

function closeSuccess() { $('successModal').classList.remove('open'); }

// ---------- 上传归档文件 ----------
let uploadCfg = null;
let uploadBound = false;

async function loadUploadCfg() {
  try {
    const r = await fetch(`${API}/public/upload-config`);
    const d = await r.json();
    if (!d || !d.enabled) return;
    uploadCfg = d;
    $('uploadCard').style.display = '';
    if (d.rule) $('uploadRuleTip').textContent = '命名规则：' + d.rule;
    if (d.dir) $('uploadDirTip').textContent = '保存到 ' + d.dir + (d.max_mb ? ` · 单文件 ≤ ${d.max_mb} MB` : '');
    bindUpload();
  } catch (e) {}
}

function bindUpload() {
  if (uploadBound) return;
  uploadBound = true;
  const dz = $('dropzone'), input = $('fileInput');
  dz.addEventListener('click', () => input.click());
  input.addEventListener('change', () => {
    if (input.files.length) uploadFiles(input.files);
    input.value = '';
  });
  ['dragenter', 'dragover'].forEach(ev => dz.addEventListener(ev, (e) => {
    e.preventDefault(); dz.classList.add('over');
  }));
  ['dragleave', 'drop'].forEach(ev => dz.addEventListener(ev, (e) => {
    e.preventDefault(); dz.classList.remove('over');
  }));
  dz.addEventListener('drop', (e) => {
    if (e.dataTransfer.files.length) uploadFiles(e.dataTransfer.files);
  });
}

async function uploadFiles(files) {
  const list = $('uploadList');
  list.innerHTML = `<div class="upload-item"><div class="name">正在上传 ${files.length} 个文件…</div></div>`;
  const fd = new FormData();
  [...files].forEach(f => fd.append('file', f));
  try {
    const r = await fetch(`${API}/public/upload`, { method: 'POST', body: fd });
    const d = await r.json();
    if (!r.ok) {
      list.innerHTML = `<div class="upload-item"><div class="name warn">${esc(d.error || '上传失败')}</div></div>`;
      return;
    }
    list.innerHTML = (d.items || []).map(renderUploadItem).join('');
    const archived = (d.items || []).filter(x => x.archived).length;
    toast(archived ? `已归档 ${archived} 个文件` : '上传完成，未匹配到领取记录');
    await loadRecords(1);
    await loadTypes();
  } catch (e) {
    list.innerHTML = '<div class="upload-item"><div class="name warn">网络错误，上传失败</div></div>';
  }
}

function renderUploadItem(it) {
  let badge = '<span class="badge badge-gray">未归档</span>';
  if (!it.ok) badge = '<span class="badge badge-red">失败</span>';
  else if (it.archived) badge = '<span class="badge badge-green">已归档</span>';
  else if (!it.rule_ok) badge = '<span class="badge badge-amber">命名不符</span>';
  const doc = it.doc_number ? `<span class="mono">${esc(it.doc_number)}</span> · ` : '';
  return `<div class="upload-item">
    <div class="name">${esc(it.file)}
      <div class="msg">${doc}${esc(it.message || '')}</div>
    </div>
    ${badge}
  </div>`;
}

function esc(s) {
  return String(s == null ? '' : s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

let toastTimer = null;
function toast(msg) {
  const t = $('toast');
  t.textContent = msg;
  t.classList.add('show');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => t.classList.remove('show'), 2400);
}
