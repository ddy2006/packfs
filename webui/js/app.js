// =========================================================================
// packfs WebUI — dataset SQLite + PostgreSQL API
// =========================================================================

(function () {
  'use strict';

  const $ = (sel, ctx) => (ctx || document).querySelector(sel);
  const $$ = (sel, ctx) => [...(ctx || document).querySelectorAll(sel)];

  const API = {
    async get(path) {
      const res = await fetch(path);
      return res.json();
    },
    async post(path, body) {
      const res = await fetch(path, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body || {}),
      });
      return res.json();
    },
  };

  const state = {
    datasets: [],
    arcsets: [],
    shards: [],
    selectedDatasetUID: '',
    online: false,
    postgres: false,
  };

  function toast(msg) {
    const el = document.createElement('div');
    el.className = 'toast';
    el.textContent = msg;
    document.body.appendChild(el);
    setTimeout(() => el.remove(), 2500);
  }

  function fmtSize(bytes) {
    if (!bytes || bytes < 0) return '0 B';
    if (bytes < 1024) return bytes + ' B';
    if (bytes < 1048576) return (bytes / 1024).toFixed(1) + ' KB';
    if (bytes < 1073741824) return (bytes / 1048576).toFixed(1) + ' MB';
    return (bytes / 1073741824).toFixed(2) + ' GB';
  }

  function esc(s) {
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
  }

  function shortUID(uid) {
    return uid ? uid.slice(0, 8) : '-';
  }

  function datasetByUID(uid) {
    return state.datasets.find(d => d.uid === uid);
  }

  function arcsetByID(id) {
    return state.arcsets.find(a => a.id === id);
  }

  function setCliCmd(cmd) {
    $('#cli-cmd').textContent = cmd;
  }
  $('#btn-copy').addEventListener('click', () => {
    navigator.clipboard.writeText($('#cli-cmd').textContent)
      .then(() => toast('命令已复制')).catch(() => {});
  });

  async function checkHealth() {
    try {
      const res = await fetch('/api/health');
      const data = await res.json();
      if (data.ok) {
        state.online = true;
        state.postgres = !!data.data.postgres;
        $('#mode-badge').innerHTML = state.postgres
          ? '<span class="dot dot-live"></span> 真实模式 · PG'
          : '<span class="dot dot-live"></span> 真实模式 · 无 PG';
        return true;
      }
    } catch (e) { /* offline */ }
    state.online = false;
    $('#mode-badge').innerHTML = '<span class="dot dot-sim" style="background:#e74c3c"></span> API 离线';
    return false;
  }

  function switchTab(tabName) {
    $$('.tab-content').forEach(t => t.classList.remove('active'));
    $$('.nav-btn').forEach(b => b.classList.remove('active'));
    const tab = $('#tab-' + tabName);
    if (tab) tab.classList.add('active');
    const btn = document.querySelector(`[data-tab="${tabName}"]`);
    if (btn) btn.classList.add('active');

    if (tabName === 'shard') {
      refreshShardTabSelects();
      loadDatasetShards($('#sh-dataset-id').value);
    }
    if (tabName === 'ec') refreshECTabSelects();
    if (tabName === 'arcset') refreshArcsetTabSelects();
  }
  $('#nav').addEventListener('click', (e) => {
    const btn = e.target.closest('.nav-btn');
    if (btn) switchTab(btn.dataset.tab);
  });
  $('#pipeline').addEventListener('click', (e) => {
    const step = e.target.closest('.pipe-step');
    if (step && step.dataset.tab) switchTab(step.dataset.tab);
  });

  function updatePipeline() {
    const datasets = state.datasets;
    const arcsets = state.arcsets;
    const totalShards = datasets.reduce((n, d) => n + (d.shard_count || 0), 0);
    const completed = arcsets.filter(a => a.status === 'completed');
    const ec = completed.find(a => (a.metadata && a.metadata.ec && a.metadata.ec !== '0'));

    const setStep = (step, done, text) => {
      const el = document.querySelector(`.pipe-step[data-step="${step}"]`);
      if (!el) return;
      el.classList.remove('pending', 'done');
      el.classList.add(done ? 'done' : 'pending');
      if (text) el.querySelector('.pipe-path').textContent = text;
    };

    setStep('source', datasets.length > 0, datasets.length ? datasets[0].current_path : '--');
    setStep('dataset', datasets.length > 0,
      datasets.length ? `${datasets[0].file_count || 0} 文件 · ${datasets.length} 个 dataset` : '扫描文件，写入独立 SQLite');
    setStep('shard', totalShards > 0, totalShards ? `${totalShards} 个 DATA shard` : '合并小文件 → 大文件');
    setStep('arcset', arcsets.length > 0, arcsets.length ? `${arcsets.length} 个 arcset` : '纠删码容器');
    setStep('ec', completed.length > 0,
      completed.length ? `RS(${ec ? ec.metadata.ec : '-'}) · ${completed.length} 个已完成` : 'RS(k+m) 冗余保护');
    setStep('downstream', completed.length > 0,
      completed.length ? 'arcset 已可移交' : '移交下游存储');

    const arrows = $$('.pipe-arrow');
    const states = $$('.pipe-step').map(s => s.classList.contains('done') ? 'done' : 'pending');
    arrows.forEach((arrow, i) => {
      arrow.classList.toggle('done', states[i] === 'done' && states[i + 1] === 'done');
    });
  }

  async function refreshAllData() {
    try {
      const [dsRes, asRes] = await Promise.all([
        API.get('/api/datasets'),
        API.get('/api/arcsets'),
      ]);
      state.datasets = dsRes.ok ? dsRes.data : [];
      state.arcsets = asRes.ok ? asRes.data : [];
      if (!asRes.ok && state.postgres) {
        toast(asRes.error || 'arcset 列表读取失败');
      }
    } catch (e) {
      console.error('refresh data failed:', e);
      state.datasets = [];
      state.arcsets = [];
    }
  }

  function refreshShardTabSelects() {
    const sel = $('#sh-dataset-id');
    const current = state.selectedDatasetUID || (state.datasets[0] && state.datasets[0].uid) || '';
    sel.innerHTML = state.datasets.map(d =>
      `<option value="${esc(d.uid)}">[${esc(shortUID(d.uid))}] ${esc(d.name)} (${d.shard_count || 0} shard)</option>`
    ).join('') || '<option value="">-- 请先创建 Dataset --</option>';
    if (current) sel.value = current;
    updateShardOutputDir();
    const ds = datasetByUID(sel.value);
    $('#btn-make-shard').disabled = !ds || ds.status !== 'building';
  }

  function updateShardOutputDir() {
    const uid = $('#sh-dataset-id').value;
    const ds = datasetByUID(uid);
    const root = ds && ds.metadata ? (ds.metadata.shard_root || ds.current_path) : '';
    $('#sh-output-dir').value = root || '';
  }

  function refreshECTabSelects() {
    const sel = $('#ec-arcset-id');
    const current = sel.value;
    sel.innerHTML = state.arcsets.map(a =>
      `<option value="${a.id}">[${a.id}] ${esc(a.name)} (${esc((a.metadata && a.metadata.ec) || '-')})</option>`
    ).join('') || '<option value="">-- 请先创建 Arcset --</option>';
    if (current) sel.value = current;
    updateECInfo();
  }

  function updateECInfo() {
    const a = arcsetByID(parseInt($('#ec-arcset-id').value));
    $('#ec-config-info').value = a && a.metadata ? (a.metadata.ec || '-') : '-';
  }

  function refreshArcsetTabSelects() {
    const asSel = $('#ap-arcset-id');
    const asCurrent = asSel.value;
    asSel.innerHTML = state.arcsets.map(a =>
      `<option value="${a.id}">[${a.id}] ${esc(a.name)}</option>`
    ).join('') || '<option value="">-- 无 Arcset --</option>';
    if (asCurrent) asSel.value = asCurrent;

    const dsSel = $('#ap-dataset-id');
    const dsCurrent = dsSel.value;
    dsSel.innerHTML = state.datasets.map(d =>
      `<option value="${esc(d.uid)}">[${esc(shortUID(d.uid))}] ${esc(d.name)} (${esc(d.status)})</option>`
    ).join('') || '<option value="">-- 无 Dataset --</option>';
    if (dsCurrent) dsSel.value = dsCurrent;
  }

  function renderDSTable() {
    const tbody = $('#ds-tbody');
    if (state.datasets.length === 0) {
      tbody.innerHTML = '<tr class="placeholder"><td colspan="9">暂无 Dataset</td></tr>';
      return;
    }
    tbody.innerHTML = state.datasets.map(d => {
      const fm = d.metadata || {};
      const canMark = d.status === 'building';
      return `
        <tr>
          <td title="${esc(d.uid)}"><code>${esc(shortUID(d.uid))}</code></td>
          <td>${esc(d.name)}</td>
          <td>${d.file_count || fm.num_files || '-'}</td>
          <td>${fmtSize(d.total_bytes || fm.total_bytes || 0)}</td>
          <td>${d.shard_count || 0}</td>
          <td>${esc(fm.format || '-')}</td>
          <td>${esc(fm.compress || '无')}</td>
          <td><span class="badge badge-${esc(d.status)}">${esc(d.status)}</span></td>
          <td>
            <button class="btn btn-secondary btn-sm" data-action="shards" data-uid="${esc(d.uid)}">查看 Shard</button>
            <button class="btn btn-secondary btn-sm" data-action="mark-built" data-uid="${esc(d.uid)}" ${canMark ? '' : 'disabled'}>Mark Built</button>
            <button class="btn btn-secondary btn-sm" data-action="unpack" data-uid="${esc(d.uid)}">解包</button>
          </td>
        </tr>`;
    }).join('');
  }

  function renderShardTable() {
    const tbody = $('#sh-tbody');
    if (!state.shards.length) {
      tbody.innerHTML = '<tr class="placeholder"><td colspan="6">暂无 Shard</td></tr>';
      return;
    }
    tbody.innerHTML = state.shards.map(s => `
      <tr>
        <td>${s.id}</td>
        <td><code>${esc(s.file_path || '-')}</code></td>
        <td><span class="badge badge-${esc((s.type || 'data').toLowerCase())}">${esc(s.type || 'DATA')}</span></td>
        <td>${fmtSize(s.file_size || 0)}</td>
        <td>${s.segment_count || 0}</td>
        <td><span class="muted">${esc(s.sha256 || '').slice(0, 12)}</span></td>
      </tr>`).join('');
  }

  function renderASTable() {
    const tbody = $('#as-tbody');
    if (state.arcsets.length === 0) {
      tbody.innerHTML = '<tr class="placeholder"><td colspan="9">暂无 Arcset</td></tr>';
      return;
    }
    tbody.innerHTML = state.arcsets.map(a => {
      const fm = a.metadata || {};
      return `
        <tr>
          <td><strong>${a.id}</strong></td>
          <td>${esc(a.name)}</td>
          <td>${esc(fm.ec || '-')}</td>
          <td>${esc(a.status)}/${esc(a.build_phase || 'none')}</td>
          <td>${esc(a.health || '-')}${a.parity_degraded ? ' · degraded' : ''}</td>
          <td>${a.dataset_count || 0}</td>
          <td>${a.shard_count || 0}</td>
          <td>${fmtSize(a.used_bytes || 0)} / ${fmtSize(a.raw_capacity || 0)}</td>
          <td><button class="btn btn-secondary btn-sm" data-action="ec-plan" data-asid="${a.id}">EC 布局</button></td>
        </tr>`;
    }).join('');
  }

  async function loadDatasetShards(uid) {
    state.selectedDatasetUID = uid;
    state.shards = [];
    if (!uid) {
      renderShardTable();
      return;
    }
    const res = await API.get('/api/datasets/' + encodeURIComponent(uid) + '/shards');
    if (res.ok) {
      state.shards = res.data || [];
    } else {
      toast(res.error || '读取 shard 失败');
    }
    renderShardTable();
  }

  // ====== Dataset create ======
  $('#ds-create-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const rootDir = $('#ds-root-dir').value.trim();
    if (!rootDir) { toast('请填写源目录'); return; }
    const body = {
      root_dir: rootDir,
      name: $('#ds-name').value.trim(),
      target_root: $('#ds-target-root').value.trim(),
      format: $('#ds-format').value,
      compress: $('#ds-compress').value,
      shard_max_bytes: parseInt($('#ds-shard-max').value) || 0,
      gen_only: $('#ds-gen-only').checked,
    };
    const cmd = `packfs dataset create --root-dir=${rootDir} --db-dir=<serve-db-dir> --target-root=${body.target_root || rootDir} --format=${body.format} --compress=${body.compress} --shard-max-bytes=${body.shard_max_bytes}${body.gen_only ? ' --gen-only' : ''}`;
    $('#ds-cmd-hint').textContent = cmd;
    $('#ds-cmd-hint').classList.add('visible');
    setCliCmd(cmd);

    const res = await API.post('/api/datasets', body);
    if (res.ok) {
      state.selectedDatasetUID = res.data.uid;
      await refreshAllData();
      renderDSTable();
      refreshShardTabSelects();
      refreshArcsetTabSelects();
      updatePipeline();
      toast(`Dataset ${res.data.name} 创建完成，${res.data.shard_count || 0} 个 shard`);
    } else {
      toast('创建失败: ' + (res.error || '未知错误'));
    }
  });

  // ====== Arcset create ======
  $('#as-create-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const name = $('#as-name').value.trim();
    const targetRoot = $('#as-target-root').value.trim();
    if (!name || !targetRoot) { toast('请填写名称和目标目录'); return; }
    const body = {
      name,
      target_root: targetRoot,
      ec: $('#as-ec').value,
      groups: parseInt($('#as-groups').value) || 1,
      layout: $('#as-layout').value,
      partition_capacity: $('#as-partition-capacity').value.trim(),
      shard_max_bytes: parseInt($('#as-shard-max').value) || 0,
    };
    const cmd = `packfs arcset create --name=${name} --target-root=${targetRoot} --ec=${body.ec} --groups=${body.groups} --layout=${body.layout} --partition-capacity=${body.partition_capacity}`;
    $('#as-cmd-hint').textContent = cmd;
    $('#as-cmd-hint').classList.add('visible');
    setCliCmd(cmd);

    const res = await API.post('/api/arcsets', body);
    if (res.ok) {
      await refreshAllData();
      renderASTable();
      refreshECTabSelects();
      refreshArcsetTabSelects();
      updatePipeline();
      toast(`Arcset ${name} 创建完成`);
    } else {
      toast('创建失败: ' + (res.error || '未知错误'));
    }
  });

  // ====== Table actions ======
  document.querySelector('main').addEventListener('click', async (e) => {
    const btn = e.target.closest('[data-action]');
    if (!btn) return;
    const action = btn.dataset.action;

    if (action === 'shards') {
      const uid = btn.dataset.uid;
      switchTab('shard');
      $('#sh-dataset-id').value = uid;
      updateShardOutputDir();
      await loadDatasetShards(uid);
    } else if (action === 'mark-built') {
      const uid = btn.dataset.uid;
      const res = await API.post('/api/datasets/' + encodeURIComponent(uid) + '/mark-built');
      if (res.ok) {
        await refreshAllData();
        renderDSTable();
        updatePipeline();
        toast(`Dataset ${shortUID(uid)} 已标记 built`);
      } else {
        toast('Mark Built 失败: ' + (res.error || '未知错误'));
      }
    } else if (action === 'unpack') {
      const uid = btn.dataset.uid;
      const target = prompt('解包目标目录:', '/tmp/packfs-unpack-' + shortUID(uid));
      if (!target) return;
      const res = await API.post('/api/datasets/' + encodeURIComponent(uid) + '/unpack',
        { target_root: target });
      if (res.ok) {
        toast(`解包完成: ${res.data.files} 个文件`);
      } else {
        toast('解包失败: ' + (res.error || '未知错误'));
      }
    } else if (action === 'ec-plan') {
      const asId = parseInt(btn.dataset.asid);
      switchTab('ec');
      $('#ec-arcset-id').value = String(asId);
      updateECInfo();
      await doECPreview();
    }
  });

  // ====== Shard tab ======
  $('#sh-dataset-id').addEventListener('change', async () => {
    updateShardOutputDir();
    const ds = datasetByUID($('#sh-dataset-id').value);
    $('#btn-make-shard').disabled = !ds || ds.status !== 'building';
    await loadDatasetShards($('#sh-dataset-id').value);
  });

  $('#btn-preview-shard').addEventListener('click', async () => {
    const uid = $('#sh-dataset-id').value;
    if (!uid) { toast('请选择一个 Dataset'); return; }
    await loadDatasetShards(uid);
    const ds = datasetByUID(uid);
    const card = $('#shard-preview-card');
    const preview = $('#shard-preview');
    card.style.display = 'block';
    if (!state.shards.length) {
      preview.innerHTML = '<div class="empty-state"><div class="icon">📭</div>暂无 shard</div>';
      return;
    }
    preview.innerHTML = state.shards.map(sh => {
      const segs = sh.segments || [];
      return `
        <div class="shard-group">
          <div class="shard-group-header">
            <span class="shard-group-name">${esc(sh.file_path)}</span>
            <span class="shard-group-meta">${sh.segment_count || 0} 个 segment · ${fmtSize(sh.file_size || 0)}</span>
          </div>
          <div class="shard-group-files">
            ${segs.slice(0, 20).map(s => `<span class="shard-file">${esc((s.file_path || '?').split('/').pop())}</span>`).join('')}
            ${segs.length > 20 ? `<span class="shard-file" style="color:var(--accent)">... 还有 ${segs.length - 20} 个文件</span>` : ''}
          </div>
        </div>`;
    }).join('');
    setCliCmd(`packfs dataset unpack --dataset-db=${ds.sqlite_path} --target-root=<target>`);
    toast(`共 ${state.shards.length} 个 shard`);
  });

  $('#btn-make-shard').addEventListener('click', async () => {
    const uid = $('#sh-dataset-id').value;
    if (!uid) { toast('请选择一个 Dataset'); return; }
    const res = await API.post('/api/datasets/' + encodeURIComponent(uid) + '/make-shards');
    if (res.ok) {
      await refreshAllData();
      renderDSTable();
      refreshShardTabSelects();
      await loadDatasetShards(uid);
      updatePipeline();
      toast(`生成完成，共 ${res.data.shard_count || 0} 个 shard`);
    } else {
      toast('生成失败: ' + (res.error || '未知错误'));
    }
  });

  // ====== EC tab ======
  async function doECPreview() {
    const asId = parseInt($('#ec-arcset-id').value);
    if (!asId) { toast('请选择一个 Arcset'); return; }
    updateECInfo();
    const res = await API.get('/api/arcsets/' + asId + '/plan');
    if (!res.ok) { toast(res.error || 'EC plan failed'); return; }
    const result = res.data;

    const card = $('#ec-preview-card');
    const preview = $('#ec-preview');
    card.style.display = 'block';
    $('#ec-legend-k').textContent = result.k;
    $('#ec-legend-m').textContent = result.m;

    preview.innerHTML = (result.stripes || []).map(stripe => {
      const dataBlocks = (stripe.data || []).map(d =>
        `<div class="stripe-block data" data-shid="${d.id}" title="${esc(d.file_path || '')}">D${d.position}<span class="tooltip">${esc((d.file_path || '').replace(/^.*\//, ''))}</span></div>`
      ).join('');
      const ecBlocks = (stripe.ec || []).map(e => {
        if (e.type === 'PAD') {
          return `<div class="stripe-block pad">—<span class="tooltip">${esc(e.name)}</span></div>`;
        }
        return `<div class="stripe-block ec">E${e.position}<span class="tooltip">${esc(e.name)}</span></div>`;
      }).join('');
      const dataCount = (stripe.data || []).length;
      const ecCount = (stripe.ec || []).filter(e => e.type === 'EC').length;
      const padCount = (stripe.ec || []).filter(e => e.type === 'PAD').length;
      const summary = [`<span class="dot-legend" style="background:#d6e8ff;border:1px solid #3b82f6"></span> ${dataCount} data`];
      if (ecCount) summary.push(`<span class="dot-legend" style="background:#fce4ec;border:1px solid #e91e63"></span> ${ecCount} EC`);
      if (padCount) summary.push(`<span class="dot-legend" style="background:#f5f5f5;border:1px dashed #ccc"></span> ${padCount} PAD`);
      return `
        <div class="stripe">
          <span class="stripe-label">Stripe ${stripe.seq} (group ${stripe.group})</span>
          <div class="stripe-blocks">${dataBlocks}${ecBlocks}</div>
          <span class="stripe-summary">${summary.join(' ')}</span>
        </div>`;
    }).join('') || '<div class="empty-state"><div class="icon">📭</div>该 arcset 还没有 shard</div>';

    const allDataShards = (result.stripes || []).flatMap(s => s.data || []);
    $('#ec-lost-shard').innerHTML = allDataShards.map(d =>
      `<option value="${esc(d.file_path)}">${esc(d.file_path)}</option>`
    ).join('') || '<option value="">-- 无 DATA shard --</option>';
    $('#ec-recover-card').style.display = 'block';
    setCliCmd(`packfs arcset make-ec --id=${asId}`);
    toast(`${(result.stripes || []).length} 个 stripe，k=${result.k}，m=${result.m}`);
  }
  $('#btn-preview-ec').addEventListener('click', doECPreview);
  $('#ec-arcset-id').addEventListener('change', updateECInfo);

  $('#btn-build-data').addEventListener('click', async () => {
    const asId = parseInt($('#ec-arcset-id').value);
    if (!asId) { toast('请先选择 Arcset'); return; }
    const res = await API.post('/api/arcsets/' + asId + '/build-data');
    if (res.ok) {
      await refreshAllData();
      renderASTable();
      await doECPreview();
      toast(`布局完成，${res.data.built || 0} 个 shard`);
    } else {
      toast('布局失败: ' + (res.error || '未知错误'));
    }
  });

  $('#btn-make-ec').addEventListener('click', async () => {
    const asId = parseInt($('#ec-arcset-id').value);
    if (!asId) { toast('请先选择 Arcset'); return; }
    const res = await API.post('/api/arcsets/' + asId + '/make-ec', { parallel: 1 });
    if (res.ok) {
      await refreshAllData();
      renderASTable();
      await doECPreview();
      toast('EC 编码完成');
    } else {
      toast('EC 编码失败: ' + (res.error || '未知错误'));
    }
  });

  $('#btn-validate-ec').addEventListener('click', async () => {
    const asId = parseInt($('#ec-arcset-id').value);
    if (!asId) { toast('请先选择 Arcset'); return; }
    const res = await API.post('/api/arcsets/' + asId + '/validate', { stage: 'all' });
    if (res.ok) {
      await refreshAllData();
      renderASTable();
      toast('校验通过');
    } else {
      toast('校验失败: ' + (res.error || '未知错误'));
    }
  });

  $('#btn-export-ec').addEventListener('click', async () => {
    const asId = parseInt($('#ec-arcset-id').value);
    if (!asId) { toast('请先选择 Arcset'); return; }
    const res = await API.post('/api/arcsets/' + asId + '/export', {});
    if (res.ok) {
      toast('manifest: ' + (res.data.manifest || ''));
    } else {
      toast('导出失败: ' + (res.error || '未知错误'));
    }
  });

  $('#btn-expand-ec').addEventListener('click', async () => {
    const asId = parseInt($('#ec-arcset-id').value);
    if (!asId) { toast('请先选择 Arcset'); return; }
    const n = parseInt(prompt('增加 group 数量:', '1') || '0');
    if (!n || n <= 0) return;
    const res = await API.post('/api/arcsets/' + asId + '/expand', { add_groups: n });
    if (res.ok) {
      await refreshAllData();
      renderASTable();
      toast(`扩容完成，groups=${res.data.group_count}`);
    } else {
      toast('扩容失败: ' + (res.error || '未知错误'));
    }
  });

  $('#btn-recover').addEventListener('click', async () => {
    const asId = parseInt($('#ec-arcset-id').value);
    const shardFile = $('#ec-lost-shard').value;
    if (!asId || !shardFile) { toast('请选择 arcset 和丢失 shard'); return; }
    $$('.stripe-block.lost').forEach(b => b.classList.remove('lost'));
    $$('.stripe-block.recovered').forEach(b => b.classList.remove('recovered'));
    const lostBlocks = $$('.stripe-block.data').filter(b => b.title === shardFile);
    lostBlocks.forEach(b => b.classList.add('lost'));

    const res = await API.post('/api/arcsets/' + asId + '/recover',
      { shard_file: shardFile, target: 'all' });
    if (res.ok) {
      lostBlocks.forEach(b => {
        setTimeout(() => { b.classList.remove('lost'); b.classList.add('recovered'); }, 800);
      });
      $('#recover-result').innerHTML = `
        <div class="recover-box">
          <h4>✓ 恢复成功</h4>
          <p>${esc(shardFile)}</p>
        </div>`;
      setCliCmd(`packfs arcset recover --id=${asId} --shard-file=${shardFile}`);
      toast('恢复完成');
    } else {
      toast('恢复失败: ' + (res.error || '未知错误'));
    }
  });

  // ====== Arcset append ======
  $('#btn-append').addEventListener('click', async () => {
    const asId = parseInt($('#ap-arcset-id').value);
    const uid = $('#ap-dataset-id').value;
    if (!asId || !uid) { toast('请选择 Arcset 和 Dataset'); return; }
    const res = await API.post('/api/arcsets/' + asId + '/append', { dataset_uid: uid });
    if (res.ok) {
      await refreshAllData();
      renderASTable();
      refreshECTabSelects();
      setCliCmd(`packfs arcset append --id=${asId} --dataset-db=<db>`);
      toast(`Append 完成，key=${res.data.dataset_key}`);
      updatePipeline();
    } else {
      toast('Append 失败: ' + (res.error || '未知错误'));
    }
  });

  // ====== Simulation ======
  $('#btn-sim-preset').addEventListener('click', () => {
    $('#sim-name').value = '1177938016';
    $('#sim-start-ts').value = '1177940019';
    $('#sim-end-ts').value = '1177940098';
    $('#sim-ch-start').value = '133';
    $('#sim-ch-end').value = '156';
    $('#sim-file-bytes').value = '10240';
    $('#sim-output-root').value = './data/dat';
    toast('已填入 SKA 天体物理数据集预设');
  });

  $('#sim-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const name = $('#sim-name').value.trim();
    const outputRoot = $('#sim-output-root').value.trim();
    if (!name || !outputRoot) { toast('请填写名称和输出目录'); return; }
    const body = {
      name,
      output_root: outputRoot,
      start_ts: parseInt($('#sim-start-ts').value) || 0,
      end_ts: parseInt($('#sim-end-ts').value) || 0,
      ch_start: parseInt($('#sim-ch-start').value) || 0,
      ch_end: parseInt($('#sim-ch-end').value) || 0,
      file_bytes: parseInt($('#sim-file-bytes').value) || 1024,
    };
    const hint = $('#sim-cmd-hint');
    const result = $('#sim-result');
    hint.textContent = `生成 ${body.name}: ch[${body.ch_start}..${body.ch_end}] × ts[${body.start_ts}..${body.end_ts}]`;
    hint.classList.add('visible');
    const res = await API.post('/api/simulate', body);
    if (res.ok) {
      const d = res.data;
      result.innerHTML = `
        <div class="result-box" style="background:#e8f5e9;border:1px solid #4caf50;border-radius:8px;padding:1rem;margin-top:.5rem">
          <strong>✓ 生成完成</strong>
          <p style="margin:.25rem 0"><code>${esc(d.output_dir)}</code></p>
          <p style="margin:.25rem 0;color:#555">${d.file_count} 个文件 · ${fmtSize(d.total_bytes)}</p>
          <button class="btn btn-primary btn-sm" id="btn-sim-to-ds" style="margin-top:.5rem">📦 用此目录创建 Dataset</button>
        </div>`;
      $('#btn-sim-to-ds').addEventListener('click', () => {
        switchTab('dataset');
        $('#ds-root-dir').value = d.output_dir;
        $('#ds-name').value = name;
        toast('已切换到 Dataset 页签，源目录已填入');
      });
    } else {
      result.innerHTML = `<div style="color:#c0392b;margin-top:.5rem">生成失败: ${esc(res.error || '未知错误')}</div>`;
    }
  });

  // ====== Init ======
  async function init() {
    const online = await checkHealth();
    if (online) await refreshAllData();
    renderDSTable();
    renderShardTable();
    renderASTable();
    refreshShardTabSelects();
    refreshECTabSelects();
    refreshArcsetTabSelects();
    updatePipeline();
    if (!online) {
      setCliCmd('API 离线 — 请启动 packfs serve');
    } else if (!state.datasets.length) {
      setCliCmd('packfs dataset create --root-dir=/data --db-dir=... --target-root=...');
    }
  }

  init();
})();
