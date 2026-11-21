# River ground-clearance candidate

Based on the MIT-licensed [Wolverines economy submission](https://github.com/varchaskukreja/TTUN/tree/b7e9a6db75154271185035ef856859be37dff60e), this virtual game bot checks the navigation grid before taking a short direct route during ground-level roaming, healing, loot collection or zone travel. It checks cell clearance and diagonal corners. For blocked shortcuts it requests the upstream grid path using the same time budget.

For calm zone travel the radial destination stays in place when it has a ground route. A blocked destination already projects to nearby floor in the upstream route finder. That existing route stays in use unless its projected floor cell is disconnected from the avatar or outside the next circle. In those two cases, the bot seeks a connected floor cell inside the circle.
