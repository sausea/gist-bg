# 离线打包 / 离线部署 Docker 镜像

部署服务器无外网时，推荐做法是：**在有网络的机器上构建镜像** → `docker save` 导出为 `tar(.gz)` → 拷贝到服务器 → `docker load` 导入运行。

这样服务器侧**不需要联网拉依赖**（也不需要 `docker pull`）。

## 1) 推荐：使用仓库脚本一键打包

在仓库根目录执行：

```bash
cd /Users/usr/Gist-bg

# 常见 x86_64 Linux 服务器
./scripts/build_offline_bundle.sh amd64

# ARM64 Linux 服务器
# ./scripts/build_offline_bundle.sh arm64
```

输出目录：

```text
/Users/usr/Gist-bg/dist/offline/
```

生成文件示例：

```text
gist-v1.0.1-offline_linux-amd64.tar.gz
gist-v1.0.1-offline_linux-amd64.tar.gz.sha256
```

## 2) 手工构建并导出镜像

在仓库根目录执行：

```bash
cd /Users/usr/Gist-bg

# 根据你的服务器架构选择其一：
# amd64 (常见云服务器)
docker build --platform linux/amd64 -f docker/Dockerfile \
  -t gist:offline-amd64 \
  -t gist:1.0.1-offline-amd64 .
# arm64 (例如部分 ARM 服务器)
# docker build --platform linux/arm64 -f docker/Dockerfile \
#   -t gist:offline-arm64 \
#   -t gist:1.0.1-offline-arm64 .

# 导出为离线包（包含基础镜像层，不需要服务器再 pull）
docker save gist:offline-amd64 gist:1.0.1-offline-amd64 | gzip -9 > gist-v1.0.1-offline_linux-amd64.tar.gz
shasum -a 256 gist-v1.0.1-offline_linux-amd64.tar.gz > gist-v1.0.1-offline_linux-amd64.tar.gz.sha256
```

把 `gist-v*.tar.gz` 和对应的 `.sha256` 传到服务器（U 盘 / scp / 内网制品库均可）。

## 3) 在离线服务器上导入镜像

```bash
sha256sum -c gist-v1.0.1-offline_linux-amd64.tar.gz.sha256
gunzip -c gist-v1.0.1-offline_linux-amd64.tar.gz | docker load
```

导入后确认镜像存在：

```bash
docker images | grep 'gist'
```

## 4) 运行容器（数据持久化 + Markdown 导出）

以下示例以 Linux 服务器路径 `/opt/gist-data` 为例（按需修改）。

```bash
mkdir -p /opt/gist-data

docker run -d --name gist \
  -p 8080:8080 \
  -v /opt/gist-data:/app/data \
  -e GIST_LOG_LEVEL=info \
  -e GIST_EXPORT_DIR=/app/data \
  gist:offline-amd64
```

说明：
- `/app/data` 会保存 `gist.db`、图标缓存、以及默认导出目录等。
- `GIST_EXPORT_DIR=/app/data` 表示 Markdown 直接保存到挂载出来的宿主目录里（例如 `/opt/gist-data/2026-03-03.md`）。

## 5) 用 docker compose（可选）

如果你更习惯 compose，可以使用仓库里的 `docker-compose.offline.yml`：

```bash
docker compose -f docker-compose.offline.yml up -d
```

> 注意：离线模式下不要使用指向 `ghcr.io/...` 的 compose 文件，否则会尝试拉取远端镜像。

如果你导入的是 ARM64 包，可以在启动时覆盖镜像标签：

```bash
GIST_IMAGE=gist:offline-arm64 docker compose -f docker-compose.offline.yml up -d
```
