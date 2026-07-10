# EC 包设计文档

## 概述

EC（Erasure Code）包提供 Reed-Solomon 纠删码的编码、解码和验证功能。

- RS 算法库：`github.com/klauspost/reedsolomon`
- 项目参数约束：k+m ≤ 255（RS 硬限制）、k ≤ 24（工程上限）、m ∈ {2,4,6}

## 领域模型

### Config

EC 参数字符串格式为 `k+m`，如 `"8+4"`、`"4+2"`。

```
ParseConfig("8+4") → Config{K:8, M:4}
Config.Validate()   // 检查参数约束
Config.Total()      // 返回 k+m
```

### Stripe

一个 stripe（条带）是 RS 编码的最小单元：k 个 data shard + m 个 EC（parity）shard = k+m 个 shard。

```
Stripe 内 Position 编号（1-based）：
  Position 1..k         → 数据 shard（D）
  Position k+1..k+m     → 纠删码 shard（E）
```

**同一个 stripe 的 k+m 个 shard 必须在编码后分散到不同的物理磁带上**，确保任意 ≤m 盘磁带损坏时数据可恢复。

### StripeFile

描述一个 stripe 内单个文件的命名信息：

```
type StripeFile struct {
    OrigPath string // 原始 data 文件路径（EC 文件为空）
    NewPath  string // 纠删码命名后的完整路径
    Type     string // "D" 或 "E"
    Stripe   int    // 顺序号 (1-based)
    Position int    // 组号 (1-based), 1..k 为 D, k+1..k+m 为 E
}
```

## 文件命名规则

```
数据文件：<顺序号>D<组号>_<原始文件名>
EC 文件： <顺序号>E<组号>.<后缀>

示例（k=3, m=2, 后缀=bin）：
  Stripe 1 数据文件: 1D1_0000.bin, 1D2_0001.bin, 1D3_0002.bin
  Stripe 1 EC 文件:   1E4.bin, 1E5.bin
  Stripe 2 数据文件: 2D1_0003.bin, 2D2_0004.bin, 2D3_0005.bin
  Stripe 2 EC 文件:   2E4.bin, 2E5.bin
```

- EC 文件后缀从原始 data 文件名提取，保持同一 stripe 内后缀一致
- 顺序号：1..n（n = data 文件总数 / k）
- 组号：1..k+m

## 文件分配到 Stripe 的规则

输入 n*k 个原始 data 文件，按 gen-def 生成的顺序，每 k 个一组顺序切分：

```
输入: f1, f2, f3, f4, f5, f6, f7, f8, f9, f10, f11, f12  (n=4, k=3)

Stripe 1: f1, f2, f3          → 1D1_f1, 1D2_f2, 1D3_f3 + 1E4, 1E5
Stripe 2: f4, f5, f6          → 2D1_f4, 2D2_f5, 2D3_f6 + 2E4, 2E5
Stripe 3: f7, f8, f9          → 3D1_f7, 3D2_f8, 3D3_f9 + 3E4, 3E5
Stripe 4: f10, f11, f12       → 4D1_f10, 4D2_f11, 4D3_f12 + 4E4, 4E5
```

如果 data 文件数不能被 k 整除，需要补空 shard（内容为空的 data 文件）补齐。

**补齐分层**：

| 层级 | 负责模块 | 机制 | 产物 |
|------|---------|------|------|
| CLI 层（主） | `arcset gen-def` | 生成 `PAD_0000.{ext}.def`，由 `shard make` 创建空 shard 文件，写入 `t_shard` | 空 .def → 空 shard（DB 有记录） |
| EC 层（兜底） | `PlanStripes` | 文件数 % k ≠ 0 时自动补 padding 位（`OrigPath=""`），`EncodeStripe` 写空文件到 `NewPath` | 空 `_pad.{ext}` 文件（DB 不知情） |

CLI 链路（`arcset create --ec → gen-def → shard make → make-ec`）由 gen-def 保证整除，EC 层 padding 不触发。EC 包独立使用时（如 `ec_app`），PlanStripes 兜底。

## 磁带分配方案（TapeLayout）

同一 stripe 的 k+m 个 shard 必须分散到不同磁带上。EC 包提供两种分配方案，通过参数选择，缺省为方案 A。

### 方案 A：固定位置映射（RAID3 风格，缺省）

Position = 磁带编号。Position i 的所有 shard（跨所有 stripe）固定写到 Tape i。

```
以 k=3, m=2, 5 盘磁带为例：

  Tape1    Tape2    Tape3    Tape4    Tape5
  (Pos=1)  (Pos=2)  (Pos=3)  (Pos=4)  (Pos=5)
  ──────── ──────── ──────── ──────── ────────
  1D1_f1   1D2_f2   1D3_f3   1E4      1E5      ← Stripe 1
  2D1_f4   2D2_f5   2D3_f6   2E4      2E5      ← Stripe 2
  3D1_f7   3D2_f8   3D3_f9   3E4      3E5      ← Stripe 3
    ⋮        ⋮        ⋮        ⋮        ⋮

  Tape1-Tape3: 纯数据，可直接读取
  Tape4-Tape5: 纯 EC 校验
  坏任意 ≤2 盘 → 可恢复
```

特点：
- 数据磁带和校验磁带分离，读取性能好
- 负载不均：校验磁带仅在恢复时读取
- 实现简单，Position 直接映射到 Tape

### 方案 D：RAID5 风格旋转（positional rotation）

每个 stripe 的 EC 位置整体右移，使得数据和校验均匀分布到所有磁带。

```
旋转公式：
  EC 起始 Position 在物理磁带上的偏移 = ((stripe-1) * m) % (k+m)
  即: tape = ((position - 1 + (stripe-1) * m) % (k+m)) + 1

以 k=3, m=2, 5 盘磁带为例：

         Tape1    Tape2    Tape3    Tape4    Tape5
Stripe 1: 1D1      1D2      1D3      1E1      1E2     ← EC 在 4,5
Stripe 2: 2E2      2D1      2D2      2D3      2E1     ← EC 在 5,1
Stripe 3: 3E1      3E2      3D1      3D2      3D3     ← EC 在 1,2
Stripe 4: 4D3      4E1      4E2      4D1      4D2     ← EC 在 2,3
Stripe 5: 5D2      5D3      5E1      5E2      5D1     ← EC 在 3,4
Stripe 6: 6D1      6D2      6D3      6E1      6E2     ← 回到 origin
           ⋮         ⋮         ⋮         ⋮         ⋮

每个磁带: 约 3/5 data + 2/5 EC，负载均匀
坏任意 ≤2 盘 → 每个 stripe 最多丢 2 个 → 可恢复
```

特点：
- 负载均匀，所有磁带均参与读写
- 轮转使得多盘损坏时部分 stripe 可能仍有足够 shard 恢复
- 部分磁带在线时不能直接读取全量数据（每个磁带都有部分 EC）

### TapeLayout 参数

```go
type TapeLayout int

const (
    LayoutFixed    TapeLayout = 0 // 方案 A（缺省）：Position = Tape
    LayoutRotation TapeLayout = 1 // 方案 D：EC 位置轮转
)
```

## 公共 API 设计

### Interface 1: PlanStripes — 文件名规划

```go
func PlanStripes(dataFiles []string, cfg Config, outputDir string) [][]StripeFile
```

- 输入：n*k 个原始 data 文件路径、EC 参数、输出目录
- 输出：n 组，每组 k+m 个 StripeFile（含命名和元数据）
- 输出目录缺省为空时使用 data 文件所在目录（原地操作）
- 不执行任何文件操作，纯规划

### Interface 2: EncodeStripe — 单组编码

```go
func EncodeStripe(files []StripeFile, cfg Config) (*StripeResult, error)
```

执行流程：
1. 读取 k 个原始 data 文件内容
2. RS 编码生成 m 个 EC shard
3. 写入 m 个 EC 文件到目标路径
4. 重命名 k 个 data 文件为新名（1D1_xxx 格式）
5. 返回 StripeResult（OriginalSizes、PaddedSize）

失败处理：
- 步骤 1-3 失败：data 文件未改名，EC 文件可能不完整，删除坏 EC 文件重试即可
- 步骤 4 失败：EC 文件完整，检测新名是否已存在，补 rename

```go
type StripeResult struct {
    OriginalSizes []int64 // 每个 data 文件的原始字节数（去 padding 用）
    PaddedSize    int64   // 零填充对齐后的统一大小
}
```

### Interface 3: ReconstructStripe — 单组恢复

```go
func ReconstructStripe(files []StripeFile, cfg Config, sizes []int64, paddedSize int64) error
```

- files 长度 = k+m，缺失的文件对应 StripeFile.NewPath = ""
- 读取所有存活的 shard 文件，缺失位置留空
- 存活 data shard 需要临时 pad 到 paddedSize 再参与重建
- 重建后裁掉 padding（TrimData），写入恢复文件

### Interface 4: VerifyStripe — 单组校验

```go
func VerifyStripe(files []StripeFile, cfg Config) (bool, error)
```

- 读取 k+m 个文件，调用 RS Verify 检查 parity 一致性
- 用于 arcset validate 的深度抽查

## 调用方伪代码

### make-ec 流程

```go
groups := ec.PlanStripes(dataFiles, ecConfig, outputDir)

for _, files := range groups {
    result, err := ec.EncodeStripe(files, ecConfig)
    // 将 result.OriginalSizes, result.PaddedSize 写入 DB
    // 为每个 StripeFile 创建 t_shard 记录（Type=DATA 或 EC）
    // 按 TapeLayout 决定物理磁带分配:
    //   LayoutFixed:    tape = file.Position
    //   LayoutRotation: tape = ((file.Position-1 + (file.Stripe-1)*cfg.M) % cfg.Total()) + 1
}
```

### recover 流程

```go
// 已知丢失数据 shard f（通过 Stripe 和 Position 定位）
// 从 DB 获取: stripe 编号、OriginalSizes、PaddedSize
// 找到同 stripe 的 k 个存活 shard

files := make([]ec.StripeFile, k+m)
// 填充存活的和丢失的 StripeFile 信息（丢失的 NewPath 为 ""）

ec.ReconstructStripe(files, ecConfig, originalSizes, paddedSize)
// 丢失的文件已写入
```

### 空 shard 补齐

当 data 文件数不能被 k 整除时，gen-def 需要生成空的 data 文件补齐，使总数 = n*k。
空 shard 在 t_shard 中标记（metadata 中 file_count=0），参与 RS 编码但 zero-padding 长度为 0。

## 内部实现

公开函数内部调用私有的 `[][]byte` 操作函数，保持文件 I/O 与 RS 数学分离：

```go
// 不公开：内存中的 RS 操作，方便单元测试
func encodeBytes(data [][]byte, cfg Config) (*stripeResult, error)
func reconstructBytes(shards [][]byte, cfg Config) error
func verifyBytes(shards [][]byte, cfg Config) (bool, error)
```
