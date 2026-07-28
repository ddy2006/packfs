// =========================================================================
// PackFS Web UI — Main App
// =========================================================================

(function () {
  'use strict';

  // DOM helpers
  const $ = (sel, ctx) => (ctx || document).querySelector(sel);
  const $$ = (sel, ctx) => [...(ctx || document).querySelectorAll(sel)];

  // ====== Toast ======
  function toast(msg) {
    const el = document.createElement('div');
    el.className = 'toast';
    el.textContent = msg;
    document.body.appendChild(el);
    setTimeout(() => el.remove(), 2500);
  }

  // ====== CLI Command Bar ======
  function setCliCmd(cmd) {
    $('#cli-cmd').textContent = cmd;
  }
  $('#btn-copy').addEventListener('click', () => {
    navigator.clipboard.writeText($('#cli-cmd').textContent).then(() => toast('命令已复制')).catch(() => {});
  });

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

  // ====== Refresh helpers ======
  function refreshShardTabSelects() {
    const sel = $('#sh-dataset-id');
    const datasets = Simulator.getAllDatasets();
    sel.innerHTML = datasets.map(d => `<option value="${d.id}">[${d.id}] ${d.name} (${d.metadata.num_files || 0} 文件)</option>`).join('') || '<option value="">-- 请先创建 Dataset --</option>';
  }
  function refreshECTabSelects() {
    const sel = $('#ec-arcset-id');
    const arcsets = Simulator.getAllArcsets();
    sel.innerHTML = arcsets.map(a => `<option value="${a.id}">[${a.id}] ${a.name} (EC: ${a.metadata.ec || '-'})</option>`).join('') || '<option value="">-- 请先创建 Arcset --</option>';
  }
  function refreshArcsetTabSelects() {
    const arcsets = Simulator.getAllArcsets();
    const datasets = Simulator.getAllDatasets();
    $('#ap-arcset-id').innerHTML = arcsets.map(a => `<option value="${a.id}">[${a.id}] ${a.name}</option>`).join('') || '<option value="">-- 无 Arcset --</option>';
    $('#ap-dataset-id').innerHTML = datasets.map(d => `<option value="${d.id}">[${d.id}] ${d.name}</option>`).join('') || '<option value="">-- 无 Dataset --</option>';
  }
  function renderDSTable() {
    const ds = Simulator.getAllDatasets();
    const tbody = $('#ds-tbody');
    if (ds.length === 0) {
      tbody.innerHTML = '<tr class="placeholder"><td colspan="9">暂无 Dataset</td></tr>';
      return;
    }
    tbody.innerHTML = ds.map(d => {
      const fm = d.metadata || {};
      const shards = Simulator.getShardsByDataset(d.id);
      return `
        <tr>
          <td><strong>${d.id}</strong></td>
          <td>${d.name}</td>
          <td>${fm.num_files || '-'}</td>
          <td>${Simulator.fmtSize(fm.total_bytes || 0)}</td>
          <td>${shards.length}</td>
          <td>${fm.format || '-'}</td>
          <td>${fm.compress || '无'}</td>
          <td><span class="badge badge-${d.status}">${d.status}</span></td>
          <td>
            <button class="btn btn-secondary btn-sm" data-action="finalize" data-dsid="${d.id}" ${d.status === 'archived' ? 'disabled' : ''}>Finalize</button>
            <button class="btn btn-secondary btn-sm" data-action="shards" data-dsid="${d.id}">查看 Shard</button>
          </td>
        </tr>`;
    }).join('');
  }
  function renderShardTable() {
    const sh = Simulator.getAllShards();
    const tbody = $('#sh-tbody');
    if (sh.length === 0) {
      tbody.innerHTML = '<tr class="placeholder"><td colspan="6">暂无 Shard</td></tr>';
      return;
    }
    tbody.innerHTML = sh.map(s => `
      <tr>
        <td>${s.id}</td>
        <td><code>${s.filePath}</code></td>
        <td>${Simulator.fmtSize(s.size)}</td>
        <td><code title="${s.sha256}">${s.sha256.slice(0,16)}...</code></td>
        <td>${s.segments ? s.segments.length : '-'}</td>
        <td>
          <span class="badge badge-${s.type.toLowerCase()}">${s.type}</span>
          <button class="btn btn-secondary btn-sm" data-action="validate-shard" data-shid="${s.id}">校验</button>
          <button class="btn btn-secondary btn-sm" data-action="unpack-shard" data-shid="${s.id}">解包</button>
        </td>
      </tr>
    `).join('');
  }
  function renderASTable() {
    const as = Simulator.getAllArcsets();
    const tbody = $('#as-tbody');
    if (as.length === 0) {
      tbody.innerHTML = '<tr class="placeholder"><td colspan="7">暂无 Arcset</td></tr>';
      return;
    }
    tbody.innerHTML = as.map(a => {
      const shards = Simulator.getShardsByArcset(a.id);
      return `
        <tr>
          <td><strong>${a.id}</strong></td>
          <td>${a.name}</td>
          <td>${a.metadata.ec || '-'}</td>
          <td>${a.datasetIds.length}</td>
          <td>${shards.length}</td>
          <td><span class="badge badge-${a.status}">${a.status}</span></td>
          <td>
            <button class="btn btn-secondary btn-sm" data-action="ec-plan" data-asid="${a.id}">EC 布局</button>
          </td>
        </tr>`;
    }).join('');
  }

  // ====== Dataset Create ======
  $('#ds-create-form').addEventListener('submit', (e) => {
    e.preventDefault();
    const rootDir = $('#ds-root-dir').value.trim();
    if (!rootDir) { toast('请填写源目录'); return; }
    const name = $('#ds-name').value.trim();
    const format = $('#ds-format').value;
    const compress = $('#ds-compress').value;
    const shardMax = parseInt($('#ds-shard-max').value) || 0;
    const genOnly = $('#ds-gen-only').checked;

    const { ds, shardCount, genOnly: onlyScan } = Simulator.createDataset(rootDir, name, format, compress, shardMax, genOnly);
    renderDSTable();
    renderShardTable();
    refreshShardTabSelects();
    refreshArcsetTabSelects();

    const cmd = genOnly
      ? `packfs dataset create --root-dir=${rootDir} --format=${format} --compress=${compress} --shard-max-bytes=${shardMax} --gen-only`
      : `packfs dataset create --root-dir=${rootDir} --format=${format} --compress=${compress} --shard-max-bytes=${shardMax}`;
    $('#ds-cmd-hint').textContent = cmd;
    $('#ds-cmd-hint').classList.add('visible');
    setCliCmd(cmd);

    toast(genOnly ? `Dataset ${ds.name} 扫描完成（仅扫描模式）` : `Dataset ${ds.name} 创建完成，${shardCount} 个 shard`);
  });

  // ====== Arcset Create ======
  $('#as-create-form').addEventListener('submit', (e) => {
    e.preventDefault();
    const name = $('#as-name').value.trim();
    const targetRoot = $('#as-target-root').value.trim();
    if (!name || !targetRoot) { toast('请填写名称和目标目录'); return; }
    const ec = $('#as-ec').value;
    const tapeCount = parseInt($('#as-tape-count').value) || 0;
    const tapeMax = $('#as-tape-max').value.trim();

    const as = Simulator.createArcset(name, targetRoot, ec, tapeCount, tapeMax);
    renderASTable();
    refreshECTabSelects();
    refreshArcsetTabSelects();

    const cmd = `packfs arcset create --name=${name} --target-root=${targetRoot} --ec=${ec}`;
    $('#as-cmd-hint').textContent = cmd;
    $('#as-cmd-hint').classList.add('visible');
    setCliCmd(cmd);
    toast(`Arcset ${as.name} 创建完成`);
  });

  // ====== Table actions (delegated) ======
  document.querySelector('main').addEventListener('click', (e) => {
    const btn = e.target.closest('[data-action]');
    if (!btn) return;
    const action = btn.dataset.action;

    if (action === 'finalize') {
      const dsId = parseInt(btn.dataset.dsid);
      const result = Simulator.finalizeDataset(dsId);
      if (result.ok) { renderDSTable(); toast(`Dataset ${dsId} 已归档`); }

    } else if (action === 'shards') {
      const dsId = parseInt(btn.dataset.dsid);
      const sh = Simulator.getShardsByDataset(dsId);
      toast(`Dataset ${dsId}: ${sh.length} 个 shard`);

    } else if (action === 'validate-shard') {
      const shId = parseInt(btn.dataset.shid);
      const result = Simulator.validateShard(shId);
      toast(result.ok ? '✓ SHA-256 校验通过' : '✗ 校验失败: ' + result.sha256);

    } else if (action === 'unpack-shard') {
      const shId = parseInt(btn.dataset.shid);
      const result = Simulator.unpackShard(shId, '/tmp/unpack');
      if (result.ok) toast(`解包完成: ${result.files.length} 个文件 → /tmp/unpack`);
      else toast('解包失败: ' + result.error);

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
    const dataset = Simulator.getAllDatasets().find(d => d.id === dsId);
    if (!dataset) { toast('Dataset 不存在'); return; }

    const shardMax = dataset.metadata.shard_max_bytes || 0;
    const groups = Simulator.generateShardGroups(dataset.files, shardMax, dsId);
    const format = dataset.metadata.format || 'bin';

    const card = $('#shard-preview-card');
    const preview = $('#shard-preview');
    card.style.display = 'block';

    if (groups.length === 0) {
      preview.innerHTML = '<div class="empty-state"><div class="icon">📭</div>暂无文件</div>';
      return;
    }

    preview.innerHTML = groups.map((grp, idx) => {
      const name = String(idx).padStart(4, '0') + '.' + format;
      const totalSize = grp.reduce((a, f) => a + f.size, 0);
      return `
        <div class="shard-group">
          <div class="shard-group-header">
            <span class="shard-group-name">${name}</span>
            <span class="shard-group-meta">${grp.length} 个文件 · ${Simulator.fmtSize(totalSize)}</span>
          </div>
          <div class="shard-group-files">
            ${grp.slice(0, 20).map(f => `<span class="shard-file">${f.path.split('/').pop()}</span>`).join('')}
            ${grp.length > 20 ? `<span class="shard-file" style="color:var(--accent)">... 还有 ${grp.length - 20} 个文件</span>` : ''}
          </div>
        </div>`;
    }).join('');

    setCliCmd(`packfs shard make --dataset-id=${dsId}`);
    toast(`预览 ${groups.length} 个 shard 分组`);
  });

  // ====== Shard Make ======
  $('#btn-make-shard').addEventListener('click', () => {
    const dsId = parseInt($('#sh-dataset-id').value);
    if (!dsId) { toast('请选择一个 Dataset'); return; }
    // 模拟：实际已在 createDataset 中生成 shard
    renderShardTable();
    toast('打包完成');
  });

  // ====== EC Preview ======
  function doECPreview() {
    const asId = parseInt($('#ec-arcset-id').value);
    if (!asId) { toast('请选择一个 Arcset'); return; }
    const result = Simulator.planEC(asId);
    if (!result.ok) { toast(result.error); return; }

    const card = $('#ec-preview-card');
    const preview = $('#ec-preview');
    card.style.display = 'block';

    preview.innerHTML = result.stripes.map(stripe => {
      const blocks = [
        ...stripe.data.map(d => `<div class="stripe-block data" data-shid="${d.id}">D${d.position}<span class="tooltip">${d.filePath}</span></div>`),
        ...stripe.ec.map(e => `<div class="stripe-block ${e.type === 'EC' ? 'ec' : 'pad'}">${e.type === 'EC' ? 'E' + e.position : 'PAD'}<span class="tooltip">${e.name}</span></div>`),
      ];
      return `
        <div class="stripe">
          <span class="stripe-label">Stripe ${stripe.index}</span>
          <div class="stripe-blocks">${blocks.join('')}</div>
        </div>`;
    }).join('');

    // 填充恢复下拉框
    const lostSel = $('#ec-lost-shard');
    const allDataShards = result.stripes.flatMap(s => s.data);
    lostSel.innerHTML = allDataShards.map(d => `<option value="${d.id}">${d.filePath}</option>`).join('');

    $('#ec-recover-card').style.display = 'block';
    setCliCmd(`packfs shard make-ec --arcset-id=${asId}`);
    toast(`${result.stripes.length} 个 stripe，k=${result.k}，m=${result.m}`);
  }
  $('#btn-preview-ec').addEventListener('click', doECPreview);

  // ====== EC Make ======
  $('#btn-make-ec').addEventListener('click', () => {
    toast('EC 编码完成（模拟）');
    setCliCmd('packfs shard make-ec --arcset-id=' + ($('#ec-arcset-id').value || '1'));
  });

  // ====== EC Recover ======
  $('#btn-recover').addEventListener('click', () => {
    const asId = parseInt($('#ec-arcset-id').value);
    const lostShardId = parseInt($('#ec-lost-shard').value);
    if (!asId || !lostShardId) return;

    // 视觉反馈：标记丢失
    $$('.stripe-block.lost').forEach(b => b.classList.remove('lost'));
    const lostBlock = document.querySelector(`.stripe-block.data[data-shid="${lostShardId}"]`);
    if (lostBlock) lostBlock.classList.add('lost');

    const result = Simulator.simulateRecover(asId, lostShardId);
    if (!result.ok) { toast(result.error); return; }
    const lostShard = Simulator.findShard(lostShardId);

    $('#recover-result').innerHTML = `
      <div class="recover-box">
        <h4>✓ 恢复成功</h4>
        <p>${result.message}</p>
        <p><code>${result.lostShard}</code> — 恢复耗时: ${result.recoverTime}</p>
      </div>`;
    setCliCmd(`packfs shard recover --arcset-id=${asId} --shard-file=${lostShard ? lostShard.filePath : ''}`);
    toast('恢复完成');
  });

  // ====== Arcset Append ======
  $('#btn-append').addEventListener('click', () => {
    const asId = parseInt($('#ap-arcset-id').value);
    const dsId = parseInt($('#ap-dataset-id').value);
    if (!asId || !dsId) { toast('请选择 Arcset 和 Dataset'); return; }

    const result = Simulator.appendDataset(asId, dsId);
    if (result.ok) {
      renderASTable();
      refreshECTabSelects();
      setCliCmd(`packfs arcset append --id=${asId} --dataset-id=${dsId}`);
      toast('Append 完成');
    } else {
      toast('Append 失败: ' + result.error);
    }
  });

  // ====== Init ======
  // 预建一个示例 Dataset，让首次打开有内容
  const demo = Simulator.createDataset('/data/astro/1177938016', 'astro-demo', 'tar', 'zstd', 67108864, false);
  renderDSTable();
  renderShardTable();
  setCliCmd(`packfs dataset create --root-dir=/data/astro/1177938016 --format=tar --compress=zstd --shard-max-bytes=67108864`);

})();
