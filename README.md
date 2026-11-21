# River resource detour candidate

Adapted from [Wolverines v8 at b7e9a6db75154271185035ef856859be37dff60e](https://github.com/varchaskukreja/TTUN/tree/b7e9a6db75154271185035ef856859be37dff60e) under the included MIT license.

When the game avatar is outside the inner 55% of the announced next circle, the circle still has room to contract, and its contraction starts within 45 seconds or is already in progress, the resource planner considers the additional distance of travelling via each previously eligible pickup, cube or unlocked supply drop. It adds 0.65 times that additional distance to the normal item cost. Immediate pickups, candidate eligibility and the original threshold for choosing a different type of objective are preserved.
