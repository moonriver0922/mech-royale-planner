# 公开参赛策略与本地实验

2026 年 11 月 21 日香港时间 10:55，AT 以 5.48 分排名第 1，BobBuildIt 以 5.25 分排名第 2，Wolverines 以 5.19 分排名第 3，r1ver 以 5.08 分排名第 4。r1ver 的版本和回执见[官方试跑记录](official-runs.md)。[官方练习榜](https://www.joinbuilders.org/buildathon/tracks/builders)

| 队伍 | 来源和版本 | 可借鉴之处 |
| --- | --- | --- |
| Wolverines | 同届另一赛道的[回执](https://github.com/varchaskukreja/TTUN/blob/b4f1a62b33a8b246377ad416a36e87ebcc211eb2/rwx-blast-radius/submissions/BB-C2E99545.json)确认了作者与队名。作者在[版本说明](https://github.com/varchaskukreja/TTUN/commit/f2cf1829c0114aa4cfe2d10ed8423c91f8ad4099)中明确以 5.19 分经济版作为 v10 的基础。源码差异与说明吻合，对应的 [v8-econ](https://github.com/varchaskukreja/TTUN/tree/b7e9a6db75154271185035ef856859be37dff60e) 提交早于该榜单记录 10 秒。另一队曾将该分数归到 v7，本记录据作者资料修正。官方榜单没有公布 Wolverines 提交的 Git 哈希。 | 比较同一局里的方块、装备和补给点。v8-econ 是后续资源与导航候选共同的对照版本。 |
| BackwardsDeployeds | 作者的[官方试跑记录](https://github.com/brycehcmcgrath/mechfight-bot/blob/18ae7c471959166ca4eea7b2a0f2581dbcbcd407/docs/RESULTS.md)列出 Lance 开启和关闭 `sneak` 时分别取得 4.54 与 4.75。 | r1ver 也单独提交了正常移速的候选，在自己的版本上得到 4.67。 |

目前尚未核实前两名 AT 和 BobBuildIt 的公开仓库。下列两项候选是按 TTUN 思路对本项目做的独立实现。近点候选在通往圈心的直线受阻时寻找能直接抵达的内圈落脚点。屋顶候选只在机甲比物资低超过 4 米、且物资比当地地面高超过 3 米时放弃追逐。

## 配对结果

所有对照使用首个官方版本 `97e0e58`。练习模式中，两个版本各自经历同一组初始场景。混战中，两个版本和 14 个固定机器人同场，两局交换出生位。积分为 16 减存活名次。下面的分数均来自本地模型。

| 候选或参考对象 | 模式与种子 | 对照均分 | 候选均分 | 观察 |
| --- | --- | ---: | ---: | --- |
| 内圈路点候选 | 练习，1201–1232 | 5.594 | 5.219 | 1 局提高、28 局持平、3 局下降。保留为独立实验补丁。 |
| 屋顶过滤候选 | 屋顶物资练习，1401–1432 | 3.500 | 3.531 | 2 局提高、26 局持平、4 局下降。对照实际捡到 4 件屋顶装备。保留为独立实验补丁。 |
| 完成空投占领 | 混战，5601–5608，各交换一次出生位 | 11.313 | 11.313 | 在 4 场触发 70 次决策。新增行为处理一个已复现的边界情况，目前没有测得均分收益。 |
| TTUN v7 原版 | 练习，4101–4116 | 4.563 | 1.688 | 这组旧实验比较的是 v7，两队最新参赛版本另见上表。 |
| TTUN v7 原版 | 混战，4501–4504，各交换一次出生位 | 12.125 | 12.125 | 两个版本的平均分相同。 |

开发版中，机甲独占已落地的空投、距离占领完成最多剩 1 秒，且周围平静、无需进圈时，会朝空投中心移动。边缘场景的回归检查可通过 `python3 tools/check_capture.py --kit ../mechfight-kit` 运行。做策略取舍时应同时看实际触发次数、配对分差及官方成绩；练习模拟器省略了声音和空投等机制，混战模拟器也使用简化对手。[逐局积分](results.csv)

## 复现已保存的候选

从仓库根目录执行以下命令，使用同一份官方启动包。生成的两个临时工作树各自包含完整的 v1 实现。

```sh
git worktree add --detach ../mech-inner 97e0e58
git -C ../mech-inner apply - < experiments/inner-circle.patch
python3 -B tools/simulate.py --kit ../mechfight-kit --bot ../mech-inner/bot.py --baseline git:97e0e58 --seed 1201 --matches 32

git worktree add --detach ../mech-roof 97e0e58
git -C ../mech-roof apply - < experiments/roof-loot.patch
python3 -B tools/simulate.py --kit ../mechfight-kit --roof-loot --bot ../mech-roof/bot.py --baseline git:97e0e58 --seed 1401 --matches 32
```

第三方对照使用上述 MIT 版本的 `mech-royale/bot/bot.ts`，可作为 `--bot` 传给两种模拟器。TypeScript 对照使用 Node 24 运行，原参赛镜像使用 Bun。机器人自身的时间预算、机器负载和运行时都可能影响复跑结果。
