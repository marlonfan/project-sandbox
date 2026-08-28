# 项目沙盘（project-sandbox）

> 以目标为分区俯瞰所有项目推进状态的个人工作台：目标 → 项目 → 事项三层结构，
> 5 秒扫一眼就知道哪些健康、哪些在着火；高频操作全部行内完成，不弹窗不打断。
>
> 两种形态：**单文件 HTML**（双击即用、离线可用、数据存本地）和 **Go 服务端版**
> （多用户注册登录 + 邮箱验证码 + 多沙盘 + 跨设备共享，SQLite 单文件存储）。

- 单文件：`project-sandbox.html`（约 84KB，双击即用，离线可用）
- 线上地址：
  - 全球：<https://static.zhire.de/product/project-sandbox.html>
  - 国内优选：<https://static.marlon.life/product/project-sandbox.html>

## 项目背景

这个项目最早是给自己用的：同时推进的目标太多——产品迭代、商业化、技术升级——
散落在各种笔记和脑子里，既看不清全局，也记不牢细节。市面上的项目管理工具要么太重
（工作流、权限、报表一堆用不上），要么太轻（只是个待办清单，没有「目标 → 项目」的归属感）。

于是从「一张俯瞰的沙盘」出发写了这个工具，坚持三个原则：

1. **单文件起步**：一个 HTML 内联全部 CSS/JS/SVG，零依赖零构建，数据存 localStorage，
   复制走就能用，放到任何静态托管就是完整产品；
2. **记录不折腾**：进度 = 事项完成率自动算，不手动维护百分比；所有修改防抖自动保存，
   高频操作（勾事项、切状态、写进展）全部行内完成；
3. **数据是自己的**：快照历史 + 自动写盘备份 + 导入导出 JSON，不做任何云端锁定。

后来需要在多台设备间共享数据，也想让家人朋友各自记录，于是加了可选的 Go 服务端：
注册登录（邮箱验证码）、每人独立沙盘空间、SQLite 单文件存储、编译后一个二进制即可部署。
单文件形态保留至今——服务端是可选增强，不是必经之路。

本文档同时是完整的项目交接记录（2026-08 会话全量沉淀）。

---

## 一、它解决什么问题

以**目标 → 项目 → 事项**三层结构管理个人工作：

- **目标（Goal）**：顶层分区，带颜色标识，可折叠
- **项目（Project）**：挂在目标下，横向一行一条，点击行内联展开编辑
- **事项（Issue）**：项目下的具体事情（用户心智模型：事项 = 一个一个的任务），勾选完成，可写进展记录

核心设计原则：**5 秒扫一眼就知道哪些健康、哪些在着火；高频操作全部行内完成，不弹窗不打断。**

## 二、功能全景

### 沙盘主视图
- 目标分区面板（白色卡片容器 + 彩色小圆点标识 + 加粗标题），支持一键折叠（状态记忆，刷新不丢）
- 项目行：`状态(圆点+灰字,点击切换) · 名称(长名自动换行) · 进度条 · 完成率 · 负责人 · 截止 · 事项数 · 展开箭头`
- **进度 = 事项完成率**（已完成/总数），无事项显示 `—`，不依赖手动维护百分比
- 未完成事项直接铺在行下方（优先级色点排序），可直接勾选完成、原地编辑文字、悬停删除、行内追加进展
- 项目描述 / 目标描述以灰色小字展示在各自名称下方
- 拖拽项目行跨目标移动 + 分区内精确插入排序（拖到某行上半=插前面，下半=插后面）；**顺序只在拖动时变化**（稳定排序，改进度/切状态不会跳）

### 编辑体验
- 所有修改**自动保存**（防抖 400ms），右上角绿字「已保存 HH:MM」
- 输入框回车即提交：事项添加、进展记录、确认弹窗、抽屉字段全部支持
- 展开编辑区出现在项目行正下方（位置固定，不随事项数量漂移），打字过程不丢焦点

### 数据安全（三层保护）
1. **快照历史**：每次保存自动留档整个沙盘状态，保留最近 **20 个版本**，「历史」按钮一键回滚（恢复前当前状态也会先存档，可再切回）
2. **自动备份**：「自动备份」按钮绑定本地 .json 文件（File System Access API，Chrome/Edge），每次保存自动写入；按钮绿点=正常 / 黄点=需重新授权。Safari/Firefox 降级为「超 7 天未导出提醒」
3. **损坏隔离**：localStorage 数据损坏时先隔离到 `-corrupt-backup` 键再重建，绝不静默覆盖

### 事项动态（顶栏趋势图标）
按事项分组的**进展时间线对比**：每个有记录的事项一张卡，从最早记录看到最新（最新高亮），按最近活跃排序；没记录的事项列在底部。点文字可编辑、悬停可删除。

### 其他
- 搜索（名称/负责人）+ 状态筛选：**不匹配的直接隐藏**，筛空的目标整个隐藏，全空显示提示
- 导出/导入 JSON（导入前自动存历史版本）
- 底部留白充足；窄屏响应式（≤900px 单列堆叠）

## 三、技术要点

| 项 | 说明 |
|---|---|
| 架构 | 单 HTML 文件，CSS/JS/SVG 全内联，零外部依赖，file:// 直接打开 |
| 存储 | localStorage `project-sandbox-v1`（数据）、`project-sandbox-history-v1`（版本）、`psb-collapsed`（折叠状态）、`psb-last-export` |
| 文件写盘 | File System Access API，句柄存 IndexedDB `psb-meta`；Chrome 122+ 权限跨会话持久 |
| XSS | 所有用户输入经 `esc()` 转义后渲染 |
| 兼容 | color-mix / FS Access API 需要较新内核；非 Chromium 自动降级 |

数据模型（normalize 后）：

```
Goal:    { id, name, description, color, createdAt }
Project: { id, goalId, name, owner, progress(遗留字段,已不用), status: on-track|at-risk|blocked|paused|done,
           deadline, description, createdAt, updatedAt, order, snaps(遗留) }
Issue:   { id, projectId, title, severity: high|medium|low, resolved, createdAt, notes: [{ts, text}] }
```

## 四、关键设计决策（会话演进记录）

1. **事项 = 任务的心智模型**：最初叫「问题」（风险语义，红色三角警告），用户明确"问题其实就是一个一个的事情"后，全面改称「事项」，去掉警报式视觉（三角图标、脉冲红点、高危红边框），优先级仅用小色点表达
2. **进度自动化**：手动百分比被废弃（"记录不下来，我还是手动描述吧"），改为完成率自动计算；项目级打点功能移除，改为**事项级手动进展描述**
3. **稳定排序**：曾按状态+进度自动排序导致卡片跳位，改为 order 字段手动排序，只有拖拽才改变顺序
4. **横排布局**：从大卡片网格改为全宽分区 + 行式布局 + 行内展开，右侧抽屉已整体移除
5. **视觉克制（Claude 风）**：暖象牙底 #FAF9F5、白卡片、赤陶色 #D97757 仅用于主按钮和聚焦态；曾尝试给目标加彩色底/彩边被否（"浮夸"），最终层次靠字号字重和中性色实现
6. **筛选即隐藏**：不做灰显，直接不渲染
7. **单文件坚持**：拒绝了后端方案，用 快照历史 + FS Access API 文件备份 组合保证稳妥性

## 五、踩过的坑（后续改动注意）

- **动态元素不能静态绑事件**：曾对抽屉动态创建的表单静态 addEventListener 导致启动即崩（按钮文字全空）。修复方式：document 级委托。新增动态元素交互一律走委托
- **change 监听器曾重复绑定**：重构时新旧两份并存导致 toast 弹两次，改监听器前先 grep 确认唯一性
- **展开区重渲染与焦点冲突**：编辑面板在 board 内，autosave 全量重渲染会丢焦点，用 `renderAllKeepFocus()`（记录 activeElement + 光标位置）解决
- **file:// 是唯一安全源**：表单裸提交会报 unsafe load，所有 form 必须 preventDefault

## 六、部署与更新

上传到 Cloudflare Worker 文件服务（cf-file-upload skill）：

```bash
python3 ~/.claude/skills/cf-file-upload/scripts/upload_file.py \
  --path product/project-sandbox.html \
  /Users/admin/Dev/code/other/workspace/ox-alpha/project-sandbox.html
```

覆盖更新 URL 不变。两个域名指向同一对象。

## 七、服务端版（server/，2026-08-28 完成联调）

在保持单文件前端可独立使用的前提下，增加可选 Go 服务端，实现**多沙盘 + 数据集中存储**。

### 使用方式

```bash
cd server
cp .env.example .env                 # 填入 SMTP 等真实凭证（必填，否则验证码发不出去）
go build -o sandbox-server .        # 已编译好二进制可直接用
./sandbox-server                    # 默认 :8787，db 存 ./sandbox.db
# 可选参数：-addr :8787 -db sandbox.db -html ../project-sandbox.html
```

配置优先级：**命令行参数 > 环境变量 > .env > config.json > 默认值**。
SMTP 凭证只能通过 `.env` 或 `PSB_SMTP_*` 环境变量提供，不进 config.json（避免密码入库）。

- 同源使用：浏览器直接开 `http://localhost:8787/`，前端自动检测 `/api/boards` 进入服务端模式
- 跨源使用：照常打开静态页（file:// 或 Cloudflare），加参数 `?api=http://host:8787`
- 连不上服务端自动回退 localStorage 本地模式，顶栏沙盘切换器随之隐藏

### 多用户与登录（2026-08-28 加入）

- 注册需**邮箱验证码**（SMTP 发信）。凭证配置在 `server/.env`（模板见 `.env.example`，已被 gitignore），
  也可用 `PSB_SMTP_*` 环境变量注入
- 新账号初始为**空白沙盘**（不带示例数据）；本地 file:// 模式保留示例数据便于首次上手
- 密码 PBKDF2-HMAC-SHA256（30 万轮 + 随机盐，Go 标准库 crypto/pbkdf2），会话为 HttpOnly Cookie（30 天）
- 每个用户只看到自己的沙盘：boards 归属 owner，所有接口按登录用户过滤；升级前遗留的无主沙盘在首个用户登录时自动归属
- 防护：验证码 15 分钟有效、60 秒发送间隔、错 5 次作废；已验证邮箱重复注册返回 409；未验证邮箱登录时前端自动切到注册 tab 引导补验证
- API 增量：`POST /api/auth/register`（发验证码）、`POST /api/auth/verify`（验证并登录）、`POST /api/auth/login`、`POST /api/auth/logout`、`GET /api/auth/me`
- 调试：`PSB_DEBUG=1` 时验证码打印到服务端日志（仅排查用，勿在生产开启）
- CORS 为 credentialed 模式（回显 Origin + Allow-Credentials），跨源 `?api=` 下登录态同样生效

环境变量一览（均可写入 `.env`）：

| 变量 | 说明 | 默认 |
|---|---|---|
| `PSB_SMTP_HOST` / `PSB_SMTP_PORT` | SMTP 服务器与端口（465 走隐式 TLS） | 端口 465 |
| `PSB_SMTP_USER` / `PSB_SMTP_PASS` | 发信账号与密码/授权码 | 无（必填） |
| `PSB_SMTP_FROM` | 发件人显示名 | 项目沙盘 |
| `PSB_ADDR` / `PSB_DB` / `PSB_HTML` | 监听地址 / 数据库 / 前端路径 | :8787 / sandbox.db / project-sandbox.html |
| `PSB_DEBUG` | 验证码打印到日志（=1 开启） | 关 |

### 架构

| 项 | 说明 |
|---|---|
| 服务端 | Go 标准库 + modernc.org/sqlite（纯 Go 驱动，无 CGO），单二进制约 14MB；代码分 `main.go` / `auth.go` / `mail.go` |
| 存储 | SQLite WAL，表 `boards`（含 owner 归属）+ `snapshots`（快照历史，每沙盘最多 20 条）+ `users` / `sessions` / `email_codes` |
| 写入串行化 | sync.Mutex 包住全部写事务（SQLite 单写者），SMTP 发信在锁外 |
| 快照语义 | 与前端原 pushHistory 一致：内容与最新快照相同则不留档；保存时自动留档 |
| CORS | 回显 Origin + Allow-Credentials，支持远程静态页带登录态指向自建服务端 |

### API

```
GET    /api/health                 健康检查（无需登录）
POST   /api/auth/register          发送注册验证码 {email, password}
POST   /api/auth/verify            验证邮箱并登录 {email, code}
POST   /api/auth/login             登录 {email, password}
POST   /api/auth/logout            登出
GET    /api/auth/me                当前用户
GET    /api/boards                 本用户的沙盘列表（需登录，下同）
POST   /api/boards                 新建沙盘 {name, state?}（带 state 时存首个快照）
PATCH  /api/boards/{id}            重命名 {name}
DELETE /api/boards/{id}            删除沙盘（连带快照）
GET    /api/boards/{id}/state      读状态 {state}
PUT    /api/boards/{id}/state      存状态 {state}（自动留档）
GET    /api/boards/{id}/history    快照列表 [{ts, data}]（时间升序）
```

### 前端集成要点

- `bootLoad()` 启动装配：`detectServer()` 探测 → 成功走 `loadFromServer()`，失败走 `loadLocal()`
- 服务端模式下：自动保存改为 PUT（防抖 500ms）；历史弹窗/回滚走服务端快照；折叠状态按沙盘隔离（`psb-collapsed:<boardId>`）；当前沙盘记 `psb-current-board`
- 顶栏新增沙盘切换器：下拉切换 + 新建/重命名/删除（删除需输入沙盘名确认）
- 顶栏沙盘切换器为自定义下拉组件（非原生 select）：当前沙盘名 + 层叠图标，菜单含更新时间、行内重命名/删除、底部新建；菜单项带 `role="menu"`/`menuitem` 语义；Esc/点击外部关闭
- 注意数据形状差异：本地历史 `data` 是 JSON **字符串**，服务端返回的是已解析**对象**，`fetchHistory()` 内已归一化

### 联调中发现并修复的 bug

1. `bootLoad()` 此前被调用但从未定义 → 页面启动即 ReferenceError 空板（改造中断点，已补上）
2. 历史快照数据形状不一致 → 服务端模式下历史弹窗全显示「无法解析的快照」、回滚失效（fetchHistory 归一化修复）
3. 「恢复此版」按钮用 `data-ts` 而事件委托读 `data-id` → 一键回滚静默失败（本地/服务端模式皆然）；同时确认弹窗 z-index 低于历史弹窗被盖住，已提高 `#confirm-modal` 层级
4. 输入弹窗「取消」按钮/遮罩用的 `data-input-cancel` 在事件委托里没有分支 → 点取消无反应（已补分支）；`inputDialog` 顺带加固：finish 幂等防重复 resolve、输入法组合期间不响应回车/Esc，单次 Esc 即可关闭
5. 项目描述（.pdesc）原本 -3px 负边距贴死在项目行下缘 → 改为 4px 上边距，有了呼吸感
6. 静态首页没有 Cache-Control → 前端更新后浏览器可能继续跑旧代码（表现为改完不生效），已加 `Cache-Control: no-cache` 强制每次校验

端到端已验证：加载、勾选自动保存落库、多沙盘新建/切换/删除、历史查看与一键回滚、本地回退；注册（含真实邮件验证码）/登录/登出/会话保持、多用户数据隔离与越权防护、未验证邮箱引导、跨源 `?api=` 登录态、PBKDF2 与 Python 参考实现比对一致。

## 八、建议的使用节奏（每周）

1. 打开沙盘 → 点「事项动态」回顾各事项上周推进
2. 在对应事项上补写本周进展（气泡按钮）
3. 勾掉完成的事项，添加新事项
4. 确认自动备份绿点亮着

## 九、可能的后续方向（未排期）

- 事项动态导出为周报文本
- 多看板（不同工作域分开）
- 浅色/深色主题切换
- 目标级进展汇总视图
