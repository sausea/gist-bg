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
- AI 分析库页面，集中查看已入库的 AI 分析结果
- AI 日报页面，基于已入库分析结果按日聚合
- AI 日报与 AI 分析库支持通过共享 API Key 免登录供外部系统调用
- AI 分析结果会在数据库中持久化，分析标题会翻译为中文后再用于入库展示
- AI 分析完成后自动归档为 Markdown 文件，支持全局目录与文件夹级目录，按日期 / 订阅文件夹 / 订阅源保存
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
- 默认归档根目录为 `/Users/usr/gist-data`
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


### 离线部署（无外网）

当部署服务器无法访问外网时，请在有网络的机器上构建镜像并 `docker save` 导出，然后在服务器上 `docker load` 导入运行。

详见 [offline-docker.md](/Users/usr/Gist-bg/docs/offline-docker.md)。


## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `GIST_ADDR` | `:8080` | 监听地址 |
| `GIST_DATA_DIR` | `./data` | 数据目录 |
| `GIST_DB_PATH` | `$GIST_DATA_DIR/gist.db` | SQLite 数据库路径 |
| `GIST_STATIC_DIR` | 自动探测 `frontend/dist` | 后端静态文件目录 |
| `GIST_EXPORT_DIR` | `$GIST_DATA_DIR/exports` | 普通文章 Markdown 导出目录 |
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
