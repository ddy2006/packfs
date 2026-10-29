# CLAUDE.md

This file provides guidance to Claude Code when working in `cmd/cli/`.

## 架构

三层分离，`main.go` 不包含业务逻辑：

```
cmd/cli/
├── main.go            # 入口：import 子包，组装命令树
├── root.go            # package main：rootCmd、日志初始化、execRoot
├── <domain>/<domain>.go  # package <domain>：导出 Command() *cobra.Command
```

```
cmd/cli/<domain>/   →  CLI 适配层：参数解析、调用 internal、格式化输出
internal/<domain>/  →  领域模型 + Store + 业务逻辑
```

## 添加新命令

1. 在 `cmd/cli/<domain>/` 下创建子包
2. 导出 `Command() *cobra.Command`，内部通过工厂函数创建子命令并注册 flag
3. 在 `cmd/cli/main.go` 中 import 并 `rootCmd.AddCommand(<domain>.Command())`

```go
func Command() *cobra.Command {
    cmd := &cobra.Command{Use: "xxx", Short: "..."}
    cmd.AddCommand(subCmd1(), subCmd2())
    return cmd
}

func subCmd1() *cobra.Command {
    cmd := &cobra.Command{Use: "do", RunE: func(...) { ... }}
    cmd.Flags().String("flag-name", "", "description")
    return cmd
}
```

## 参数命名

- 目录路径：`--source-root` / `--target-root`
- 单文件：`--shard-file`、`--def-file`、`--ec-shard-file`
- 标识：`--name`、`--id`、`--dataset-db`、`--arcset-id`、`--dataset-uid`

## Shard 定义文件（.def）

文件名：`<id>[.<compress>].<format>.def`（如 `0001.bin.def`）

文件内容每行一个 segment：
- 不以 `{` 开头 → 相对路径（完整文件）
- 以 `{` 开头 → JSON 行 `{"path":"...","offset":0,"size":1024}`
- 注释头 `# dataset_db: <sqlite_path>` 是 `shard make` 打开 dataset SQLite 的入口。

生成：外部脚本 / `gen-def.sh`，消费：`shard make --def-file=...`。解析逻辑在 `internal/shard/def.go`。

## 当前新命令

```sh
packfs dataset create --root-dir=... --db-dir=... --target-root=... [--gen-only]
packfs dataset mark-built --dataset-db=...
packfs dataset unpack --dataset-db=... --target-root=...
packfs shard make --def-file=... [--output-dir=...]

packfs arcset create --name=... --target-root=... --ec=... --partition-capacity=...
packfs arcset append --id=... --dataset-db=...
packfs arcset build-data --id=...
packfs arcset make-ec --id=...
packfs arcset validate --id=... --stage=all
packfs arcset recover --id=... --shard-file=...
packfs arcset export --id=...

packfs fs mount --dataset-db=... | --arcset-id=...
packfs fs list
packfs fs umount --mount-point=...
```

旧 `dataset validate/finalize`、`arcset rebuild`、`shard make-ec/recover` 尚未清理，不要在新流程中使用。
