# r1ver 官方练习记录

每次提交在主办方的 48 局练习中评测，每局积分为 16 减存活名次。这里的分数来自队伍提交回执。模拟器的逐局结果另行记录。各版本的 Git 链接固定到实际评分的提交。[官方赛道和榜单](https://www.joinbuilders.org/buildathon/tracks/builders)

| 香港时间 | 版本 | 官方均分 | 回执 |
| --- | --- | ---: | --- |
| 2026-11-21 10:26 | [Lance 衍生版，圈内目的地承诺与慢行](https://github.com/moonriver0922/mech-royale-planner/tree/97e0e588ac9fb49533602a738344cb3fdc8b1551) | 4.54 | `BB-407987E8` |
| 2026-11-21 10:51 | [正常移速与补给占领修正](https://github.com/moonriver0922/mech-royale-planner/tree/8a536b50fb36da9bd664f7b3eaf6a92c54e53b35) | 4.67 | `BB-2DE41BE7` |
| 2026-11-21 10:54 | [TTUN v8-econ 衍生版，按升级价值选方块堆](https://github.com/moonriver0922/mech-royale-planner/tree/96848df450cb7b49c1cce870fc964affcfa1bb47) | 5.08 | `BB-74CEE7A0` |
| 2026-11-21 11:01 | [TTUN v8-econ 衍生版，短距离地面路径检查](https://github.com/moonriver0922/mech-royale-planner/tree/af337d5c1275d03478685bd4936c8a551fa6b200) | 5.10 | `BB-EAEAF89A` |

v8-econ 的上游源码固定于 [b7e9a6d](https://github.com/varchaskukreja/TTUN/tree/b7e9a6db75154271185035ef856859be37dff60e)。在 9151–9182 的 32 个本地同场景种子里，方块堆策略比 v8-econ 原版低 0.56 分。8 个混战种子各交换一次出生位，场均低 0.13 分。5.08 分是整个版本对官方对手的成绩，与先前的 4.67 分相比同时换了策略基础，不能把 0.41 分增量单独归因给方块选择。


11:03 核对时，r1ver 以 5.10 排第 4，AT 以 5.48 排第 1。最高分版本已由系统选入决赛。48 局按整数计分，两个显示分数分别对应 245 和 263 总分。超过同一榜首至少需要 264 总分，相当于在这 48 局中累计再提高 19 个名次。

## 用已知官方分数校准模拟器

在同一组 16 个新种子（19551–19566）上复测全部四个官方版本，启用屋顶物资和已校正的水平运动。Python 两个版本分别使用各自 Dockerfile 中的移动开关，两个 TypeScript 版本使用同一固定计算时钟。[逐种子积分](calibration-19551.csv)

| 版本 | 官方均分（48 局） | 本地均分（16 局） |
| --- | ---: | ---: |
| 97e0e58 慢行 | 4.54 | 3.5625 |
| 8a536b5 正常移速 | 4.67 | 4.6875 |
| 96848df 方块堆 | 5.08 | 4.6875 |
| af337d5 地面通行检查 | 5.10 | 4.4375 |

四个版本形成六组高低比较。本地有三组与官方同序、一组持平、两组反序。官方相差 0.41 分的两版在本地打平，另一组相差 0.43 分的版本顺序反转。后续用本地回放找出行为失误、比较同一机制的效果，再用官方练习确认名次。官方使用隐藏对手和场景，校准的本地运行时为 Python 3.12 和 Node 24，参赛镜像为 Python 3.13.9 和 Bun 1.4.4。

## 下一轮对照

各行分差均为候选减去所列基线的本地场均积分。两种练习模拟器的对手和官方内置对手有差异。实时地图规划受 CPU 负载影响。单独固定寻路工作量后，部分正向差异消失。后续在同一批新种子上比较时，应同时记录进圈、路径选择等机制实际触发了几次。

| 候选和基线 | 实时练习 | 固定寻路工作量的练习 | 换出生位混战 |
| --- | --- | --- | --- |
| 地面路径检查，对 v8-econ 原版 | 旧水平运动模型的 32 局探索 +1.56，另 32 局 +1.13。校正模型另 24 局 −0.17 | 旧模型 16 局 −0.19，15 局持平 | 旧模型 16 局 −0.44 |
| [可达缩圈目标](https://github.com/moonriver0922/mech-royale-planner/tree/29d8eeea5a55f3ce952bf275a642f589b733dbba)，对 5.10 版 | 校正模型 32 局 +2.19 | 16 局 −1.38。6 局更换过目标，2 局名次下降，其余持平 | 校正模型 16 局 −1.00 |
| [缩圈截止时间](https://github.com/moonriver0922/mech-royale-planner/tree/d423f07c9d5ead1916da9b0fc43cc35a2d615137)，对 v8-econ 原版 | 32 局 +0.16 | 另 16 局 +0.06，1 局提高、15 局持平 | 这次实验未测 |

[按额外路程估值的物资候选](https://github.com/moonriver0922/mech-royale-planner/tree/b3f7a424fcbed55506cbeb5e448ee8a2c3ade42f)在临近缩圈时，用经由物资再去圈内所增加的距离给目标排序。它在 32 个开发种子中的固定计算量均分差为 +0.219，另 32 个留出种子为 −0.219；单独用真实时钟测试 16 个种子为 +0.813。它在多局中确实改变了资源选择，适合与改变进圈时机的版本分别验证。

可达目标的几何检查在 32 组随机圈、32 个方位、五个非零目标半径上共测试 5,120 次。在 2,120 次原目标不可达时，都成功找到圈内同一连通区域的地面格。这个机制能纠正地图目标点，但比赛得分的三组测量方向不一致。进一步检查发现，上游寻路已能把其中 2,002 个目标投影到圈内可达地面。[限制版候选](https://github.com/moonriver0922/mech-royale-planner/tree/dfdce1838cdb3438f56c2f64b58f75c4f1092a75)只修正上游投影仍然不可达或已在圈外的 118 个目标。固定计算量检查和新留出合计 48 局，限制版与 5.10 版所有名次相同；另 8 组交换出生位的混战积分也全部持平。这项几何修正能留作回归测试，本轮没有显示得分优势。

从含模拟器的默认分支运行以下命令。模拟器分别从 Git 取出候选与基线。

```sh
git fetch origin
python3 -B tools/simulate.py --kit ../mechfight-kit --roof-loot \
  --bot git:29d8eee:mech-royale/bot/bot.ts --baseline git:af337d5:mech-royale/bot/bot.ts \
  --seed 15601 --matches 32
python3 -B tools/ffa/league.py --kit ../mechfight-kit \
  --bot git:29d8eee:mech-royale/bot/bot.ts --baseline git:af337d5:mech-royale/bot/bot.ts \
  --seed 15701 --pairs 8
```
