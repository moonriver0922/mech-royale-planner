# River moving-frame dodge candidate

Based on the MIT-licensed [Wolverines economy submission](https://github.com/varchaskukreja/TTUN/tree/b7e9a6db75154271185035ef856859be37dff60e), this virtual game bot checks the navigation grid before taking a short direct route during ground-level roaming, healing, loot collection or zone travel. It checks cell clearance and diagonal corners. For blocked shortcuts it requests the upstream grid path using the same time budget.

When the intended movement approximately matches the mech's current horizontal velocity, the bot checks a bullet's closest approach in the moving mech's frame. It uses the original dodge calculation when the movement command calls for braking or a substantial turn.

Fixed-clock local comparisons use the [5.10 practice entry](https://github.com/moonriver0922/wgsrobot/commit/af337d5c1275d03478685bd4936c8a551fa6b200) as baseline. The development set (60201–60216) averaged −0.50 points, and two holdout sets (61101–61116 and 61501–61524) averaged +0.35 points over 40 games. In mixed free-for-all, the average difference was −0.125 points over 24 games on seeds 61201–61212 with swapped starting positions. Official practice uses different opponents and hidden seeds.
