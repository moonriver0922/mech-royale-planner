# 来源和实现边界

允许的输入为[官方比赛规则](https://www.joinbuilders.org/buildathon/tracks/builders)、[官方协议与测试包](https://www.joinbuilders.org/buildathon/tracks/builders/files/mechfight-kit.tar.gz)和 Go 标准库。工具包 SHA-256 为 `1758fe9125e193b268c6c1cffbf2c0ae96377214ab06d117412571c1523065ba`。地图是工具包内的 `arena/v2/world.json` 和 `terrain.i16`。进程在 hello 时读比赛挂载的地图和配置。

项目早期接触过旧机器人源码。这里的实现从空目录开始，按本次设计、官方字段、通用几何与算法自行编写。旧代码及二进制没有进入源码、默认入口或运行依赖。

| 文件 | 实现依据与职责 | 编写和复查 |
| --- | --- | --- |
| `go.mod` | Go 标准库，无外部模块 | 集成 |
| `cmd/ownbot/main.go` | 按 hello 加载地图，用当前控制器处理比赛 | 协议实现，集成复查 |
| `internal/wire/types.go` | 协议 §5–6 和正式 Schema，含数字型升级配置 | 协议实现，集成补充升级字段 |
| `internal/wire/stream.go` | 协议 §2–3、§6 的 JSON Lines、握手与 tick 对应 | 协议实现，流式和官方 checker |
| `internal/wire/stream_test.go` | 流式消息、错误字段、输入边界 | 协议实现 |
| `internal/arena/map.go` | 协议 §7、§11，官方地图，双线性采样、线段判交和圆柱净空 | 几何实现，实图与几何测试 |
| `internal/arena/route.go` | 八邻接有界地面图搜索，起点与终点按真实地面连接 | 几何实现，绕障测试 |
| `internal/arena/map_test.go` | 几何、地形、尺寸和路径夹具 | 几何实现 |
| `internal/decision/controller.go` | 单一动作组装、观察事件、武器和升级状态，协议 §6、§10 | 集成，决策测试 |
| `internal/decision/navigation.go` | 路线跟随、实际几何校验、圈插值与恢复，协议 §7、§9 | 集成，圈与恢复测试 |
| `internal/decision/combat.go` | 匀速截击二次方程，实际炮塔射线、电弧锥角，协议 §5、§7 | 集成，转向、装填和电弧测试 |
| `internal/decision/resources.go` | 拾取距离、空槽、成长上限和装备时间，协议 §7、§10 | 集成，拾取和优先级测试 |
| `internal/decision/controller_test.go` | 小型地面比赛夹具，测试最终动作 | 集成 |
| `Dockerfile`、`.dockerignore` | 按协议 §1 的最小入口定义，启动同一 Go 命令 | 协议实现，集成复查 |
| `README.md`、`docs/*` | 构建复现、设计和来源 | 集成 |

### S02

S02 branches directly from our own S00 root at `19126d6516826c1de4e0e54c107c7bc4d6a23d81`. The laser work used that tree, our design memo and the official kit. Protocol §3 supplies coasting and missed-trigger semantics. Sections 5 and 6 define observed weapon stats, actual aim, events and action duration. Section 7 supplies cylinder geometry, forward muzzle, charge/release, forced discharge, lightning short and pickup rules. Section 10 supplies size scaling. Fallback rates, timeouts and range come from the match hello. No opponent implementation or simulator source was used to write S02.

The original S02 work is in `internal/decision/laser.go` and its integration in the existing combat, navigation, controller and resource files. `laser_test.go` exercises actions through `Decide` on synthetic map fixtures. Linear prediction, the finite cylinder intersection, shared-turret scheduling, the short post-charge alignment window and zone ETA are our implementation choices. The controller acts on current observations, with bounded static map segment queries. No training data or extra dependencies are required.

### S04A

The baseline is our published S02 commit `0e6fe8893373923952fa67fd5c0a450246ac10f2`. Our S02 review and the original shrinking-zone fixture motivated a shared eligibility check in the controller, combat and laser files. It uses the existing S02 charge duration and zone ETA. The inward-cube regression exercises the final controller action.
