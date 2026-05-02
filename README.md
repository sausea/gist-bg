# Gist

[![License: GPL v2](https://img.shields.io/badge/License-GPL_v2-blue.svg)](https://www.gnu.org/licenses/old-licenses/gpl-2.0.en.html) [![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/9bingyin/Gist) [![zread]

[![GitHub Release](https://img.shields.io/github/v/release/9bingyin/Gist)](https://github.com/9bingyin/Gist/releases/latest) [![Build Docker Image](https://github.com/9bingyin/Gist/actions/workflows/docker-build.yml/badge.svg)](https://github.com/9bingyin/Gist/actions/workflows/docker-build.yml)

轻量级自托管 RSS 阅读器，内置 AI 翻译、结构化分析与 AI 日报能力。


## 功能特性

- 全格式订阅，支持 RSS 2.0 / Atom / JSON Feed
- Readability 沉浸式阅读模式
- AI 翻译、结构化分析与日报，支持 OpenAI / Anthropic / 兼容接口（BYOK）
- AI 分析可自动后台异步处理，详情页直接复用 AI 分析中的摘要字段
- 文章详情页展示 AI 后台任务状态，仅显示 AI 分析卡片
- AI 分析库页面，顶部查看分析队列，下方默认展示最近 10 条已入库分析结果
- AI 日报页面，基于已入库分析结果按日聚合
- AI 日报与 AI 分析库支持通过共享 API Key 免登录供外部系统调用
- AI 分析结果会在数据库中持久化，分析标题会翻译为中文后再用于入库展示
- AI 分析完成后自动归档为 Markdown 文件，支持全局目录与文件夹级目录，按日期 / 订阅文件夹 / 订阅源保存
- AI Prompt 模板支持外置到数据目录，Docker 场景可直接编辑挂载目录中的模板文件
- 文件夹分层管理与内容分类
- 浅色 / 深色 / 跟随系统主题
- PWA，可安装到桌面和移动设备
- 多语言（简体中文 / English）

## AI 相关新增说明

### AI 能力划分

- 当前 AI 能力主要包括三部分：
  - AI 翻译
  - AI 分析
  - AI 日报
- 设置页支持分别配置三组模型：
  - `AI 翻译`
  - `AI 分析`
  - `AI 日报`
- 文章详情页不再单独展示 “AI 摘要” 卡片，而是直接展示 “AI 分析” 卡片
- AI 分析结果中的 `summary` 字段会直接作为详情页摘要内容复用

### AI 分析库

- 后端会将 AI 分析结果保存到数据库中的 `ai_analyses`
- AI 分析库标题优先读取中文翻译缓存 `ai_list_translations`
- 前端页面入口为 `/ai-analyses`
- 页面顶部优先展示“分析队列表”，便于先查看当前排队与处理中状态
- “已入库分析”区域默认只展示最近 10 条结果
- 点击 AI 分析库中的文章会直接在新标签页打开原始文章链接

### AI 日报

- AI 日报基于当日已入库的 AI 分析结果实时聚合
- 前端页面入口为 `/ai-daily-report`
- 后端接口为 `GET /api/ai/reports/daily?date=YYYY-MM-DD`
- 日报输出结构为：
  - 今日热点综述
  - 风险点评
  - 趋势判断

### 外部系统免登录调用

- 已支持通过共享 API Key 调用：
  - `GET /api/ai/reports/daily`
  - `GET /api/ai/analyses`
- 请求头支持：
  - `Authorization: Bearer <token>`
  - `X-Gist-API-Key: <key>`
  - `X-API-Key: <key>`
- 共享访问密钥可在系统设置中配置

### AI Markdown 归档

- AI 分析完成后会额外生成一份 Markdown 文件
- 服务默认归档根目录为 `$GIST_DATA_DIR/ai-archive`
- 可在“设置 -> 通用”中配置全局 `AI 分析归档目录`
- 可在“设置 -> 文件夹”中为每个订阅文件夹单独配置 `AI 归档目录`
- 归档目录优先级如下：
  - 当前订阅文件夹的归档目录
  - 最近父文件夹的归档目录
  - 通用设置中的全局 `AI 分析归档目录`
  - 服务启动时的默认目录
- 路径结构示例：

```text
/Users/usr/gist-data/20260407/CnNews/俄罗斯卫星通信社/阮春福当选国家主席.md
```

- 当某个文件夹配置了独立归档目录后，该文件夹下的新分析结果会写入该目录
- 已生成的历史 Markdown 文件不会自动迁移

### AI Prompt 模板外置

- 后端启动时会自动在 `$GIST_PROMPTS_DIR` 生成默认 prompt 模板文件
- 若未显式配置 `GIST_PROMPTS_DIR`，默认目录为 `$GIST_DATA_DIR/prompts`
- 当前会生成以下模板：
  - `summary.tmpl`
  - `translate_block.tmpl`
  - `translate_text.tmpl`
  - `analysis.tmpl`
  - `daily_report.tmpl`
  - `coordinate_lookup.tmpl`
- 前端“设置 -> AI”页支持直接在线查看和编辑这些 Prompt 模板
- Docker 部署时可直接编辑宿主机挂载目录中的这些模板文件
- 模板修改后，新发起的 AI 请求会自动读取新内容；若模板语法错误，系统会自动回退到内置默认 prompt


### 离线部署（无外网）

当部署服务器无法访问外网时，请在有网络的机器上构建镜像并 `docker save` 导出，然后在服务器上 `docker load` 导入运行。

仓库已提供一键离线打包脚本：

```bash
cd /Users/usr/Gist-bg

# 常见 x86_64 Linux 服务器
./scripts/build_offline_bundle.sh amd64

# ARM64 Linux 服务器
# ./scripts/build_offline_bundle.sh arm64
```

输出文件默认位于：

```text
/Users/usr/Gist-bg/dist/offline/
```

生成文件示例：

```text
gist-v1.0.1-offline_linux-amd64.tar.gz
gist-v1.0.1-offline_linux-amd64.tar.gz.sha256
```

离线 compose 也支持通过环境变量切换镜像标签，例如：

```bash
GIST_IMAGE=gist:offline-arm64 docker compose -f docker-compose.offline.yml up -d
```

离线包中会同时包含两个镜像标签：

- `gist:offline-<arch>`：可直接复用默认 `docker-compose.offline.yml`
- `gist:1.0.1-offline-<arch>`：便于识别当前导入的离线镜像版本

详见 [offline-docker.md](/Users/usr/Gist-bg/docs/offline-docker.md)。


## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `GIST_ADDR` | `:8080` | 监听地址 |
| `GIST_DATA_DIR` | `./data` | 数据目录 |
| `GIST_DB_PATH` | `$GIST_DATA_DIR/gist.db` | SQLite 数据库路径 |
| `GIST_STATIC_DIR` | 自动探测 `frontend/dist` | 后端静态文件目录 |
| `GIST_EXPORT_DIR` | `$GIST_DATA_DIR/exports` | 普通文章 Markdown 导出目录 |
| `GIST_PROMPTS_DIR` | `$GIST_DATA_DIR/prompts` | AI Prompt 模板目录 |
| `GIST_LOG_LEVEL` | `info` | 日志级别，支持 `debug` / `info` / `warn` / `error` |
| `GIST_SWAGGER` | `false` | 是否启用 Swagger |

## 本地开发

### 前置依赖

- Go 1.25+
- [Bun](https://bun.sh/)

### 方式一：前后端分离开发

适合日常开发。前端使用 Vite 开发服务器，`/api` 会自动代理到后端 `http://localhost:8080`。

终端 1：启动后端 API

```bash
cd /Users/usr/Gist-bg/backend
go mod download
go run ./cmd/server/main.go
```

终端 2：启动前端开发服务器

```bash
cd /Users/usr/Gist-bg/frontend
bun install
bun run dev
```

然后访问 Vite 输出的本地地址，通常是 [http://localhost:5173](http://localhost:5173)。

首次进入后可在设置页继续完成：

- AI 翻译 / AI 分析 / AI 日报模型配置
- AI 日报共享访问密钥配置
- 全局 AI 分析归档目录配置
- 文件夹级 AI 归档目录配置

### 方式二：前端先构建，后端直接托管静态文件

适合接近生产环境的本地联调。

先构建前端：

```bash
cd /Users/usr/Gist-bg/frontend
bun install
bun run build
```

再启动后端：

```bash
cd /Users/usr/Gist-bg
GIST_STATIC_DIR=/Users/usr/Gist-bg/frontend/dist \
GIST_EXPORT_DIR=/Users/usr/gist-data \
GIST_PROMPTS_DIR=/Users/usr/gist-data/prompts \
go run ./backend/cmd/server/main.go
```

启动后直接访问 [http://localhost:8080](http://localhost:8080)。

### 前端单独构建

```bash
cd /Users/usr/Gist-bg/frontend
bun install
bun run build
```

### 后端单独构建

```bash
cd /Users/usr/Gist-bg/backend
go build -o gist-server ./cmd/server
```

## 测试

### 后端

```bash
cd /Users/usr/Gist-bg/backend
make test
make lint
```

### 前端

```bash
cd /Users/usr/Gist-bg/frontend
bun run test
bun run lint
```

## 许可证

[GPL-2.0](/Users/usr/Gist-bg/LICENSE)
