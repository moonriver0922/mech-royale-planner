# Mech Royale close-of-phase crossing

Based on Wolverines' MIT [TTUN economy bot](https://github.com/varchaskukreja/TTUN/tree/b7e9a6db75154271185035ef856859be37dff60e). The supplied map module and JSON-lines wrapper are unchanged. The original MIT notice is in LICENSE.

When a shrink is in progress and we are outside its next safe circle, compare time remaining to walking time to a small interior margin. If the budget is within two seconds, prioritize the existing circle waypoint. This resolves an action priority conflict that previously waited for the outside-zone damage flag. The source bot’s existing item selection, combat and movement actions are retained.
