# wgsrobot for Mech Royale

This entry builds on r1ver's ground-clearance version, which derives from the MIT-licensed [Wolverines economy submission](https://github.com/varchaskukreja/TTUN/tree/b7e9a6db75154271185035ef856859be37dff60e).

The bot scans while walking along clear ground, retains a selected supply route while aiming at nearby objects, and waits in cover without carrying over a jump or boost from navigation. Weapons share the torso aim and check their own readiness. Drone tracking uses the current observed health when available.

Build from the repository root with the included Dockerfile. The entry point uses Bun 1.4.4 and reads the map from the path in the match greeting.
