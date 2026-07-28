// =========================================================================
// PackFS Simulator
// 纯前端模拟 packfs 核心逻辑，后续可替换为真实 API 调用。
// =========================================================================

const Simulator = (() => {

  // ---------- In-memory store ----------
  let nextDSId = 1;
  let nextASId = 1;
  let nextShId = 1;
  const datasets  = [];   // {id, name, rootDir, files: [{path,size}], metadata, status}
  const arcsets   = [];   // {id, name, targetRoot, metadata, datasetIds: [], status}
  const shards    = [];   // {id, datasetId, arcsetId, filePath, type, segments, size, sha256}

  // 模拟文件系统（预设的假文件树）
  const MOCK_FILES = {
    "1177938016/1177938016_1177940019_ch133.dat": 8388608,
    "1177938016/1177938016_1177940020_ch133.dat": 8388608,
    "1177938016/1177938016_1177940021_ch133.dat": 8388608,
    "1177938016/1177938016_1177940019_ch134.dat": 8388608,
    "1177938016/1177938016_1177940020_ch134.dat": 8388608,
    "1177938016/1177938016_1177940021_ch134.dat": 8388608,
    "1177938016/1177938016_1177940019_ch135.dat": 8388608,
    "1177938016/1177938016_1177940020_ch135.dat": 8388608,
    "1177938016/1177938016_1177940019_ch136.dat": 8388608,
    "1177938016/1177938016_1177940020_ch136.dat": 8388608,
    "1177938016/1177938016_1177940019_ch137.dat": 8388608,
    "1177938016/1177938016_1177940020_ch137.dat": 8388608,
    "1177938016/1177938016_1177940021_ch137.dat": 8388608,
    "1177938016/1177938016_1177940022_ch137.dat": 8388608,
    "1177938016/1177938016_1177940019_ch138.dat": 8388608,
    "1177938016/1177938016_1177940020_ch138.dat": 8388608,
    "1177938016/1177938016_1177940021_ch138.dat": 8388608,
    "1177938016/1177938016_1177940022_ch138.dat": 8388608,
    "1177938016/1177938016_1177940019_ch139.dat": 8388608,
    "1177938016/1177938016_1177940020_ch139.dat": 8388608,
  };

  function uuid() { return Math.random().toString(36).slice(2, 10); }
  function fmtSize(bytes) {
    if (bytes < 1024) return bytes + ' B';
    if (bytes < 1048576) return (bytes / 1024).toFixed(1) + ' KB';
    if (bytes < 1073741824) return (bytes / 1048576).toFixed(1) + ' MB';
    return (bytes / 1073741824).toFixed(2) + ' GB';
  }
  function fakeSHA() {
    let h = '';
    for (let i = 0; i < 64; i++) h += '0123456789abcdef'[Math.floor(Math.random()*16)];
    return h;
  }

  // ---------- Dataset ----------
  function createDataset(rootDir, name, format, compress, shardMaxBytes, genOnly) {
    const ds = {
      id: nextDSId++,
      name: name || rootDir.split('/').pop(),
      rootDir,
      files: Object.entries(MOCK_FILES).map(([p, s]) => ({ path: rootDir + '/' + p, size: s })),
      metadata: {
        format: format || 'tar',
        compress: compress || 'zstd',
        shard_max_bytes: shardMaxBytes || 1073741824,
      },
      status: 'active',
      createdAt: new Date().toISOString(),
    };
    ds.metadata.num_files = ds.files.length;
    ds.metadata.total_bytes = ds.files.reduce((a, f) => a + f.size, 0);

    let shardCount = 0;
    if (!genOnly) {
      const groups = generateShardGroups(ds.files, shardMaxBytes || 0, ds.id);
      shardCount = groups.length;
      groups.forEach((grp, idx) => {
        const shName = String(idx).padStart(4, '0') + '.' + (format || 'tar');
        const totalSize = grp.reduce((a, f) => a + f.size, 0);
        shards.push({
          id: nextShId++,
          datasetId: ds.id,
          arcsetId: null,
          filePath: shName,
          type: 'DATA',
          segments: grp.map((f, si) => ({ path: f.path, offset: 0, size: f.size, fileId: si + 1 })),
          size: totalSize,
          sha256: fakeSHA(),
        });
      });
    }

    datasets.push(ds);
    return { ds, shardCount, genOnly };
  }

  // ---------- Shard 分组（与 Go 端 GenerateShardDefs 逻辑一致）----------
  function generateShardGroups(files, shardMaxBytes, datasetId) {
    if (shardMaxBytes <= 0) return [files]; // 全部放一个 shard

    const groups = [];
    let current = [];
    let currentSize = 0;

    for (const f of files) {
      if (f.size > shardMaxBytes) {
        // 大文件拆分
        if (current.length > 0) { groups.push(current); current = []; currentSize = 0; }
        for (let offset = 0; offset < f.size; offset += shardMaxBytes) {
          const segSize = Math.min(shardMaxBytes, f.size - offset);
          groups.push([{ ...f, _split: true, _offset: offset, _size: segSize }]);
        }
      } else if (currentSize + f.size > shardMaxBytes && current.length > 0) {
        groups.push(current);
        current = [f];
        currentSize = f.size;
      } else {
        current.push(f);
        currentSize += f.size;
      }
    }
    if (current.length > 0) groups.push(current);
    return groups;
  }

  // ---------- Shard 校验 ----------
  function validateShard(shardId) {
    const sh = shards.find(s => s.id === shardId);
    if (!sh) return { ok: false, error: 'shard not found' };
    // 模拟：95% 概率通过
    const ok = Math.random() > 0.05;
    return { ok, sha256: ok ? sh.sha256 : 'MISMATCH_' + fakeSHA().slice(0, 16) };
  }

  // ---------- Shard 解包 ----------
  function unpackShard(shardId, outputDir) {
    const sh = shards.find(s => s.id === shardId);
    if (!sh) return { ok: false, error: 'shard not found' };
    const files = sh.segments.map(seg => ({
      path: outputDir + '/' + seg.path.split('/').pop(),
      size: seg.size,
    }));
    return { ok: true, files };
  }

  // ---------- Arcset ----------
  function createArcset(name, targetRoot, ec, tapeCount, tapeMaxBytes) {
    const meta = { ec: ec || '8+4' };
    if (tapeCount) meta.tape_count = tapeCount;
    if (tapeMaxBytes) meta.tape_max_bytes = Number(tapeMaxBytes);

    const as = {
      id: nextASId++,
      name,
      targetRoot,
      metadata: meta,
      datasetIds: [],
      status: 'building',
      createdAt: new Date().toISOString(),
    };
    arcsets.push(as);
    return as;
  }

  function appendDataset(arcsetId, datasetId) {
    const as = arcsets.find(a => a.id === arcsetId);
    if (!as) return { ok: false, error: 'arcset not found' };
    const ds = datasets.find(d => d.id === datasetId);
    if (!ds) return { ok: false, error: 'dataset not found' };

    // 兼容性校验
    if (as.datasetIds.length > 0) {
      const first = datasets.find(d => d.id === as.datasetIds[0]);
      const firstFmt = first && first.metadata.format;
      const firstCmp = first && first.metadata.compress;
      if (ds.metadata.format !== firstFmt || ds.metadata.compress !== firstCmp) {
        return { ok: false, error: `format/compress mismatch: dataset has ${ds.metadata.format}/${ds.metadata.compress}, arcset expects ${firstFmt}/${firstCmp}` };
      }
    } else {
      // 首次 append，继承 shard_max_bytes
      as.metadata.shard_max_bytes = ds.metadata.shard_max_bytes;
    }
    as.datasetIds.push(datasetId);
    return { ok: true };
  }

  // ---------- EC ----------
  function planEC(arcsetId) {
    const as = arcsets.find(a => a.id === arcsetId);
    if (!as) return { ok: false, error: 'arcset not found' };

    const ecStr = as.metadata.ec || '8+4';
    const [k, m] = ecStr.split('+').map(Number);
    const allShards = shards.filter(s => as.datasetIds.includes(s.datasetId) && s.type === 'DATA');
    const total = k + m;

    let stripeIndex = 0;
    const stripes = [];
    for (let i = 0; i < allShards.length; i += k) {
      const data = allShards.slice(i, i + k);
      const ecBlocks = [];
      for (let j = 0; j < m; j++) {
        ecBlocks.push({ name: `E${stripeIndex}-${k + j}.ec`, type: 'EC', position: k + j });
      }
      // pad if not enough data
      const padCount = k - data.length;
      for (let j = 0; j < padCount; j++) {
        ecBlocks.push({ name: `PAD-${stripeIndex}-${data.length + j}.pad`, type: 'PAD', position: data.length + j });
      }
      stripes.push({
        index: stripeIndex,
        data: data.map((sh, idx) => ({ ...sh, position: idx })),
        ec: ecBlocks,
        total,
      });
      stripeIndex++;
    }
    return { ok: true, stripes, k, m };
  }

  function simulateRecover(arcsetId, lostShardId) {
    // 从 stripe 中找丢失的 shard，模拟从其余 shard 恢复
    const plan = planEC(arcsetId);
    if (!plan.ok) return plan;
    const lost = shards.find(s => s.id === lostShardId);
    if (!lost) return { ok: false, error: 'shard not found' };

    // 找到该 shard 所在的 stripe
    let foundStripe = null;
    for (const stripe of plan.stripes) {
      if (stripe.data.some(d => d.id === lostShardId)) {
        foundStripe = stripe;
        break;
      }
    }
    return {
      ok: true,
      recovered: true,
      message: `从 Stripe ${foundStripe ? foundStripe.index : '?'} 的剩余 ${plan.k - 1} 个 data shard + ${plan.m} 个 EC shard 成功恢复`,
      lostShard: lost.filePath,
      recoverTime: (Math.random() * 3 + 0.5).toFixed(1) + 's',
    };
  }

  // ---------- Dataset finalize ----------
  function finalizeDataset(datasetId) {
    const ds = datasets.find(d => d.id === datasetId);
    if (!ds) return { ok: false, error: 'dataset not found' };
    ds.status = 'archived';
    return { ok: true };
  }

  return {
    createDataset, generateShardGroups,
    validateShard, unpackShard,
    createArcset, appendDataset,
    planEC, simulateRecover,
    finalizeDataset,
    getAllDatasets: () => [...datasets],
    getAllArcsets: () => [...arcsets],
    getShardsByDataset: (dsId) => shards.filter(s => s.datasetId === dsId && s.type === 'DATA'),
    getShardsByArcset: (asId) => shards.filter(s => s.arcsetId === asId || datasets.some(d => d.id === s.datasetId && arcsets.some(a => a.id === asId && a.datasetIds.includes(d.id)))),
    getAllShards: () => [...shards],
    findShard: (id) => shards.find(s => s.id === id),
    fmtSize, uuid, fakeSHA,
    reset: () => { datasets.length = 0; arcsets.length = 0; shards.length = 0; nextDSId = 1; nextASId = 1; nextShId = 1; },
  };
})();
