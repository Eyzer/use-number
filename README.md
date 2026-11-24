# use-number · 发文字号取号系统

单文件 Go 程序：内嵌前端页面，SQLite 存储，零外部依赖。局域网内网部署，前台免登录取号，后台管账。

## 功能

- 前台按类型领取发文字号，编号自动递增，永不重号（按最大序号兜底）
- 领取人必填，归档状态二选一（已归档 = 电子版已交保密室存档）
- 领取记录按号码数字倒序，分页、搜索、按归档状态筛选
- 批量导入历史编号（每行 `编号|标题`，支持 Tab / 空格分隔）
- 归档目录扫描：按文件名模糊匹配字号或标题，自动改归档状态
- 前台上传归档文件，按命名规则（`{字号}-{标题}`）校验并自动归档
- 导出 CSV（UTF-8 带 BOM，Excel 直开不乱码）
- 后台登录：本地账号 + DzzOffice 单点登录（读 `dzz_user` 表）

## 初始状态

数据库首次启动自动创建，只有管理员账号：

    账号 admin
    密码 admin123

登录后请立刻改密码，并在「发文字号类型」里新建类型。

## 编译

需要 Go 1.22+，纯 Go 驱动，无需 CGO：

    bash build.sh

产物在 `dist/`：Windows amd64、Linux arm64（麒麟）、Linux amd64。

## 部署

**Windows**：双击 `use-number-windows-amd64.exe`（或在同目录放 `启动.bat`）。

**麒麟 / 通用 Linux**：

    sudo bash install.sh 80

脚本装到 `/opt/use-number`，注册 systemd 服务 `use-number`，开机自启。卸载用 `uninstall.sh`。

## 数据

SQLite 文件在程序同级的 `data/use-number.db`，备份这一个文件即可。

## 目录

    main.go        启动、路由、内嵌静态资源
    store.go       建库、字段迁移、口令、会话
    auth.go        本地登录 + DzzOffice 登录
    api.go         业务接口
    archive.go     目录扫描匹配、CSV 导出
    upload.go      前台上传与命名规则
    web/           前端页面与静态资源
