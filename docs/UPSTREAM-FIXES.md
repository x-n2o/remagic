# Personal fork integration

Checked against upstream on 2026-10-18. The repair is maintained in
`x-n2o/remagic`, `x-n2o/riddle` and `x-n2o/quill`; it does not create duplicate
upstream pull requests.

| Fix | Existing upstream work | Personal integration |
| --- | --- | --- |
| AppLoad hooks for 3.28+ | [rm-appload #59](https://github.com/asivery/rm-appload/pull/59), rmitchellscott, merged | Use the official v0.6.0 release containing the fix. |
| Required resource table and compatible AppLoad setup | [remagic #3](https://github.com/MaximeRivest/remagic/pull/3), frapeti, `a99f90e` | Merge with original authorship; keep the recovery command and doctor diagnostics. Resolve setup against the tested shared scripts and release selection. The temporary embedded AppLoad binary and duplicate rebuild scripts are superseded. |
| Empty reasoning setting | [Riddle #14](https://github.com/MaximeRivest/riddle/pull/14), vnnkl, `a8558d1` | Merge with original authorship. The same trim/filter behavior remains in the tested helper. |
| New display ABI | [Quill #5](https://github.com/MaximeRivest/quill/pull/5), nedcoinn, `e7de0c9` and `6d0fded` | Both original commits are retained in the personal Quill fork, followed by build/test and device-result changes. |
| Historical bundled-Quill ABI fix | [Riddle #18](https://github.com/MaximeRivest/riddle/pull/18), frapeti | Superseded here by Quill #5. Its files belong to the retired monorepo adapter; that implementation is not reintroduced into the current split projects. |

Our additional changes include managed SSH authentication, firmware/archive
selection, hook staging and recovery checks, the corrected catalog checksum,
tablet-library builds, focused tests, responsive sleep and optional cinematic
pacing. Existing upstream contributions retain their authors and commit IDs.

## Contribution rules found

The three upstream repositories expose MIT licenses, but no dedicated
`CONTRIBUTING.md`, PR template, CLA or DCO policy was found in their current
trees or GitHub community profiles. No owner-level `.github` defaults repository
was present at the time of this check.

Quill additionally documents [clean-room provenance](https://github.com/MaximeRivest/quill/blob/main/CLEANROOM.md)
and its [public C ABI policy](https://github.com/MaximeRivest/quill#c-abi-policy).
Its vendor libraries must remain local, compatibility is verified through the
public interface and black-box tests, and the old adapter must not be used as
a reference for the independent implementation. This integration preserves
those boundaries and does not add proprietary device libraries or credentials.

Hardware results and their remaining manual verification boundaries are in
[BETA-3.29.md](BETA-3.29.md), Riddle's `VALIDATION.md`, and Quill's
`HARDWARE-RESULTS.md`.
