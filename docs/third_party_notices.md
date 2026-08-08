# Third-Party Source Review

This notice records the Phase 2B source and license review. It does not grant
rights beyond the licenses supplied by each upstream author.

## EmbyProxy

* Project: `hkfires/EmbyProxy`
* Reviewed revision: `0629ca3472a14d0fe7b65a36664295d0e5a648bf`
* Repository README statement: "This project is open sourced under the MIT
  License" (translated from the Chinese README).
* Repository license file: none found in the current tree or Git history.
* GitHub detected license metadata: none; the repository license endpoint
  returned not found when reviewed on 2026-08-09.

The README expresses an MIT intent, but the repository should add the complete
MIT license text and copyright notice before redistribution. Existing
EmbyProxy source remains the host project for this local POC.

## emby-reverse-proxy-go

* Project: `Gsy-allen/emby-reverse-proxy-go`
* Reviewed revision: `74297fddfe2c1cbadd82afb410e8c1de713dc1d5`
* Repository license file: none found in the current tree or Git history.
* README license statement: none found.
* GitHub detected license metadata: none; the repository license endpoint
  returned not found when reviewed on 2026-08-09.

No permission to copy, modify, merge, or redistribute the Gsy source can be
established from the repository. Phase 2B therefore does not copy, translate,
or create a derivative `internal/gsyproxy` package. Implementation is blocked
until the copyright holder publishes a compatible license or provides written
permission covering modification, combination, and redistribution.

If permission is later obtained, retain the upstream copyright and license
notice in this file and in any copied source as required by that license. Pin
the reviewed revision and record any local modifications.
