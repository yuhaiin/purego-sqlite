# 与 mattn/go-sqlite3 的功能差距调研

调研日期：2026-11-05。本文是接口与迁移语义的调研，不表示已经决定实现全部 API。

本文记录实现前的固定 main 快照。随后完成的高优先级功能、当前 API 与
验证方法见 [兼容性指南](mattn-compatibility.md)；中优先级内容仍是后续计划。

## 对比范围

- mattn/go-sqlite3：固定源码 [45f73f261e58ede2a09b91a392ef13a5ec0bb0fa][mattn-tree]，不是只参考 README 或旧版印象。
- 最新发布版为 [v1.14.52][mattn-release]（2026-09-05，源码 `b0be46fa28d17ee0b65c79774ac0dad84b6db068`），也是本库 benchmarks/go.mod 使用的版本。另行核对了该版的语句缓存实现，下面的缓存建议并非只针对未发布 master。
- purego-sqlite：远程 main [550d58b7e757e5dbc83fe128fe27a7cdc1421948][ours-tree]。
- 调研工作区 HEAD 为 `eedc669e5bf73d34b5ba1ca0d37d112dea0e2760`，分支 `codex/connection-limits`。它与上述 main 的差异集中在 Go codegen、自动更新和相应 CI/文档；本文核对的连接、绑定、rows、backup、limits 核心文件一致。

结论：优先补齐迁移时会改变行为的 DSN 和类型语义，其次是 Go 标量函数/排序规则、内存快照和备份恢复。SQL 引擎已经提供的 FTS、JSON、WAL、RETURNING 等不需要再包装成“新增功能”。回调可通过公开 SQLite ABI 实现，并不需要修改系统 libsqlite3；主要成本是各后端 ABI、连接锁和生命周期的完整验证。

## 已经具备，不应重复列为缺失

| 能力 | 当前状态 | 真正剩余差距 |
|---|---|---|
| 每条物理连接初始化 | `WithInit` 对应 mattn 的 `SQLiteDriver.ConnectHook` 使用场景 | 名称不同，不等于缺功能；需要明确新连接都会执行 |
| 运行时资源上限 | `Conn.SetLimit(category, value)`，负数查询当前值 | mattn 分开暴露 `GetLimit`/`SetLimit`；当前能力已经覆盖 |
| 在线备份 | `BackupTo(ctx, filename)` / `Backup(ctx, *sql.Conn, filename)` | 仅 main 到按 filename 另开连接的文件，已存在的内容会被替换；缺少任意连接/附加 schema、恢复及进度控制 |
| 原生扩展加载 | `WithExtensions` + `LoadExtension`，绝对路径、显式授权 | 系统 so/平台/静态 musl 的限制应保留，不能为 API 同名而放宽 |
| 无参数 SQL 脚本 | `Exec` 接受 `;` 分隔，SQLite 自己解析边界 | 参数化脚本和多语句 Query 尚不支持 |
| 元数据 | 声明类型名称、未知 nullability | 缺 `ColumnTypeScanType`；当前类型名称还会大写并去掉括号参数 |
| 原生 SQL | savepoint、WAL、JSON、FTS、RTree、窗口 SQL 等按所选库能力执行 | 模块是否编译进库与 Go 接口是否存在应分开判断 |

依据：[本库初始化流程][ours-driver]、[资源上限][ours-limits]、[备份][ours-backup]、[扩展][ours-extensions]、[脚本执行][ours-conn]、[rows 元数据][ours-rows]、[能力探测][ours-capabilities]；mattn 的 [ConnectHook][mattn-driver]、[limits][mattn-limits] 和 [backup][mattn-backup]。

## 迁移价值最高的兼容差距

### 1. DSN 配置不能静默失效

mattn 除 SQLite 原生 URI 参数外，还解析驱动专用参数及别名：

| 类别 | mattn 参数 |
|---|---|
| 等待、事务、时区、连接 | `_busy_timeout` / `_timeout`、`_txlock`、`_loc`、`_mutex` |
| 常用 PRAGMA | `_foreign_keys` / `_fk`、`_journal_mode` / `_journal`、`_synchronous` / `_sync`、`_cache_size` |
| 其他 PRAGMA | `_auto_vacuum` / `_vacuum`、`_defer_foreign_keys` / `_defer_fk`、`_ignore_check_constraints`、`_locking_mode` / `_locking`、`_query_only`、`_recursive_triggers` / `_rt`、`_secure_delete` |
| 历史或需单独评估 | `_case_sensitive_like` / `_cslike`、`_writable_schema`、`_auth*` |
| 语句复用 | `_stmt_cache_size`，默认 0；已确认 v1.14.52 和上述 master 都支持，不推断更早版本 |

以上来自 [mattn 实际参数解析][mattn-dsn]。`mode`、`cache`、`immutable`、`vfs` 则属于 [SQLite 原生 URI][sqlite-uri]，本库已经传入 `SQLITE_OPEN_URI`，不是缺少同名 Go option 就不可用。

本库目前不解析上述 mattn 专用参数，而是原样把 DSN 交给 SQLite。对于 `file:app.db?_foreign_keys=on&_busy_timeout=100`，标准 SQLite/VFS 不会因为这两个名字而执行相应 PRAGMA 或设置 timeout；本库自己的默认 timeout 仍为 5000ms。普通 `app.db?_foreign_keys=on` 又与 URI 不同：SQLite 只把 `file:` 开头视为 URI，本库没有 mattn 那样的 DSN 拆解，存在把参数当作文件名的一部分的迁移风险。[本库打开连接][ours-driver]、[SQLite URI 识别规则][sqlite-uri]

建议先提供明确的兼容策略：支持最常用且能可靠验证的参数，或对已知但不支持的 mattn 参数报错，并给出 typed option / `WithInit` 替代方式。不要简单拒绝所有未知 URI 参数，因为自定义 VFS 也可能需要参数。实现参数支持时，定义与 typed options 的优先级、别名冲突、非法值，以及对每条物理连接的应用顺序。`_defer_foreign_keys` 的事务重置语义也不能靠“建连接时设置过一次”推断永久有效。这里是行为兼容问题，不是 SQLite 缺少外键或 WAL。

不建议无条件照搬历史选项：mattn 的 `sqlite_userauth` 已明确返回不再支持的错误；SQLite 也标记 [`case_sensitive_like` 为弃用][sqlite-pragma]。本库继续固定 FULLMUTEX，并将 `_mutex=no` 列为显式不支持，比无声接受更清楚。[mattn userauth][mattn-auth]

### 2. time.Time、BOOLEAN 与列元数据

| 场景 | mattn 固定版本 | 本库固定 main |
|---|---|---|
| 绑定 `time.Time` | 按 `SQLiteTimestampFormats[0]` 编码为 TEXT，格式数组公开可变 | 直接报不支持，要求调用者编码 |
| 读取 DATE / DATETIME / TIMESTAMP 的 TEXT | 尝试公开格式列表，失败返回零值 `time.Time`，支持 `_loc` | 返回原始 string |
| 读取这些声明列的 INTEGER | 大于 `1e12` 或小于 `-1e12` 按毫秒，其余按秒；转 `time.Time` | 返回 int64 |
| 读取 BOOLEAN 的 INTEGER | `val > 0` 转 bool，负数得到 false | 返回 int64；绑定 bool 已支持 |
| `Rows.ColumnTypes().ScanType()` | 提供声明类型映射，如 sql.NullTime、sql.NullBool、sql.NullInt64 | 无该可选接口，database/sql 只能用默认类型 |
| 声明类型名称 | 原始 `sqlite3_column_decltype` 字符串 | 大写、去掉括号参数，例如 VARCHAR(50) 变 VARCHAR |
| nullable | 硬编码 `(true, true)` | `(true, false)`，明确未知 |

依据：[mattn 时间格式][mattn-time-formats]、[绑定][mattn-time-bind]、[读取转换][mattn-time-read]、[元数据][mattn-types]；[本库绑定][ours-stmt] 和 [读取][ours-rows]。SQLite 自身只有存储类和类型亲和性，日期/布尔自动转换属于驱动策略，不是原生引擎功能缺失。[SQLite 类型系统][sqlite-types]

建议把 `time.Time` 绑定、日期读取与 bool 读取分开设计。可用显式 mattn 兼容模式保留既有用户的原始类型行为；不要全局改变已有结果值。`ScanType` 成本较小，但必须与实际返回值匹配。不要为“对齐”把未知 nullability 伪装成已知，也不必把全局可变格式表照抄到并发连接实现里。负数 boolean、非法日期、整数秒/毫秒边界、NULL、表达式列、时区/DST、schema 重新 prepare 和可选 row bridge 都需要验证。

### 临时对照探针

在 Linux 本机创建独立临时 Go 模块，通过本地 replace 分别指向上述两份固定源码；mattn 使用 CGO，purego-sqlite 分别跑 `CGO_ENABLED=1 go run -tags=sqlite_purego .` 和 `CGO_ENABLED=1 go run -tags=sqlite_dynamic .`。两种本库 backend 的结果一致。探针不属于仓库回归测试，也不能证明其他平台的行为。

| 探针 | purego-sqlite | mattn |
|---|---|---|
| `file:...?_foreign_keys=on&_busy_timeout=17` 后读取 PRAGMA | foreign_keys=0，busy_timeout=5000 | foreign_keys=1，busy_timeout=17 |
| 绑定 `time.Time` | 明确拒绝 | 成功写入 |
| BOOLEAN / TIMESTAMP 列扫描到 any | int64 / string | bool / time.Time |
| 同列 `ScanType()` | interface{} / interface{} | sql.NullBool / sql.NullTime |
| 普通绝对路径 `/tmp/.../name.db?_foreign_keys=on` | 文件名包含 `?_foreign_keys=on` | 剥离参数后创建 name.db |

最后一项符合本库现有“SQLite 原生 URI”契约，属于迁移陷阱，不能直接据此称驱动实现错误。加 `file:` 仅解决文件名识别，不会让 mattn 专用参数自动生效；这些参数仍须转换为本库 typed options / `WithInit`，或由新增的兼容层解析。

## mattn 已公开而本库缺少的扩展 API

| 建议优先级 | 功能与 mattn 公开接口 | 用户价值、实现成本及系统库条件 |
|---|---|---|
| 高 | `RegisterFunc(name, impl, pure)` | 迁移 Go 自定义 SQL 标量函数、正则、业务计算。mattn 支持反射函数、变参、返回 `(value, error)`；`pure` 对应 DETERMINISTIC。本库需先建立通用 callback/value/result 桥，无需自定义 so。 |
| 高 | `RegisterCollation(name, cmp)` | 自定义排序规则和字符串索引；同一套 callback 所有权可复用。比较函数需满足稳定全序关系。 |
| 中高 | `Serialize(schema)` / `Deserialize(bytes, schema)` | 内存库加载、测试夹具、快照导出；无 Go 回调，适合作为独立增量。系统库可能未导出符号，须可选绑定和 ErrUnsupported；不能提高所有系统库最低要求。mattn 默认 amalgamation 有实现，`libsqlite3` 模式另需 `sqlite_serialize` 标签。 |
| 中高 | `Backup(destSchema, srcConn, srcSchema)` + `Step/Remaining/PageCount/Finish/Close` | 比现有文件备份多出恢复、内存到内存、ATTACH schema 和进度。可优先提供 context 管理的一次性连接到连接复制与进度配置，未必必须照搬手动 native handle。双方必须属于同一个 SQLite 库实例。 |
| 中 | `RegisterAggregator(name, ctor, pure)` | Go 聚合器的 Step/Done；每组状态要独立清理，覆盖空输入、Step 报错、取消、最终回收。回调基础设施稳定后再加。 |
| 中 | `RegisterUpdateHook` / `RegisterCommitHook` / `RegisterRollbackHook` | 本连接写入通知、提交否决、事务事件。回调期间不能修改触发它的连接；通知不等于完整 CDC，update hook 不覆盖 WITHOUT ROWID，并有 REPLACE/截断删除例外。 |
| 中 | `RegisterAuthorizer` | 控制不可信 SQL 的表/列/操作访问；发生于 prepare 或自动 reprepare，不能只套在 Exec 外层。mattn 的签名只传 action 加三个字符串；新设计可保留 SQLite 完整元数据。 |
| 中 | `SetTrace(*TraceConfig)`、`TraceInfo` | SQL 和性能观测；mattn 需 `sqlite_trace` 或 `trace` 标签。不要默认展开 SQL 参数，避免普通日志无意记录用户数据；回调负载、生命周期与其他 hooks 共用基础设施。 |
| 中低 | `GetFilename`、`AutoCommit`、`SQLiteStmt.Readonly` | 轻量状态/诊断访问。本库内部已经绑定相应原生 API，只是未公开；可按真实需求提供受锁保护的方法，不暴露裸句柄。 |
| 中低 | `SetFileControlInt` / `SetFileControlInt64` | 特定 VFS 调参、持久 WAL 等。`void*` 的真实类型由 opcode 决定，不能把任意 opcode 都当 int 指针；建议白名单 typed 操作，正确处理 NOTFOUND 和直接返回码。 |
| 中低 | `SQLiteStmt.ColumnTableName` | `sqlite_column_metadata` 标签提供结果列来源表；依赖库编译 COLUMN_METADATA，与已有声明类型名称不是同一功能。 |
| 中、成本较低 | `Error.SystemErrno`、具名 extended error 常量 | mattn 在部分 CANTOPEN/IOERR 错误里补充系统 errno，并提供 ErrConstraintUnique/ErrConstraintForeignKey/ErrBusySnapshot 等常量。本库已有 Code/ExtendedCode/Message 和 Busy/Locked/Constraint/ReadOnly 四类 errors.Is，但缺同等诊断便捷性；应保留原始 native code，不把未知 errno 误报为确定的系统原因。 |
| 低、需求驱动 | `CreateModule` / `DeclareVTab`、Module/VTab/VTabCursor 等 | mattn 的 `sqlite_vtable` / `vtable` 标签开放 Go 虚拟表模块；需要大量函数指针、结构体 ABI、游标/错误内存和生命周期管理。比“执行原生 FTS5 SQL”复杂很多。 |
| 低、需求驱动 | `RegisterPreUpdateHook`、`SQLitePreUpdateData.Old/New/Depth/Count` | mattn 的 `sqlite_preupdate_hook` 标签才有真正实现，默认方法是 no-op；依赖 SQLite 编译启用 PREUPDATE_HOOK，不应承诺任何系统库都可用。 |

API 证据：[mattn 函数/聚合/排序规则/hooks][mattn-callbacks]、[序列化实现][mattn-serialize]、[系统库序列化 stub][mattn-serialize-omit]、[backup][mattn-backup]、[trace][mattn-trace]、[连接状态/file control][mattn-limits]、[stmt Readonly][mattn-readonly]、[column metadata][mattn-column-metadata]、[system errno][mattn-errno]、[具名错误码][mattn-errors]、[vtable][mattn-vtable]、[preupdate][mattn-preupdate]、[preupdate 默认 stub][mattn-preupdate-omit]。

原生约束：[函数注册和销毁][sqlite-functions]、[在线备份][sqlite-backup]、[serialize][sqlite-serialize]、[deserialize][sqlite-deserialize]、[update hook][sqlite-update]、[commit/rollback hook][sqlite-commit]、[authorizer][sqlite-authorizer]、[trace_v2][sqlite-trace]、[file_control][sqlite-filecontrol]。这些是公开 ABI；通常不必重新编译 so。只有所选库省略相关 API 或模块时，才需要换库/改构建。

### 多语句与 stmt cache 需要单独判断

mattn 的 Exec 循环解析 SQL tail，并按每条语句消耗参数；Query 也循环 tail，但只把最后的 rows 返回给调用者。它没有实现 `RowsNextResultSet`，不能说“mattn 支持通过 NextResultSet 取得每个结果集”。本库当前只允许无参数 Exec 脚本，Prepare、Query 与有参数 Exec 都要求单条语句。[mattn 执行逻辑][mattn-scripts]、[本库执行逻辑][ours-conn]、[本库单语句检查][ours-stmt]

参数化脚本对迁移有价值，但要明确参数跨语句分配、命名参数复用、错误后已执行部分的语义和最后结果。多个可遍历结果集是另一项功能，不能以 mattn parity 为由混入。固定版本 mattn 的 `_stmt_cache_size` 说明其提供可选连接缓存；本库没有透明缓存，但调用者已经能复用 `sql.Stmt`。缓存应在真实 workload 证明收益后再考虑，必须包括 schema 变化、驱动状态、连接关闭和失败 eviction 的验证。[mattn 缓存配置][mattn-dsn]

透明语句缓存有明确的通用适用场景：调用者反复通过 `db.Exec` / `db.Query` 执行相同 SQL，而没有显式复用 `sql.Stmt`。它可以减少反复 prepare/finalize；对已经复用 statement 的循环，以及 FullScan 中逐行取列的开销，不能据此承诺改善。建议做每物理连接有界、默认关闭的候选方案，测重复 SQL、不同 SQL、schema 变更和大参数释放后的收益与资源占用，再决定是否启用。mattn v1.14.52 的 [配置][mattn-cache-stable]和 [LRU/清理实现][mattn-cache-impl]可作为参照，不需要修改官方 so。

## SQLite 有原生 API，但不属于本次已确认的 mattn 缺口

检查上述固定源码的公开 Go 方法及可选实现后，以下不应标记成“mattn 已有，本库必须追平”：增量 BLOB 句柄 I/O、session/changeset、Go 自定义窗口聚合 xInverse/xValue、独立 Go FTS5 tokenizer 注册、通用 db_status/stmt_status 等完整统计 API、Go 自定义 VFS。SQLite 有相应底层能力，并不意味着 mattn 已提供同样的公开 Go API；通过 C 扩展使用它们也不同于驱动内建封装。[mattn 固定源码清单][mattn-tree]、[SQLite BLOB API][sqlite-blob]、[Session 扩展][sqlite-session]、[窗口函数注册][sqlite-functions]、[FTS5 tokenizer][sqlite-fts5]

`sqlite_userauth` 的历史类型/API、mattn 多个 SQLite 编译开关也不应自动成为当前路线图；后者先通过 Connector 的所选库 CompileOptions/HasFeature 诊断即可。本库应继续允许使用系统官方 so。[本库能力探测][ours-capabilities]

## 回调可以实现，但应先验证共用基础设施

PureGo 0.11.3 已有 `NewCallback`；Unix 和 Windows 的调用约定实现不同，callback 槽位为进程级有限资源且不回收。新连接或每次注册都生成新的闭包 trampoline，会在正常连接 churn 中耗尽。建议每类签名只创建少数进程级 trampoline，再以整数 token 查找连接拥有的 Go 回调；连接关闭、替换注册和失败路径释放 token/Go 引用。[PureGo Unix callbacks][purego-unix]、[Windows callbacks][purego-windows]

需验证的约束：

1. PureGo amd64/arm64 的 Linux、macOS、Windows，以及已有 BSD 支持范围；Windows cdecl/整数宽度/void 返回、Mac arm64 原生回调和 race/cgocheck 的真实运行，而非只交叉编译。
2. MIPS 等 CGO 后端使用编译器确定类型的 C trampoline 和安全 token 注册，不依赖 PureGo 对不支持架构的 callback；支持 CGO 不意味着自动获得 PureGo callback。
3. native 仅存 token，不能保留 Go 指针；SQLite value/context/text/blob 指针通常只在回调期间有效，返回 TEXT/BLOB 明确复制或使用 native 所有权。
4. `Conn.mu` 当前覆盖执行；回调通常由已持锁的 `sqlite3_step/prepare` 同步触发。用户回调如果重入同一连接会死锁或违反 SQLite 限制，需定义并验证错误/禁止重入契约；不能粗暴在 native 调用时解锁。
5. 回调 panic 不得越过 FFI 边界；标量/聚合尽可能转 SQLite error，authorizer/commit 则使用保守拒绝值；hook/trace 的错误如何报告需要 API 设计。
6. 注册替换、失败、关闭、执行取消、聚合析构、GC 压力与一万次连接 churn 都必须有验证；可用 `create_function_v2` destructor 时遵守 native 销毁顺序，回调执行中不能提前释放 token。
7. 自定义 Go 函数仍可处理 float64：SQLite xFunc 本身收 context/argc/argv 指针，数值经 sqlite3_value_double 与 sqlite3_result_double 传递；无需把任意 Go 函数签名直接当 native callback，也不应因 float 返回 ABI 就宣布无法支持。[SQLite 函数 ABI][sqlite-functions]

这些是设计和验证要求，尚未在本次调研中证明整套跨平台 callback 已能安全运行。callback 的每行 native→Go 成本也需要单独 benchmark，避免把功能兼容误写成速度优势。

## 建议实施顺序与验收

1. **先解决迁移行为**：常用 DSN 支持或明确报错、time.Time 输入与可选日期/布尔兼容、ScanType/声明类型契约。用同数据/同 SQL 的 mattn 对照测试；原始模式行为保留。
2. **独立、无需回调的增量**：具名错误码/SystemErrno、可选 serialize/deserialize、连接到连接的备份/恢复；可按需求先加 GetFilename/Autocommit。旧系统 so 缺符号应仍能正常使用其余驱动功能。透明语句缓存可另作性能实验，收益通过普通重复 SQL 工作负载验证。
3. **一次搭好回调所有权**：先标量函数和排序规则，随后聚合、authorizer、事务 hooks、trace。提供 `WithInit` 注册范例与连接池正确用法；至少系统 so 和 embed 都跑，不要求专用辅助函数。
4. **高级特性按调用方需求追加**：vtable、preupdate、file control、参数化脚本。不要为了列表齐全把所有历史开关和裸 C 指针开放给用户。

## 本次完成的验证

- 核对工作区与 main 的核心实现一致，纯 Go backend 的 `CGO_ENABLED=0 go test -count=1 ./...` 通过。
- 在独立临时模块执行上述 DSN、time.Time、BOOLEAN/TIMESTAMP、ScanType 和普通文件名探针；分别验证 PureGo 和动态 CGO backend，与固定 mattn 源码对照。
- 对照 mattn 公开 API、可选 build tags 和 SQLite 官方 ABI 文档，列出已有能力、真实缺口、实现约束与后续验收要求。
- 以上为实施前的调研验证；后续实现和实际测试范围以兼容性指南及 PR 检查为准。

[mattn-tree]: https://github.com/mattn/go-sqlite3/tree/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa
[mattn-release]: https://github.com/mattn/go-sqlite3/releases/tag/v1.14.52
[mattn-cache-stable]: https://github.com/mattn/go-sqlite3/blob/b0be46fa28d17ee0b65c79774ac0dad84b6db068/sqlite3.go#L1581-L1594
[mattn-cache-impl]: https://github.com/mattn/go-sqlite3/blob/b0be46fa28d17ee0b65c79774ac0dad84b6db068/sqlite3.go#L1967-L2049
[ours-tree]: https://github.com/yuhaiin/purego-sqlite/tree/550d58b7e757e5dbc83fe128fe27a7cdc1421948
[mattn-driver]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3.go#L542-L547
[mattn-callbacks]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3.go#L770-L1096
[mattn-dsn]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3.go#L1375-L1770
[mattn-time-formats]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3.go#L409-L425
[mattn-time-bind]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3.go#L2450-L2478
[mattn-time-read]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3.go#L3108-L3179
[mattn-types]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3_type.go
[mattn-limits]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3.go#L2280-L2379
[mattn-readonly]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3.go#L2795-L2800
[mattn-column-metadata]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3_opt_column_metadata.go
[mattn-errno]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3.go#L1112-L1135
[mattn-errors]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/error.go
[mattn-scripts]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3.go#L1142-L1257
[mattn-backup]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/backup.go
[mattn-serialize]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3_opt_serialize.go
[mattn-serialize-omit]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3_opt_serialize_omit.go
[mattn-trace]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3_trace.go
[mattn-vtable]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3_opt_vtable.go
[mattn-preupdate]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3_opt_preupdate_hook.go
[mattn-preupdate-omit]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3_opt_preupdate_omit.go
[mattn-auth]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3_opt_userauth.go#L30-L49
[ours-driver]: https://github.com/yuhaiin/purego-sqlite/blob/550d58b7e757e5dbc83fe128fe27a7cdc1421948/driver.go
[ours-conn]: https://github.com/yuhaiin/purego-sqlite/blob/550d58b7e757e5dbc83fe128fe27a7cdc1421948/conn.go
[ours-stmt]: https://github.com/yuhaiin/purego-sqlite/blob/550d58b7e757e5dbc83fe128fe27a7cdc1421948/stmt.go
[ours-rows]: https://github.com/yuhaiin/purego-sqlite/blob/550d58b7e757e5dbc83fe128fe27a7cdc1421948/rows.go
[ours-backup]: https://github.com/yuhaiin/purego-sqlite/blob/550d58b7e757e5dbc83fe128fe27a7cdc1421948/backup.go
[ours-limits]: https://github.com/yuhaiin/purego-sqlite/blob/550d58b7e757e5dbc83fe128fe27a7cdc1421948/limits.go
[ours-extensions]: https://github.com/yuhaiin/purego-sqlite/blob/550d58b7e757e5dbc83fe128fe27a7cdc1421948/extensions.go
[ours-capabilities]: https://github.com/yuhaiin/purego-sqlite/blob/550d58b7e757e5dbc83fe128fe27a7cdc1421948/capabilities.go
[sqlite-uri]: https://www.sqlite.org/uri.html
[sqlite-types]: https://www.sqlite.org/datatype3.html
[sqlite-pragma]: https://www.sqlite.org/pragma.html#pragma_case_sensitive_like
[sqlite-functions]: https://www.sqlite.org/c3ref/create_function.html
[sqlite-backup]: https://www.sqlite.org/c3ref/backup_finish.html
[sqlite-serialize]: https://www.sqlite.org/c3ref/serialize.html
[sqlite-deserialize]: https://www.sqlite.org/c3ref/deserialize.html
[sqlite-update]: https://www.sqlite.org/c3ref/update_hook.html
[sqlite-commit]: https://www.sqlite.org/c3ref/commit_hook.html
[sqlite-authorizer]: https://www.sqlite.org/c3ref/set_authorizer.html
[sqlite-trace]: https://www.sqlite.org/c3ref/trace_v2.html
[sqlite-filecontrol]: https://www.sqlite.org/c3ref/file_control.html
[sqlite-blob]: https://www.sqlite.org/c3ref/blob_open.html
[sqlite-session]: https://www.sqlite.org/sessionintro.html
[sqlite-fts5]: https://www.sqlite.org/fts5.html#custom_tokenizers
[purego-unix]: https://github.com/ebitengine/purego/blob/3fc0c5fee75d22b9514d778832e130f2697698a8/syscall_unix.go#L22-L104
[purego-windows]: https://github.com/ebitengine/purego/blob/3fc0c5fee75d22b9514d778832e130f2697698a8/syscall_windows.go#L19-L43
