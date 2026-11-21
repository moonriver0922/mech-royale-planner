# 公开参赛策略与本地实验

2026 年 11 月 21 日香港时间约 10:35 核对时，Wolverines 以 5.19 分排名第 3，BackwardsDeployeds 和 r1ver 同为 4.54 分，分别排名第 10 和第 11。[官方练习榜](https://www.joinbuilders.org/buildathon/tracks/builders)

| 队伍 | 来源和版本 | 可借鉴之处 |
| --- | --- | --- |
| Wolverines | 作者在同届另一赛道的[回执](https://github.com/varchaskukreja/TTUN/blob/b4f1a62b33a8b246377ad416a36e87ebcc211eb2/rwx-blast-radius/submissions/BB-C2E99545.json)确认队名及仓库。另一支参赛队的[提交记录](https://github.com/micah-andelman/mech-royale/commit/e189b9730af98b19e0ffdd3bc9f3f155cca22586)将 5.19 分对应到 [TTUN v7](https://github.com/varchaskukreja/TTUN/tree/e8cfba4b4152709eedc1d17cb24daa1108449c99)。官网公开队伍成绩，具体哈希由该参赛队指认。 | v7 选择自身方位上下一圈半径 45% 处的路点，过滤比当地地面高 2 米以上的物资，并结合空投到达时间和占领状态决定是否前往。 |
| BackwardsDeployeds | [实验记录](https://github.com/brycehcmcgrath/mechfight-bot/blob/a2f8df71f0762f50232c99d1cb110a7553e5c5de/docs/RESULTS.md)逐项给出提交、官方回执和分数。 | 使用有 MIT 许可的 Lance 版本及 `sneak` 配置得到 4.54。r1ver 的首次官方提交也得了 4.54。 |

本轮未核实第 1 名 AT、第 2 名 BobBuildIt 或第 4 名 ImagineLosing 的仓库。下列两项候选是按 TTUN 思路对本项目做的独立实现。近点候选在通往圈心的直线受阻时寻找能直接抵达的内圈落脚点。屋顶候选只在机甲比物资低超过 4 米、且物资比当地地面高超过 3 米时放弃追逐。

## 配对结果

所有对照使用首个官方版本 `97e0e58`。练习模式中，两个版本各自经历同一组初始场景。混战中，两个版本和 14 个固定机器人同场，两局交换出生位。积分为 16 减存活名次。下面的分数均来自本地模型。

| 候选或参考对象 | 模式与种子 | 对照均分 | 候选均分 | 观察 |
| --- | --- | ---: | ---: | --- |
| 内圈路点候选 | 练习，1201–1232 | 5.594 | 5.219 | 1 局提高、28 局持平、3 局下降。保留为独立实验补丁。 |
| 屋顶过滤候选 | 屋顶物资练习，1401–1432 | 3.500 | 3.531 | 2 局提高、26 局持平、4 局下降。对照实际捡到 4 件屋顶装备。保留为独立实验补丁。 |
| 完成空投占领 | 混战，5601–5608，各交换一次出生位 | 11.313 | 11.313 | 在 4 场触发 70 次决策。新增行为处理一个已复现的边界情况，目前没有测得均分收益。 |
| TTUN v7 原版 | 练习，4101–4116 | 4.563 | 1.688 | 官方榜上 Wolverines 高于 r1ver。这 16 个本地场景给出了相反顺序。 |
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
