// =========================================================================
// PackFS Web UI — Main App (real API mode)
// =========================================================================

(function () {
  'use strict';

  // DOM helpers
  const $ = (sel, ctx) => (ctx || document).querySelector(sel);
  const $$ = (sel, ctx) => [...(ctx || document).querySelectorAll(sel)];

  // ====== API client ======
  const API = {
    async get(path) {
      const res = await fetch(path);
      return res.json();
    },
    async post(path, body) {
      const res = await fetch(path, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
      return res.json();
    },
  };

  // ====== Global state ======
  let state = {
    datasets: [],
    arcsets: [],
    shards: [],
    loading: false,
  };

  // ====== Toast ======
  function toast(msg) {
    const el = document.createElement('div');
    el.className = 'toast';
    el.textContent = msg;
    document.body.appendChild(el);
    setTimeout(() => el.remove(), 2500);
  }

  // ====== Formatting ======
  function fmtSize(bytes) {
    if (!bytes || bytes < 0) return '0 B';
    if (bytes < 1024) return bytes + ' B';
    if (bytes < 1048576) return (bytes / 1024).toFixed(1) + ' KB';
    if (bytes < 1073741824) return (bytes / 1048576).toFixed(1) + ' MB';
    return (bytes / 1073741824).toFixed(2) + ' GB';
  }

  // ====== CLI Command Bar ======
  function setCliCmd(cmd) {
    $('#cli-cmd').textContent = cmd;
  }
  $('#btn-copy').addEventListener('click', () => {
    navigator.clipboard.writeText($('#cli-cmd').textContent).then(() => toast('命令已复制')).catch(() => {});
  });

  // ====== Header mode badge ======
  async function checkHealth() {
    try {
      const res = await fetch('/api/health');
      const data = await res.json();
      if (data.ok) {
        $('#mode-badge').innerHTML = '<span class="dot dot-live"></span> 真实模式';
        return true;
      }
    } catch (e) { /* offline */ }
    $('#mode-badge').innerHTML = '<span class="dot dot-sim" style="background:#e74c3c"></span> API 离线';
    return false;
  }

  // ====== Tab Navigation ======
  function switchTab(tabName) {
    $$('.tab-content').forEach(t => t.classList.remove('active'));
    $$('.nav-btn').forEach(b => b.classList.remove('active'));

    const tab = $('#tab-' + tabName);
    if (tab) tab.classList.add('active');
    const btn = document.querySelector(`[data-tab="${tabName}"]`);
    if (btn) btn.classList.add('active');

    if (tabName === 'shard') refreshShardTabSelects();
    if (tabName === 'ec') refreshECTabSelects();
    if (tabName === 'arcset') refreshArcsetTabSelects();
  }
  $('#nav').addEventListener('click', (e) => {
    const btn = e.target.closest('.nav-btn');
    if (!btn) return;
    switchTab(btn.dataset.tab);
  });

  // ====== Pipeline click navigation ======
  $('#pipeline').addEventListener('click', (e) => {
    const step = e.target.closest('.pipe-step');
    if (!step) return;
    const tabName = step.dataset.tab;
    if (tabName) switchTab(tabName);
  });

  // ====== Pipeline state update ======
  function updatePipeline() {
    const ds = state.datasets;
    const as = state.arcsets;
    const sh = state.shards;
    const hasDS = ds.length > 0;
    const hasShard = sh.length > 0;
    const hasArcset = as.length > 0;
    const ecShards = sh.filter(s => s.type === 'EC');
    const hasEC = ecShards.length > 0;
    const hasTape = as.some(a => a.status === 'taped' || a.status === 'ready');

    const arrows = $$('.pipe-arrow');

    // 源目录
    const srcStep = document.querySelector('.pipe-step[data-step="source"]');
    if (srcStep) {
      srcStep.classList.remove('pending', 'done', 'active-step');
      if (hasDS) {
        srcStep.classList.add('done');
        const d = ds[0];
        $('#pipe-src').textContent = d.current_path || d.rootDir || '--';
      } else {
        srcStep.classList.add('pending');
        $('#pipe-src').textContent = '--';
      }
    }

    // Dataset
    const dsStep = document.querySelector('.pipe-step[data-step="dataset"]');
    if (dsStep) {
      dsStep.classList.remove('pending', 'done', 'active-step');
      if (hasDS) {
        dsStep.classList.add('done');
        const d = ds[0];
        const nf = (d.metadata && d.metadata.num_files) ? d.metadata.num_files : 0;
        $('#pipe-ds').textContent = nf + ' 文件 · DB: packfs.db';
      } else {
        dsStep.classList.add('pending');
        $('#pipe-ds').textContent = '扫描文件，写入 DB';
      }
    }

    // Shard
    const shStep = document.querySelector('.pipe-step[data-step="shard"]');
    if (shStep) {
      shStep.classList.remove('pending', 'done', 'active-step');
      if (hasShard) {
        shStep.classList.add('done');
        const dataShards = sh.filter(s => s.type === 'DATA');
        $('#pipe-shard').textContent = dataShards.length + ' 个 shard → 输出目录';
      } else {
        shStep.classList.add('pending');
        $('#pipe-shard').textContent = '合并小文件 → 大文件';
      }
    }

    // Arcset
    const asStep = document.querySelector('.pipe-step[data-step="arcset"]');
    if (asStep) {
      asStep.classList.remove('pending', 'done', 'active-step');
      if (hasArcset) {
        asStep.classList.add('done');
        const a = as[0];
        $('#pipe-arc').textContent = a.current_path || '--';
      } else {
        asStep.classList.add('pending');
        $('#pipe-arc').textContent = '纠删码容器';
      }
    }

    // EC
    const ecStep = document.querySelector('.pipe-step[data-step="ec"]');
    if (ecStep) {
      ecStep.classList.remove('pending', 'done', 'active-step');
      if (hasEC) {
        ecStep.classList.add('done');
        const a = as[0];
        const ecStr = a && a.metadata ? (a.metadata.ec || '8+4') : '8+4';
        $('#pipe-ec').textContent = 'RS(' + ecStr + ') · ' + (a ? a.current_path : '--');
      } else {
        ecStep.classList.add('pending');
        $('#pipe-ec').textContent = 'RS(k+m) 冗余保护';
      }
    }

    // Tape
    const tapeStep = document.querySelector('.pipe-step[data-step="tape"]');
    if (tapeStep) {
      tapeStep.classList.remove('pending', 'done', 'active-step');
      if (hasTape) {
        tapeStep.classList.add('done');
        const a = as.find(a => a.status === 'taped' || a.status === 'ready');
        const tc = a && a.metadata && a.metadata.tape_count ? a.metadata.tape_count : 1;
        $('#pipe-tape').textContent = tc + ' 盘磁带';
      } else {
        tapeStep.classList.add('pending');
        $('#pipe-tape').textContent = '--';
      }
    }

    // 更新箭头颜色
    const stepStates = [];
    $$('.pipe-step').forEach(s => {
      if (s.classList.contains('done')) stepStates.push('done');
      else if (s.classList.contains('active-step')) stepStates.push('active');
      else stepStates.push('pending');
    });
    arrows.forEach((arrow, i) => {
      arrow.classList.remove('done');
      if (stepStates[i] === 'done' && stepStates[i+1] === 'done') {
        arrow.classList.add('done');
      }
    });
  }

  // ====== Data refresh ======
  async function refreshAllData() {
    try {
      const [dsRes, asRes] = await Promise.all([
        API.get('/api/datasets'),
        API.get('/api/arcsets'),
      ]);
      if (dsRes.ok) state.datasets = dsRes.data;
      if (asRes.ok) state.arcsets = asRes.data;
      // Fetch shards for each dataset and arcset
      state.shards = [];
      for (const ds of state.datasets) {
        const shRes = await API.get('/api/shards?dataset_id=' + ds.id);
        if (shRes.ok && shRes.data) state.shards.push(...shRes.data);
      }
    } catch (e) {
      console.error('refresh data failed:', e);
    }
  }

  // ====== Refresh helpers ======
  function refreshShardTabSelects() {
    const sel = $('#sh-dataset-id');
    const ds = state.datasets;
    sel.innerHTML = ds.map(d => `<option value="${d.id}">[${d.id}] ${d.name} (${(d.metadata && d.metadata.num_files) || 0} 文件)</option>`).join('') || '<option value="">-- 请先创建 Dataset --</option>';
  }
  function refreshECTabSelects() {
    const sel = $('#ec-arcset-id');
    const as = state.arcsets;
    sel.innerHTML = as.map(a => `<option value="${a.id}">[${a.id}] ${a.name} (EC: ${(a.metadata && a.metadata.ec) || '-'})</option>`).join('') || '<option value="">-- 请先创建 Arcset --</option>';
  }
  function refreshArcsetTabSelects() {
    $('#ap-arcset-id').innerHTML = state.arcsets.map(a => `<option value="${a.id}">[${a.id}] ${a.name}</option>`).join('') || '<option value="">-- 无 Arcset --</option>';
    $('#ap-dataset-id').innerHTML = state.datasets.map(d => `<option value="${d.id}">[${d.id}] ${d.name}</option>`).join('') || '<option value="">-- 无 Dataset --</option>';
  }

  // ====== Table rendering ======
  function renderDSTable() {
    const ds = state.datasets;
    const tbody = $('#ds-tbody');
    if (ds.length === 0) {
      tbody.innerHTML = '<tr class="placeholder"><td colspan="9">暂无 Dataset</td></tr>';
      return;
    }
    tbody.innerHTML = ds.map(d => {
      const fm = d.metadata || {};
      return `
        <tr>
          <td><strong>${d.id}</strong></td>
          <td>${d.name}</td>
          <td>${fm.num_files || '-'}</td>
          <td>${fmtSize(fm.total_bytes || 0)}</td>
          <td>${d.shard_count || 0}</td>
          <td>${fm.format || '-'}</td>
          <td>${fm.compress || '无'}</td>
          <td><span class="badge badge-${d.status}">${d.status}</span></td>
          <td>
            <button class="btn btn-secondary btn-sm" data-action="finalize" data-dsid="${d.id}" ${d.status === 'archived' ? 'disabled' : ''}>Finalize</button>
            <button class="btn btn-secondary btn-sm" data-action="shards" data-dsid="${d.id}">查看 Shard</button>
            <button class="btn btn-secondary btn-sm" data-action="delete-ds" data-dsid="${d.id}" style="color:#c0392b;border-color:#c0392b">删除</button>
          </td>
        </tr>`;
    }).join('');
  }

  function renderShardTable() {
    const sh = state.shards;
    const tbody = $('#sh-tbody');
    if (sh.length === 0) {
      tbody.innerHTML = '<tr class="placeholder"><td colspan="7">暂无 Shard</td></tr>';
      return;
    }
    tbody.innerHTML = sh.map(s => {
      const type = s.type || 'DATA';
      const outputDir = s.output_dir || '--';
      const segCount = s.segments ? s.segments.length : (s.Segments ? s.Segments.length : '-');
      return `
      <tr>
        <td>${s.id}</td>
        <td><code>${s.file_path || s.FilePath || '-'}</code></td>
        <td><span class="badge badge-${type.toLowerCase()}">${type}</span></td>
        <td>${fmtSize(s.file_size || s.FileSize || 0)}</td>
        <td><code style="font-size:.72rem;max-width:180px;overflow:hidden;text-overflow:ellipsis;display:inline-block">${outputDir}</code></td>
        <td>${segCount}</td>
        <td>
          <button class="btn btn-secondary btn-sm" data-action="validate-shard" data-shid="${s.id}">校验</button>
          <button class="btn btn-secondary btn-sm" data-action="unpack-shard" data-shid="${s.id}">解包</button>
        </td>
      </tr>`;
    }).join('');
  }

  function renderASTable() {
    const as = state.arcsets;
    const tbody = $('#as-tbody');
    if (as.length === 0) {
      tbody.innerHTML = '<tr class="placeholder"><td colspan="8">暂无 Arcset</td></tr>';
      return;
    }
    tbody.innerHTML = as.map(a => {
      const dsCount = (a.dataset_ids && a.dataset_ids.length) || 0;
      return `
        <tr>
          <td><strong>${a.id}</strong></td>
          <td>${a.name}</td>
          <td>${(a.metadata && a.metadata.ec) || '-'}</td>
          <td><code style="font-size:.72rem;max-width:160px;overflow:hidden;text-overflow:ellipsis;display:inline-block">${a.current_path}</code></td>
          <td>${dsCount}</td>
          <td>${a.shard_count || 0}</td>
          <td><span class="badge badge-${a.status}">${a.status}</span></td>
          <td>
            <button class="btn btn-secondary btn-sm" data-action="ec-plan" data-asid="${a.id}">EC 布局</button>
          </td>
        </tr>`;
    }).join('');
  }

  // ====== Dataset Create ======
  $('#ds-create-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const rootDir = $('#ds-root-dir').value.trim();
    if (!rootDir) { toast('请填写源目录'); return; }
    const name = $('#ds-name').value.trim();
    const format = $('#ds-format').value;
    const compress = $('#ds-compress').value;
    const shardMax = parseInt($('#ds-shard-max').value) || 0;
    const genOnly = $('#ds-gen-only').checked;

    const body = {
      root_dir: rootDir,
      name: name,
      format: format,
      compress: compress,
      shard_max_bytes: shardMax,
      gen_only: genOnly,
    };

    const cmd = genOnly
      ? `packfs dataset create --root-dir=${rootDir} --format=${format} --compress=${compress} --shard-max-bytes=${shardMax} --gen-only`
      : `packfs dataset create --root-dir=${rootDir} --format=${format} --compress=${compress} --shard-max-bytes=${shardMax}`;
    $('#ds-cmd-hint').textContent = cmd;
    $('#ds-cmd-hint').classList.add('visible');
    setCliCmd(cmd);

    const res = await API.post('/api/datasets', body);
    if (res.ok) {
      await refreshAllData();
      renderDSTable();
      renderShardTable();
      refreshShardTabSelects();
      refreshArcsetTabSelects();
      updatePipeline();
      toast(genOnly ? `Dataset ${name || rootDir} 扫描完成` : `Dataset ${name || rootDir} 创建完成，${res.data.shard_count || 0} 个 shard`);
    } else {
      toast('创建失败: ' + (res.error || '未知错误'));
    }
  });

  // ====== Arcset Create ======
  $('#as-create-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const name = $('#as-name').value.trim();
    const targetRoot = $('#as-target-root').value.trim();
    if (!name || !targetRoot) { toast('请填写名称和目标目录'); return; }
    const ec = $('#as-ec').value;
    const tapeCount = parseInt($('#as-tape-count').value) || 0;
    const tapeMax = $('#as-tape-max').value.trim();

    const body = {
      name: name,
      target_root: targetRoot,
      ec: ec,
      tape_count: tapeCount,
      tape_max_bytes: tapeMax ? parseInt(tapeMax) : 0,
    };

    const cmd = `packfs arcset create --name=${name} --target-root=${targetRoot} --ec=${ec}`;
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

  // ====== Table actions (delegated) ======
  document.querySelector('main').addEventListener('click', async (e) => {
    const btn = e.target.closest('[data-action]');
    if (!btn) return;
    const action = btn.dataset.action;

    if (action === 'finalize') {
      const dsId = parseInt(btn.dataset.dsid);
      const res = await API.post('/api/datasets/' + dsId + '/finalize');
      if (res.ok) {
        await refreshAllData();
        renderDSTable();
        updatePipeline();
        toast(`Dataset ${dsId} 已归档`);
      } else {
        toast('Finalize 失败: ' + (res.error || '未知错误'));
      }

    } else if (action === 'shards') {
      const dsId = parseInt(btn.dataset.dsid);
      const dsShards = state.shards.filter(s => {
        const did = s.dataset_id || (s.Dataset && s.Dataset.Int64);
        return did === dsId || s.datasetId === dsId;
      });
      toast(`Dataset ${dsId}: ${dsShards.length} 个 shard`);

    } else if (action === 'validate-shard') {
      const shId = parseInt(btn.dataset.shid);
      toast('Shard 校验请使用 CLI: packfs shard validate --shard-file=...');

    } else if (action === 'unpack-shard') {
      toast('Shard 解包请使用 CLI: packfs shard unpack --shard-file=...');

    } else if (action === 'delete-ds') {
      const dsId = parseInt(btn.dataset.dsid);
      const ds = state.datasets.find(d => d.id === dsId);
      const label = ds ? ds.name : dsId;
      if (!confirm(`确认删除 Dataset "${label}"？\n此操作会同时删除关联的文件记录和 shard 记录，不可恢复。`)) return;
      const res = await fetch('/api/datasets/' + dsId, { method: 'DELETE' });
      const data = await res.json();
      if (data.ok) {
        await refreshAllData();
        renderDSTable();
        renderShardTable();
        renderASTable();
        refreshShardTabSelects();
        refreshArcsetTabSelects();
        refreshECTabSelects();
        updatePipeline();
        toast(`Dataset "${label}" 已删除`);
      } else {
        toast('删除失败: ' + (data.error || '未知错误'));
      }

    } else if (action === 'ec-plan') {
      const asId = parseInt(btn.dataset.asid);
      switchTab('ec');
      $('#ec-arcset-id').value = asId;
      doECPreview();
    }
  });

  // ====== Shard Preview ======
  $('#btn-preview-shard').addEventListener('click', () => {
    const dsId = parseInt($('#sh-dataset-id').value);
    if (!dsId) { toast('请选择一个 Dataset'); return; }
    const dataset = state.datasets.find(d => d.id === dsId);
    if (!dataset) { toast('Dataset 不存在'); return; }

    const shardMax = (dataset.metadata && dataset.metadata.shard_max_bytes) || 0;
    const format = (dataset.metadata && dataset.metadata.format) || 'bin';
    const dsShards = state.shards.filter(s => {
      const did = s.dataset_id || (s.Dataset && s.Dataset.Int64);
      return (did === dsId || s.datasetId === dsId) && (s.type === 'DATA' || !s.type);
    });

    const card = $('#shard-preview-card');
    const preview = $('#shard-preview');
    card.style.display = 'block';

    if (dsShards.length === 0) {
      preview.innerHTML = '<div class="empty-state"><div class="icon">📭</div>暂无 shard</div>';
      return;
    }

    preview.innerHTML = dsShards.map((sh, idx) => {
      const name = sh.file_path || sh.FilePath || (String(idx).padStart(4, '0') + '.' + format);
      const segs = sh.segments || sh.Segments || [];
      const segCount = segs.length;
      const shSize = sh.file_size || sh.FileSize || 0;
      return `
        <div class="shard-group">
          <div class="shard-group-header">
            <span class="shard-group-name">${name}</span>
            <span class="shard-group-meta">${segCount} 个 segment · ${fmtSize(shSize)}</span>
          </div>
          <div class="shard-group-files">
            ${segs.slice(0, 20).map(s => {
              const fp = s.file_path || s.FilePath || s.path || '?';
              return `<span class="shard-file">${fp.split('/').pop()}</span>`;
            }).join('')}
            ${segCount > 20 ? `<span class="shard-file" style="color:var(--accent)">... 还有 ${segCount - 20} 个文件</span>` : ''}
          </div>
        </div>`;
    }).join('');

    setCliCmd(`packfs shard make --dataset-id=${dsId}`);
    toast(`共 ${dsShards.length} 个 shard`);
  });

  // ====== Shard Make ======
  $('#btn-make-shard').addEventListener('click', async () => {
    const dsId = parseInt($('#sh-dataset-id').value);
    if (!dsId) { toast('请选择一个 Dataset'); return; }
    // Re-create shards for this dataset
    const res = await API.post('/api/datasets', {
      root_dir: state.datasets.find(d => d.id === dsId)?.current_path || '',
      name: state.datasets.find(d => d.id === dsId)?.name || '',
      format: (state.datasets.find(d => d.id === dsId)?.metadata?.format) || 'tar',
      compress: (state.datasets.find(d => d.id === dsId)?.metadata?.compress) || '',
      shard_max_bytes: (state.datasets.find(d => d.id === dsId)?.metadata?.shard_max_bytes) || 0,
    });
    if (res.ok) {
      await refreshAllData();
      renderShardTable();
      updatePipeline();
      toast('打包完成');
    } else {
      toast('打包失败: ' + (res.error || '未知错误'));
    }
  });

  // ====== EC Preview ======
  async function doECPreview() {
    const asId = parseInt($('#ec-arcset-id').value);
    if (!asId) { toast('请选择一个 Arcset'); return; }

    const res = await API.get('/api/ec/plan/' + asId);
    if (!res.ok) { toast(res.error || 'EC plan failed'); return; }
    const result = res.data;

    const card = $('#ec-preview-card');
    const preview = $('#ec-preview');
    card.style.display = 'block';

    $('#ec-legend-k').textContent = result.k;
    $('#ec-legend-m').textContent = result.m;

    preview.innerHTML = result.stripes.map(stripe => {
      const dataBlocks = (stripe.data || []).map(d => {
        const shortName = d.file_path ? d.file_path.replace(/^.*\//, '') : '?';
        return `<div class="stripe-block data" data-shid="${d.id}" title="${d.file_path || ''}">D${d.position}<span class="tooltip">${shortName}</span></div>`;
      });
      const ecBlocks = (stripe.ec || []).map(e => {
        if (e.type === 'PAD') {
          return `<div class="stripe-block pad">—<span class="tooltip">${e.name}</span></div>`;
        }
        return `<div class="stripe-block ec">E${e.position}<span class="tooltip">${e.name}</span></div>`;
      });
      const dataCount = (stripe.data || []).length;
      const ecCount = (stripe.ec || []).filter(e => e.type === 'EC').length;
      const padCount = (stripe.ec || []).filter(e => e.type === 'PAD').length;
      let summaryParts = [`<span class="dot-legend" style="background:#d6e8ff;border:1px solid #3b82f6"></span> ${dataCount} data`];
      if (ecCount > 0) summaryParts.push(`<span class="dot-legend" style="background:#fce4ec;border:1px solid #e91e63"></span> ${ecCount} EC`);
      if (padCount > 0) summaryParts.push(`<span class="dot-legend" style="background:#f5f5f5;border:1px dashed #ccc"></span> ${padCount} PAD`);

      return `
        <div class="stripe">
          <span class="stripe-label">Stripe ${stripe.index}</span>
          <div class="stripe-blocks">${dataBlocks.join('')}${ecBlocks.join('')}</div>
          <span class="stripe-summary">${summaryParts.join(' ')}</span>
        </div>`;
    }).join('');

    // 填充恢复下拉框
    const lostSel = $('#ec-lost-shard');
    const allDataShards = result.stripes.flatMap(s => s.data || []);
    lostSel.innerHTML = allDataShards.map(d => `<option value="${d.id}">${d.file_path || d.id}</option>`).join('');

    $('#ec-recover-card').style.display = 'block';
    setCliCmd(`packfs shard make-ec --arcset-id=${asId}`);
    toast(`${result.stripes.length} 个 stripe，k=${result.k}，m=${result.m}`);
  }
  $('#btn-preview-ec').addEventListener('click', doECPreview);

  // ====== EC Make ======
  $('#btn-make-ec').addEventListener('click', async () => {
    const asId = parseInt($('#ec-arcset-id').value);
    if (!asId) { toast('请先选择 Arcset'); return; }

    const res = await API.post('/api/ec/encode/' + asId);
    if (res.ok) {
      await refreshAllData();
      renderShardTable();
      updatePipeline();
      toast('EC 编码完成');
    } else {
      toast('EC 编码失败: ' + (res.error || '未知错误'));
    }
    setCliCmd('packfs shard make-ec --arcset-id=' + asId);
  });

  // ====== EC Recover ======
  $('#btn-recover').addEventListener('click', async () => {
    const asId = parseInt($('#ec-arcset-id').value);
    const lostShardId = parseInt($('#ec-lost-shard').value);
    if (!asId || !lostShardId) return;

    // 视觉反馈
    $$('.stripe-block.lost').forEach(b => b.classList.remove('lost'));
    $$('.stripe-block.recovered').forEach(b => b.classList.remove('recovered'));

    const lostBlock = document.querySelector(`.stripe-block.data[data-shid="${lostShardId}"]`);
    if (lostBlock) lostBlock.classList.add('lost');

    // 从下拉框获取选中的 shard file path
    const lostSel = $('#ec-lost-shard');
    const selectedOption = lostSel.options[lostSel.selectedIndex];
    const shardFile = selectedOption ? selectedOption.textContent : '';

    const res = await API.post('/api/ec/recover/' + asId, { shard_file: shardFile });
    if (res.ok) {
      if (lostBlock) {
        setTimeout(() => {
          lostBlock.classList.remove('lost');
          lostBlock.classList.add('recovered');
        }, 1500);
      }
      $('#recover-result').innerHTML = `
        <div class="recover-box">
          <h4>✓ 恢复成功</h4>
          <p>从 EC stripe 中恢复 ${shardFile}</p>
        </div>`;
      setCliCmd(`packfs shard recover --arcset-id=${asId} --shard-file=${shardFile}`);
      toast('恢复完成');
    } else {
      toast('恢复失败: ' + (res.error || '未知错误'));
    }
  });

  // ====== Arcset Append ======
  $('#btn-append').addEventListener('click', async () => {
    const asId = parseInt($('#ap-arcset-id').value);
    const dsId = parseInt($('#ap-dataset-id').value);
    if (!asId || !dsId) { toast('请选择 Arcset 和 Dataset'); return; }

    const res = await API.post('/api/arcsets/' + asId + '/append', { dataset_id: dsId });
    if (res.ok) {
      await refreshAllData();
      renderASTable();
      refreshECTabSelects();
      setCliCmd(`packfs arcset append --id=${asId} --dataset-id=${dsId}`);
      toast('Append 完成');
      updatePipeline();
    } else {
      toast('Append 失败: ' + (res.error || '未知错误'));
    }
  });

  // ====== Simulation ======
  // SKA preset: fills form with default astro dataset parameters
  $('#btn-sim-preset').addEventListener('click', async () => {
    try {
      const res = await fetch('/api/simulate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ _preset: 'ska' }),
      });
      // This returns with an error (no name), but we use DefaultConfig from Go.
      // Fallback: fill hardcoded defaults matching dataset.def
    } catch (e) { /* ignore */ }

    // Fill SKA preset values directly
    $('#sim-name').value = '1177938016';
    $('#sim-start-ts').value = '1177940019';
    $('#sim-end-ts').value = '1177940098';
    $('#sim-ch-start').value = '133';
    $('#sim-ch-end').value = '156';
    $('#sim-file-bytes').value = '10240';
    $('#sim-output-root').value = './data/dat';
    toast('已填入 SKA 天体物理数据集预设');
  });

  // Simulation form submission
  $('#sim-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const name = $('#sim-name').value.trim();
    const outputRoot = $('#sim-output-root').value.trim();
    if (!name) { toast('请填写 Dataset 名称'); return; }
    if (!outputRoot) { toast('请填写输出根目录'); return; }

    const body = {
      name: name,
      output_root: outputRoot,
      start_ts: parseInt($('#sim-start-ts').value) || 0,
      end_ts: parseInt($('#sim-end-ts').value) || 0,
      ch_start: parseInt($('#sim-ch-start').value) || 0,
      ch_end: parseInt($('#sim-ch-end').value) || 0,
      file_bytes: parseInt($('#sim-file-bytes').value) || 1024,
    };

    const hint = $('#sim-cmd-hint');
    const result = $('#sim-result');
    const btn = e.target.querySelector('button[type="submit"]');
    const origText = btn.textContent;
    btn.disabled = true;
    btn.textContent = '生成中...';
    result.innerHTML = '';

    hint.textContent = `python3 simulate.sh → ${body.name}: ch[${body.ch_start}..${body.ch_end}] × ts[${body.start_ts}..${body.end_ts}], ${body.file_bytes} bytes/file`;
    hint.classList.add('visible');

    try {
      const res = await API.post('/api/simulate', body);
      if (res.ok) {
        const d = res.data;
        result.innerHTML = `
          <div class="result-box" style="background:#e8f5e9;border:1px solid #4caf50;border-radius:8px;padding:1rem;margin-top:.5rem">
            <strong>✓ 生成完成</strong>
            <p style="margin:.25rem 0"><code>${d.output_dir}</code></p>
            <p style="margin:.25rem 0;color:#555">${d.file_count} 个文件 · ${fmtSize(d.total_bytes)}</p>
            <button class="btn btn-primary btn-sm" id="btn-sim-to-ds" style="margin-top:.5rem">📦 用此目录创建 Dataset</button>
          </div>`;
        toast(`${d.file_count} 个文件已生成`);

        // "Create Dataset" bridge button
        $('#btn-sim-to-ds').addEventListener('click', () => {
          switchTab('dataset');
          $('#ds-root-dir').value = outputRoot;
          $('#ds-name').value = name;
          toast('已切换到 Dataset 页签，源目录已填入，请点击"创建 Dataset"');
        });
      } else {
        result.innerHTML = `<div style="color:#c0392b;margin-top:.5rem">生成失败: ${res.error || '未知错误'}</div>`;
        toast('生成失败: ' + (res.error || '未知错误'));
      }
    } catch (err) {
      result.innerHTML = `<div style="color:#c0392b;margin-top:.5rem">请求失败: ${err.message}</div>`;
      toast('请求失败');
    } finally {
      btn.disabled = false;
      btn.textContent = origText;
    }
  });

  // ====== Init ======
  async function init() {
    const online = await checkHealth();
    if (online) {
      await refreshAllData();
    }
    renderDSTable();
    renderShardTable();
    renderASTable();
    updatePipeline();
    if (!online) {
      setCliCmd('API 离线 — 请启动 packfs serve');
    } else if (state.datasets.length === 0) {
      setCliCmd('packfs dataset create --root-dir=/data --format=tar --compress=zstd');
    }
  }

  init();

})();
