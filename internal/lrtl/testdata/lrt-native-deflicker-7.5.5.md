# Native Deflicker single-pass observations

`lrt-native-deflicker-7.5.5.json` records 108 synthetic calls to installed
LRTimelapse 7.5.5 build 1059 on 2026-10-17. It contains original numeric
inputs and observations, without vendor code or photographs.

The independent Java reflection probe initialized `tQa`, `Rsa`, and `cTa`,
then constructed `cTa(null, true)` using its batch-mode constructor. No
window, UI constructor bypass or license-state change was needed. Every
`tQa(Object[4096])` frame received a measurement through `d(Double)` and a
current correction through `K(Double)`. `Rsa(-1)` held the frames.

The probe invoked the inherited void worker with parameters
`(int[], boolean, boolean, Rsa, float, double, int, int)`, using every frame,
the listed constant flag, additive=true, the selected accuracy threshold,
smoothing=0.5, the listed zero-based pass index and totalPasses=4. Target
values were retrieved from `b()` returning Double; final corrections from
`j()` returning Double.

Cases cross lengths 2,3,7,8,15,31; constant=false/true; accuracy=-1/0/1;
and pass indices 0,1,3. Measurements are `0.2+(i*37%19)/40.0`, and previous
corrections alternate between 0.12 and -0.2. Go compares both target and
correction with absolute tolerance 2e-8, using the independently tested
LOWESS component. Inputs must remain unchanged.

This establishes the dense additive pass's numerical behavior. The Go core
is connected to CLI XMP planning and Go-rendered pixel feedback through
`--calculation lrt-7.5.5`. Storage tests compare these observations through
modern/legacy internal slots and the global fallback. Sparse row selection,
native RAW development, surrounding UI/worker scheduling and complete native
multipass image rendering remain unverified.
