# Docker 化部署设计

## 目标

为 `miaodi-agent` 提供可复现的容器化构建与本地运行方案：

- 编写 `Dockerfile` 构建精简镜像
- 编写 `docker-compose.yml` 编排应用与依赖
- 提供 `.env.example` 配置模板
- 实际执行 `docker build` 验证

## 设计决策

### 1. Dockerfile

- **多阶段构建**：使用 `golang:1.26.3-alpine` 编译，运行阶段使用 `alpine:latest`
- **产物**：仅复制编译后的 `miaodi-agent` 二进制与必要的 `ca-certificates`
- **用户**：以非 root 用户运行（`appuser`）
- **端口**：暴露 `8080`

### 2. docker-compose.yml

- 服务：
  - `app`：构建当前目录，通过 `.env` 注入环境变量
  - `redis`：`redis:7-alpine`，供应用本地缓存/会话使用
- 不启动 MySQL，由用户配置远程数据库地址
- 使用 `depends_on` + `condition: service_healthy` 等待 Redis 就绪

### 3. 配置

- 环境变量覆盖 `config.yaml` 中的同名项
- 提供 `.env.example`，用户复制为 `.env` 后修改

## 运行方式

```bash
cp .env.example .env
# 编辑 .env，填入远程 MySQL、OpenAI API Key 等
docker compose up --build
```

## 弃用方案

- 单阶段 `golang` 镜像：体积过大
- `scratch`/`distroless`：调试不便，暂不使用
