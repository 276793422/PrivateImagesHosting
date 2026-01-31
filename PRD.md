# HTTP 图床工具 - 产品需求文档 (PRD)

## 1. 项目概述

### 1.1 项目名称
HTTP Image Hosting (HttpImageHosting)

### 1.2 项目简介
一个轻量级的跨平台文件托管解决方案，包含 Linux 服务端和 Windows 客户端，支持文件上传、下载及自动过期清理功能。

### 1.3 项目目标
- 提供简单易用的文件上传和下载服务
- 支持自定义文件存储路径和文件名
- 自动清理过期文件，节省服务器存储空间
- 提供命令行客户端，便于脚本化和自动化集成

---

## 2. 系统架构

### 2.1 架构图
```
┌─────────────────┐         HTTP               ┌─────────────────┐
│  Windows 客户端 │ ──────────────────────────> │  Linux 服务端    │
│  (http-cli.exe) │    上传(需认证)/下载(公开)   │  (httpserver)   │
└─────────────────┘                              └─────────────────┘
      │                                                │
      │                                                ▼
      │                                        ~/HttpServer/Images/
      │                                        └── 20260131/
      └────────────────────────────────────────>     └── 20260131-143022123-0.jpg
                 任意 HTTP 客户端可直接下载
```

**访问控制说明：**
- 上传操作：需要客户端认证（使用 API Key）
- 下载操作：公开访问，任意 HTTP 客户端可通过 URL 直接获取
- 文件列表：需要访问密码认证（弹窗输入密码，Cookie 会话管理，5 分钟超时）
- Web 管理界面：需要用户名/密码认证
- 删除操作：需要客户端认证

**目录结构：**
```
# Linux
~/HttpServer/
├── httpserver          # 服务端可执行程序（单文件，可放任意位置）
├── config.json         # 配置文件
├── metadata.db         # SQLite 数据库（元数据、索引等）
└── Images/             # 文件存储根目录（服务器根目录）
    └── 20260131/       # 按日期分目录
        └── 20260131-143022123-0.jpg

# Windows
<程序所在目录>\
├── httpserver.exe      # 服务端可执行程序
├── config.json         # 配置文件
├── metadata.db         # SQLite 数据库
└── Images\             # 文件存储根目录
    └── 20260131\
        └── 20260131-143022123-0.jpg
```

**说明：**
- 服务端程序可放置在任意目录执行
- Linux：数据目录固定为 `~/HttpServer/`
- Windows：数据目录为程序所在目录
- 文件存储根目录为 `{数据目录}/Images/`（下载 URL 的根路径对应此目录）
- 网页内容（list.html、manager.html）嵌入在服务端程序内部
- Linux 版本支持 systemd 服务管理
- Windows 版本仅作为控制台应用运行

### 2.2 技术栈
| 组件 | 平台 | 技术选型 |
|------|------|----------|
| 服务端 | Linux / Windows | Go (单文件可执行程序，跨平台编译) |
| 客户端 | Windows | Go (单文件可执行程序 http-cli.exe) |
| 通信协议 | - | HTTP/1.1 |
| 文件存储 | - | 文件系统 |
| 元数据存储 | - | SQLite |
| 服务管理 | Linux | systemd (内置自安装/卸载) |
| 服务管理 | Windows | 控制台应用（无服务安装功能） |
| 会话管理 | - | Cookie |
| 认证方式 | CLI | API Key |
| 认证方式 | Web | 用户名/密码 / 访问密码 |

---

## 3. 功能需求

### 3.1 服务端功能 (Linux)

#### 3.1.1 HTTP 服务
| 功能 | 访问路径 | 认证要求 | 描述 |
|------|----------|----------|------|
| 文件上传 | POST /upload | API Key | 接收客户端上传的文件，自动重命名和分目录 |
| 文件下载 | GET /files/{path} | 无 | 公开访问，返回文件内容 |
| 文件列表 | GET /list.html | 访问密码 | Cookie 会话，5 分钟超时 |
| 文件列表 API | GET /api/files | Cookie 会话 | 返回文件列表 JSON |
| 登录验证 | POST /api/login | 无 | 验证访问密码，返回 Cookie |
| Web 管理界面 | GET /manager.html | 用户名/密码 | 后台管理功能 |
| 健康检查 | GET /health | 无 | 服务状态检查接口 |
| 文件删除 | DELETE /files/{path} | API Key | 删除指定文件 |

#### 3.1.2 API 接口设计

**上传文件**
```
POST /upload
Content-Type: multipart/form-data
X-API-Key: <your-api-key>    # 必需，客户端认证

请求参数:
- file: 文件内容
- filename: 原始文件名（如 photo.jpg）
- ttl: 必需，文件存活时间（小时），最大值 8760（365天），默认值 1

服务端处理流程:
1. 验证 API Key
2. 获取当前日期（如 20260131）
3. 从数据库获取当日索引，递增并持久化
4. 生成新文件名：YYYYMMDD-HHMMSSmmm-index.ext
   - 例如：20260131-143022123-0.jpg
5. 创建 ~/HttpServer/Images/YYYYMMDD/ 目录（如不存在）
6. 保存文件到 ~/HttpServer/Images/YYYYMMDD/YYYYMMDD-HHMMSSmmm-index.ext
7. 记录元数据到数据库

响应:
{
  "success": true,
  "message": "File uploaded successfully",
  "file_path": "20260131/20260131-143022123-0.jpg",
  "download_url": "http://server:port/files/20260131/20260131-143022123-0.jpg",
  "expires_at": "2026-02-02T10:00:00Z"
}

错误响应 (401):
{
  "success": false,
  "message": "Invalid or missing API key"
}

错误响应 (400):
{
  "success": false,
  "message": "TTL exceeds maximum value of 8760 hours (365 days)"
}
```

**文件重命名规则（已确定）：**
- 格式：`YYYYMMDD-HHMMSSmmm-index.ext`
- YYYYMMDD：年月日（8 位）
- HHMMSSmmm：时分秒毫秒（10 位）
- index：当日上传文件索引，从 0 开始，每天 0 点重置，持久化存储
- ext：原始文件扩展名
- 示例：`20260131-143022123-0.jpg`

**文件存储规则（已确定）：**
- 所有文件按日期分目录存储
- 目录路径：`~/HttpServer/Images/YYYYMMDD/`
- 完整路径：`~/HttpServer/Images/YYYYMMDD/YYYYMMDD-HHMMSSmmm-index.ext`
- 客户端无法指定存储路径，由服务端统一管理

**下载文件** (公开访问，无需认证)
```
GET /files/{file_path}

示例: /files/20260131/20260131-143022123-0.jpg

响应:
- 成功: 文件二进制内容 + Content-Type
- 失败: 404 Not Found

说明: 任意 HTTP 客户端均可通过 URL 直接访问下载
```

**登录验证** (文件列表访问认证)
```
POST /api/login
Content-Type: application/json

请求:
{
  "password": "490003219"
}

成功响应 (200):
Set-Cookie: session_token=<token>; Max-Age=300; Path=/; HttpOnly
{
  "success": true
}

失败响应 (401):
{
  "success": false,
  "message": "Invalid password"
}

说明:
- 密码固定为 490003219
- 成功后设置 Cookie，有效期 300 秒（5 分钟）
- 后续请求携带 Cookie 即可访问受保护资源
```

**删除文件**
```
DELETE /files/{file_path}
X-API-Key: <your-api-key>    # 必需，客户端认证

响应:
{
  "success": true,
  "message": "File deleted successfully"
}

错误响应 (401):
{
  "success": false,
  "message": "Invalid or missing API key"
}
```

**健康检查**
```
GET /health

响应:
{
  "status": "ok",
  "uptime": 3600,
  "storage_info": {
    "total_files": 100,
    "total_size": "1.2GB"
  }
}
```

**文件列表 API** (需要 Cookie 认证)
```
GET /api/files
Cookie: session_token=<token>

查询参数:
- path: 可选，指定目录路径（如 20260131），默认为根目录

响应:
{
  "success": true,
  "current_path": "20260131",
  "files": [
    {
      "name": "20260131-143022123-0.jpg",
      "path": "20260131/20260131-143022123-0.jpg",
      "size": 1024000,
      "created_at": "2026-01-31T14:30:22Z",
      "expires_at": "2026-02-01T14:30:22Z",
      "original_filename": "photo.jpg",
      "ttl": 24
    }
  ],
  "directories": [
    {
      "name": "20260131",
      "path": "20260131"
    },
    {
      "name": "20260201",
      "path": "20260201"
    }
  ]
}
```

**文件列表页面** (需要访问密码认证)
```
GET /list.html

返回: HTML 页面（嵌入在服务端程序内）

功能:
  - 访问前弹窗要求输入密码（490003219）
  - Cookie 会话管理，5 分钟无操作超时
  - 提供退出登录按钮
  - 浏览日期目录结构
  - 查看文件信息（大小、创建时间、过期时间、原始文件名）
  - 点击文件名直接下载

认证流程:
  1. 访问 /list.html
  2. 检查 Cookie 是否有效
  3. 若无效，显示登录弹窗
  4. 用户输入密码，POST /api/login
  5. 成功后设置 Cookie，刷新页面显示文件列表
```

**Web 管理界面** (需要用户名/密码认证)
```
GET /manager.html

认证信息 (HTTP Basic Auth):
  用户名: 276793422
  密码: 490003219

功能模块:

1. 文件管理
   - 浏览所有文件和目录
   - 删除文件
   - 查看文件详细信息

2. 系统配置管理
   - 修改 API Key（CLI 客户端认证密钥）
   - 设置 IP 白名单（允许上传/删除的 IP 地址列表）
   - 设置速率限制（每 IP 每分钟请求数）
   - 设置最大文件大小限制

3. 统计信息
   - 文件总数、总大小
   - 存储使用情况
   - 服务运行时间

4. 日志查看
   - 查看最近的操作日志
   - 查看错误日志

管理界面 API (需要管理员认证):

获取系统配置:
GET /api/admin/config
Authorization: Basic <base64(username:password)>

更新系统配置:
PUT /api/admin/config
Authorization: Basic <base64(username:password)>
Content-Type: application/json

{
  "api_key": "new-api-key",
  "ip_whitelist": ["192.168.1.0/24", "10.0.0.1"],
  "rate_limit": 60,
  "max_file_size": 104857600
}

获取系统统计:
GET /api/admin/stats
Authorization: Basic <base64(username:password)>

获取操作日志:
GET /api/admin/logs?limit=100
Authorization: Basic <base64(username:password)>
```

#### 3.1.3 文件清理功能
| 功能 | 描述 |
|------|------|
| 定时扫描 | 每小时扫描一次文件存储目录 |
| 过期判断 | 根据文件创建时间和 TTL 判断是否过期 |
| 自动删除 | 删除过期文件及其空目录 |
| 日志记录 | 记录清理操作日志 |

**TTL 计算方式（已确定）：**
- 文件上传时由客户端指定 TTL（小时）
- TTL 默认值：1 小时
- TTL 最大值：8760 小时（365 天）
- 服务端记录文件的创建时间和过期时间
- 过期时间 = 创建时间 + TTL
- 清理任务每小时执行一次，删除所有过期文件
- 清理时无需通知，仅记录日志

#### 3.1.4 配置管理

**配置文件位置：**
- Linux：配置文件 `~/HttpServer/config.json`，数据库 `~/HttpServer/metadata.db`
- Windows：配置文件 `<程序所在目录>\config.json`，数据库 `<程序所在目录>\metadata.db`
- 首次运行时自动创建默认配置

**端口设置：**
- 配置文件中的 `server.port` 为默认端口（8080）
- 命令行参数 `-p` 可覆盖配置文件中的端口设置
- 使用 `-i` 安装服务时，指定的端口会持久化到配置文件
- 支持任意端口（如 80、8080、9000 等）

**配置文件格式** (~/HttpServer/config.json):
```json
{
  "server": {
    "host": "0.0.0.0",
    "port": 8080
  },
  "storage": {
    "images_dir": "~/HttpServer/Images",
    "max_file_size": 104857600,
    "cleanup_interval": 60,
    "default_ttl": 1,
    "max_ttl": 8760
  },
  "auth": {
    "api_key": "your-api-key-here",
    "admin_username": "276793422",
    "admin_password": "490003219",
    "list_password": "490003219"
  },
  "security": {
    "ip_whitelist": [],
    "rate_limit_per_minute": 60,
    "session_timeout": 300
  },
  "database": {
    "path": "~/HttpServer/metadata.db"
  },
  "auto_restart": {
    "enabled": true,
    "max_restart_count": 10
  }
}
```

**配置项说明:**

| 配置项 | 描述 | 默认值 |
|--------|------|--------|
| server.host | HTTP 服务监听地址 | 0.0.0.0 |
| server.port | HTTP 服务监听端口 | 8080 |
| storage.images_dir | 文件存储根目录 | ~/HttpServer/Images |
| storage.max_file_size | 单文件上传大小限制（字节） | 104857600 (100MB) |
| storage.cleanup_interval | 文件清理扫描间隔（分钟） | 60 |
| storage.default_ttl | 默认 TTL（小时） | 1 |
| storage.max_ttl | 最大 TTL（小时） | 8760 (365天) |
| auth.api_key | CLI 客户端认证密钥 | 必须配置 |
| auth.admin_username | Web 管理界面用户名 | 276793422 |
| auth.admin_password | Web 管理界面密码 | 490003219 |
| auth.list_password | 文件列表访问密码 | 490003219 |
| security.ip_whitelist | IP 白名单（空数组表示不限制） | [] |
| security.rate_limit_per_minute | 每分钟请求限制 | 60 |
| security.session_timeout | 会话超时时间（秒） | 300 |
| auto_restart.enabled | 是否启用自动重启 | true |
| auto_restart.max_restart_count | 最大自动重启次数 | 10 |

**注意：**
- 配置文件可通过 Web 管理界面修改，存储在数据库中
- IP 白名单、速率限制、API Key 等可动态修改，无需重启服务

#### 3.1.5 日志功能
| 日志类型 | 内容 |
|----------|------|
| 访问日志 | 记录每个 HTTP 请求 |
| 上传日志 | 记录文件上传操作 |
| 清理日志 | 记录文件清理操作 |
| 错误日志 | 记录系统错误信息 |

### 3.2 客户端功能 (Windows)

**程序名称：** `http-cli.exe`

#### 3.2.1 命令行格式

**基本语法：**
```
http-cli <文件路径> -a <token> [-t <ttl>] [-s <server>]
```

| 参数 | 简写 | 必需 | 描述 | 示例 |
|------|------|------|------|------|
| 文件路径 | - | 是 | 本地文件路径（绝对或相对） | C:/Zoo/photo.jpg 或 photo.png |
| -a | --auth | 是 | API 认证密钥（token） | my-secret-token |
| -t | --ttl | 否 | 文件存活时间（小时），默认 1 | 24 |
| -s | --server | 否 | 服务端地址，默认 http://localhost:8080 | http://192.168.1.100:8080 |
| -h | --help | 否 | 显示帮助信息 | - |
| -v | --version | 否 | 显示版本信息 | - |

**使用示例：**
```bash
# 上传文件（绝对路径）
http-cli C:/Zoo/photo.jpg -a my-token

# 上传文件（相对路径）
http-cli photo.png -a my-token

# 上传文件并指定 TTL
http-cli photo.jpg -a my-token -t 24

# 上传文件并指定服务器
http-cli photo.jpg -a my-token -s http://192.168.1.100:8080

# 完整示例
http-cli C:/Users/Zoo/Documents/image.png -a abc123def456 -t 48 -s http://10.0.0.50:8080
```

#### 3.2.2 客户端行为

**上传流程：**
1. 读取本地文件
2. 提取文件名（如 photo.jpg）
3. 向服务端发送上传请求（携带 file、filename、api_key、ttl）
4. 接收服务端响应
5. 输出服务端返回的文件路径到命令行
6. 退出

**成功输出示例：**
```
$ http-cli photo.jpg -a my-token -t 24
20260131/20260131-143022123-0.jpg
```

**错误输出示例：**
```
$ http-cli photo.jpg -a wrong-token
Error: Invalid or missing API key
```

**注意：**
- 客户端上传完成后立即退出，不保持连接
- 不支持下载、删除、查看信息等其他操作
- 只上传文件，无法指定服务端存储路径

#### 3.2.3 进度显示
- 上传进度条
- 传输速度显示
- 预计剩余时间

---

## 3.3 服务端部署

### 3.3.1 服务端程序

**程序名称：**
- Linux：`httpserver`
- Windows：`httpserver.exe`

**支持平台：**
- Linux（amd64、arm64）
- Windows（amd64）

**运行模式：**
- 普通模式：直接运行服务
- 安装模式：使用 `-i` 参数安装为服务（仅 Linux）
- 卸载模式：使用 `-u` 参数卸载服务（仅 Linux）

**命令行参数：**
| 参数 | 平台 | 描述 |
|------|------|------|
| -i, --install | Linux | 安装 systemd 服务（自动创建 unit 文件并启用） |
| -u, --uninstall | Linux | 卸载 systemd 服务 |
| -p, --port | 全部 | 指定监听端口（覆盖配置文件设置） |
| -c, --config | 全部 | 指定配置文件路径 |
| --no-restart | Linux | 禁用自动重启功能 |
| -h, --help | 全部 | 显示帮助信息 |
| -v, --version | 全部 | 显示版本信息 |

**使用示例：**

```bash
# Linux - 直接运行（前台）
./httpserver

# Linux - 指定端口运行
./httpserver -p 80

# Linux - 安装为系统服务
./httpserver -i

# Linux - 卸载服务
./httpserver -u

# Windows - 直接运行
httpserver.exe

# Windows - 指定端口运行
httpserver.exe -p 8080
```

### 3.3.2 平台差异

**Linux 平台特性：**
- 支持 systemd 服务安装/卸载
- 自动重启机制（通过 systemd）
- 数据目录：`~/HttpServer/`

**Windows 平台特性：**
- 仅支持控制台模式运行
- 无服务安装功能
- 数据目录：程序所在目录
- 可配置为开机启动（通过其他方式，如任务计划程序）

### 3.3.3 自动重启机制（Linux）

**实现方式：**
- 使用 systemd 服务配置 Restart=always
- 服务异常退出后 systemd 自动重启
- 可通过配置文件或命令行参数控制

**systemd 服务配置（程序自动生成）：**
```ini
[Unit]
Description=HTTP Image Hosting Server
After=network.target

[Service]
Type=simple
User=<current_user>
WorkingDirectory=<executable_directory>
ExecStart=<full_path_to_httpserver> --config <config_path>
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

**端口持久化机制：**
- 安装服务时，程序会将当前端口设置保存到配置文件
- systemd 服务启动时，程序读取配置文件中的端口设置
- 即使通过 `-p` 参数指定端口，安装后也会持久化到配置文件

**安装流程：**
1. 检查是否已存在配置文件，不存在则创建默认配置
2. 如果命令行指定了 `-p` 参数，将端口写入配置文件
3. 生成 systemd unit 文件，使用 `--config` 参数指定配置文件路径
4. 执行 `systemctl daemon-reload`、`systemctl enable httpserver`、`systemctl start httpserver`

**卸载流程：**
1. 执行 `systemctl stop httpserver`
2. 执行 `systemctl disable httpserver`
3. 删除 `/etc/systemd/system/httpserver.service` 文件
4. 执行 `systemctl daemon-reload`
5. 保留配置文件和数据目录（不删除）

**说明：**
- `-i` 参数触发服务安装流程
- `-u` 参数触发服务卸载流程
- 端口设置通过配置文件持久化，服务重启后保持不变

---

## 4. 非功能需求

### 4.1 性能要求
| 指标 | 要求 |
|------|------|
| 并发上传 | 支持至少 10 个并发上传 |
| 单文件大小 | 支持 100MB 以内文件 |
| 响应时间 | API 响应时间 < 100ms (不含传输时间) |

### 4.2 安全要求
| 功能 | 描述 |
|------|------|
| 路径遍历防护 | 防止 `../` 等路径遍历攻击 |
| 上传认证 | 上传操作必须通过 X-API-Key Header 认证 |
| 删除认证 | 删除操作必须通过 X-API-Key Header 认证 |
| 管理认证 | Web 管理界面使用 HTTP Basic Auth 认证 |
| 下载公开 | 下载操作无需认证，任意 HTTP 客户端可访问 |
| IP 白名单 | 可配置 IP 白名单限制上传/删除操作 |
| 速率限制 | 可配置每 IP 每分钟请求次数限制 |
| TTL 限制 | TTL 最大值 8760 小时（365 天） |
| 访问日志 | 记录所有访问操作用于审计 |

**注意：**
- 不限制文件类型，任意类型文件均可上传
- 无用户隔离机制，所有认证客户端共享同一存储空间
- IP 白名单和速率限制可通过 Web 管理界面动态配置

### 4.3 可靠性要求
| 功能 | 描述 |
|------|------|
| 断点续传 | 可选：支持大文件断点续传 |
| 错误重试 | 客户端自动重试失败的请求 |
| 优雅关闭 | 服务端支持信号处理，优雅关闭 |

### 4.4 可维护性要求
| 功能 | 描述 |
|------|------|
| 清晰的日志 | 结构化日志输出 |
| 配置热更新 | 支持配置文件修改后自动重载 |
| 版本信息 | 提供 version 命令查看版本 |

---

## 5. 待讨论的细节问题

### 5.1 技术选型 ✅ 已确定
- [x] 服务端开发语言：Go
- [x] 客户端开发语言：Go
- [x] 不使用 HTTPS
- [x] 元数据存储：SQLite
- [x] 服务管理：systemd（内置自安装）

### 5.2 功能范围 ✅ 已确定
- [x] 不需要用户系统（无用户隔离）
- [x] 需要 Web 管理界面（/manager.html）
- [x] 需要文件列表功能（/list.html）
- [x] 文件列表需要访问密码认证
- [x] 文件自动重命名（按规则）
- [x] 文件按日期分目录存储
- [x] 服务端内置 Web 内容
- [x] 服务端单文件可执行程序
- [x] 客户端仅支持上传功能
- [x] 清理无需通知
- [x] 不需要图片缩略图功能

### 5.3 文件命名与存储 ✅ 已确定
- [x] 重命名格式：YYYYMMDD-HHMMSSmmm-index.ext
- [x] 索引规则：每日 0 点重置，持久化存储
- [x] 存储规则：按日期分目录 ~/HttpServer/Images/YYYYMMDD/
- [x] 客户端无法指定路径

### 5.4 TTL 设计 ✅ 已确定
- [x] 上传时由客户端指定 TTL
- [x] TTL 默认值：1 小时
- [x] TTL 最大值限制：8760 小时（365 天）

### 5.5 安全性 ✅ 已确定
- [x] 上传/删除需要 API 认证
- [x] 不限制上传文件类型
- [x] 下载公开访问
- [x] 文件列表需要访问密码（Cookie 会话，5 分钟超时）
- [x] IP 白名单（由 Web 管理界面配置）
- [x] 速率限制（由 Web 管理界面配置）
- [x] Web 管理界面使用用户名/密码认证

### 5.6 部署方式 ✅ 已确定
- [x] 使用 systemd，内置自安装/卸载功能（-i / -u 参数，仅 Linux）
- [x] 端口设置持久化到配置文件，服务重启后保持不变
- [x] 服务端支持跨平台编译（Linux、Windows）
- [x] 服务端支持命令行指定端口（-p 参数）
- [x] 不需要 Docker 支持
- [x] 客户端单文件可执行程序
- [x] 服务端单文件可执行程序
- [x] Linux 数据目录：~/HttpServer/
- [x] Windows 数据目录：程序所在目录

### 5.7 配置管理 ✅ 已确定
- [x] 服务端使用 JSON 配置文件
- [x] 配置文件位置：~/HttpServer/config.json
- [x] 客户端使用命令行参数，不需要配置文件
- [x] Web 管理界面可动态修改配置

---

## 6. 里程碑

| 阶段 | 内容 | 状态 |
|------|------|------|
| 第一阶段 | 需求确认，技术选型确定 | ✅ 完成 |
| 第二阶段 | 服务端基础功能开发 | 待开始 |
| 第三阶段 | 文件重命名、索引管理、日期目录功能 | 待开始 |
| 第四阶段 | Web 界面开发（文件列表、管理界面） | 待开始 |
| 第五阶段 | 客户端开发 | 待开始 |
| 第六阶段 | systemd 自安装功能 | 待开始 |
| 第七阶段 | 联调与测试 | 待开始 |
| 第八阶段 | 优化与文档完善 | 待开始 |

---

## 7. 文档版本

| 版本 | 日期 | 修改内容 | 作者 |
|------|------|----------|------|
| 1.0 | 2026-01-31 | 初始版本 | Claude |
| 1.1 | 2026-01-31 | 确定技术选型（Go）、认证机制、TTL方式 | Claude |
| 1.2 | 2026-01-31 | 确定 Web 管理界面、文件列表、SQLite、配置格式等 | Claude |
| 1.3 | 2026-01-31 | 确定文件重命名规则、客户端命令格式、systemd自安装、文件列表认证等 | Claude |
| 1.4 | 2026-01-31 | 确定跨平台支持、服务卸载功能、端口配置 | Claude |
| 1.5 | 2026-01-31 | 修正 Windows 数据目录为程序所在目录，完善 systemd 端口持久化机制 | Claude |

---

*需求文档已完成，所有主要功能需求已确定*
