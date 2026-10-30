# packfs WebUI

管理面板前端，编译时通过根目录 `embed.go` 的 `//go:embed all:webui` 嵌入 `packfs` 二进制。

## 启动

```sh
PACKFS_PG_URL='postgres://...' packfs serve --addr=:8080 --db-dir=/data/packfs/datasets
# 浏览器打开 http://localhost:8080
```

- dataset 页签读取 `--db-dir` 下的独立 SQLite。
- arcset / EC / shard 布局页签使用 PostgreSQL；未配置 PG 时这些页签返回 503。
- 管道末端为“移交下游存储”，不展示介质/磁带状态。

## 文件

- `index.html`：页面骨架、页签、表单。
- `js/app.js`：REST API 客户端与渲染逻辑。
- `css/style.css`：样式。

API 路由与端点说明见 `docs/design/api-webui.md`。
