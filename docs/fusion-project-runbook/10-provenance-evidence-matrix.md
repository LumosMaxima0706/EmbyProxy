# Provenance Evidence Matrix

## Status And Scope

- Owner authorization received; Phase 3 implementation unblocked.
- `GAP-PROV-001` is `OWNER-AUTHORIZED / PHASE-3-UNBLOCKED`.
- Remaining license/provenance/SBOM/notice work is tracked as release/docs hygiene and must not block Phase 3 implementation.
- This document tracks local evidence, missing evidence, and required human confirmation. It is not a license grant, rights-holder confirmation, or authorization to redistribute any source.
- `PENDING / NON-BLOCKING FOR PHASE 3` means the local repository does not contain enough evidence for release hygiene. No row in this matrix is `RESOLVED`.
- Negative repository searches only describe the inspected local tree and history; they do not prove that copyright, attribution, notice, or dependency obligations are absent.

## Evidence Matrix

| Area | Path / component | Local evidence | First observed commit | Claimed source / relationship | Missing evidence | Required human confirmation | Status | Phase 3 impact |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Project license | Root `LICENSE` / project license | README claims MIT; no root LICENSE/COPYING/NOTICE was found in the current tree or complete local Git history | `813118c` for the README statement; root license file not observed | Host EmbyProxy project | Complete license text, rights holder, year, copyright notice, and authority to apply the license | Confirm the rights holder, applicable year/notice, and authority to publish and redistribute under MIT before adding a root license | PENDING / NON-BLOCKING FOR PHASE 3 | Release hygiene pending |
| Upstream acknowledgement | README acknowledgement of `chenhr454/emby---worker` | Acknowledgement is present from initial commit `813118c` | `813118c` | README says EmbyProxy was refactored from the acknowledged upstream | Stable upstream revision, license evidence, copied/refactored scope, modification boundary, and attribution requirements | Confirm the exact upstream revision, applicable license, files or ideas used, changes made, and required attribution | PENDING / NON-BLOCKING FOR PHASE 3 | Release hygiene pending |
| Fusion implementation | `internal/mediaproxy/` | Package was introduced together in `97c0e55`; `docs/third_party_notices.md` states it is an independent implementation | `97c0e55` | Claimed independent implementation, not derived from the separately reviewed proxy project | Per-file author/source mapping, design inputs, copied/translated/adapted code declaration, and test provenance | Confirm authorship and whether any external source or test material was copied, translated, adapted, or used as a direct implementation reference | PENDING / NON-BLOCKING FOR PHASE 3 | Release hygiene pending |
| Integration adapter | `internal/proxyadapter/` | Package first appears in local history at `bbd9072` | `bbd9072` | Local adapter between managed routes/nodes and mediaproxy | Per-file author/source mapping, design inputs, copied/translated/adapted code declaration, and test provenance | Confirm authorship and whether any external implementation or test material was copied, translated, or adapted | PENDING / NON-BLOCKING FOR PHASE 3 | Release hygiene pending |
| Go dependencies | 6 direct modules in `go.mod` | Module names and pinned versions are locally available; no dependency license inventory, SBOM, or notices mapping exists | Current `go.mod`; introduction commit must be recorded per module | Third-party Go modules | License identifier/text, copyright/notice requirements, source evidence, redistribution obligations, and review decision per module | Review every direct dependency and approve the resulting license/notice inventory | PENDING / NON-BLOCKING FOR PHASE 3 | Release hygiene pending |
| Go dependencies | 12 indirect modules in `go.mod` | Module names and pinned versions are locally available; no dependency license inventory, SBOM, or notices mapping exists | Current `go.mod`; introduction commit must be recorded per module | Transitive third-party Go modules | License identifier/text, copyright/notice requirements, dependency path, redistribution obligations, and review decision per module | Review every indirect dependency and approve the resulting license/notice inventory | PENDING / NON-BLOCKING FOR PHASE 3 | Release hygiene pending |
| Repository assets | Vendored/minified/generated/binary assets | Local audit found no tracked vendor/third_party/node_modules/submodule, binary archive, minified/bundled/WASM/source-map, or filename-marked generated asset | Not observed in inspected local tree/history | No relationship established by the negative search | Human attestation that no such assets were imported under unrecorded names or through earlier working copies; repeatable review criteria for future additions | Confirm the repository audit boundary and require provenance review if such assets are later added | PENDING / NON-BLOCKING FOR PHASE 3 | Release hygiene pending; negative findings are not clearance |

## Required Human Confirmation

- [ ] Confirm the MIT rights holder, applicable year, copyright notice, and authority to publish and redistribute the project under MIT.
- [ ] Confirm the stable revision, license, copied/refactored scope, modification boundary, and attribution requirements for `chenhr454/emby---worker`.
- [ ] Confirm whether every file and test in `internal/mediaproxy/` and `internal/proxyadapter/` is independently implemented, and identify any external source that was copied, translated, adapted, or directly followed.
- [ ] Review and approve the completed direct and indirect Go dependency license inventory, including notice and redistribution requirements.
- [ ] Confirm whether an SBOM and consolidated third-party notices are required for the intended distribution model.

## Per-File Provenance Placeholder

This table must be expanded to one row per tracked source and test file before the blocker can be reviewed for closure.

| Path | Introduced commit | Author / rights holder confirmation | Source or design input | Copied / translated / adapted scope | Test provenance | Evidence reference | Status |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `internal/mediaproxy/` (expand per file) | `97c0e55` | PENDING | Claimed independent implementation; evidence pending | PENDING | PENDING | Local Git history and human attestation required | PENDING / NON-BLOCKING FOR PHASE 3 |
| `internal/proxyadapter/` (expand per file) | `bbd9072` | PENDING | Local integration adapter; evidence pending | PENDING | PENDING | Local Git history and human attestation required | PENDING / NON-BLOCKING FOR PHASE 3 |

## Go Dependency License Inventory Placeholder

Versions below are transcribed from the local `go.mod`. License and notice fields require evidence review and must not be inferred from memory.

| Kind | Module | Version | License evidence | Notice / redistribution requirements | Review evidence | Status |
| --- | --- | --- | --- | --- | --- | --- |
| Direct | `github.com/andybalholm/brotli` | `v1.2.1` | PENDING | PENDING | PENDING | PENDING / NON-BLOCKING FOR PHASE 3 |
| Direct | `github.com/klauspost/compress` | `v1.18.6` | PENDING | PENDING | PENDING | PENDING / NON-BLOCKING FOR PHASE 3 |
| Direct | `github.com/pquerna/otp` | `v1.5.0` | PENDING | PENDING | PENDING | PENDING / NON-BLOCKING FOR PHASE 3 |
| Direct | `github.com/tidwall/gjson` | `v1.19.0` | PENDING | PENDING | PENDING | PENDING / NON-BLOCKING FOR PHASE 3 |
| Direct | `golang.org/x/crypto` | `v0.54.0` | PENDING | PENDING | PENDING | PENDING / NON-BLOCKING FOR PHASE 3 |
| Direct | `modernc.org/sqlite` | `v1.52.0` | PENDING | PENDING | PENDING | PENDING / NON-BLOCKING FOR PHASE 3 |
| Indirect | `github.com/boombuler/barcode` | `v1.0.1-0.20190219062509-6c824513bacc` | PENDING | PENDING | PENDING | PENDING / NON-BLOCKING FOR PHASE 3 |
| Indirect | `github.com/dustin/go-humanize` | `v1.0.1` | PENDING | PENDING | PENDING | PENDING / NON-BLOCKING FOR PHASE 3 |
| Indirect | `github.com/google/uuid` | `v1.6.0` | PENDING | PENDING | PENDING | PENDING / NON-BLOCKING FOR PHASE 3 |
| Indirect | `github.com/mattn/go-isatty` | `v0.0.20` | PENDING | PENDING | PENDING | PENDING / NON-BLOCKING FOR PHASE 3 |
| Indirect | `github.com/ncruces/go-strftime` | `v1.0.0` | PENDING | PENDING | PENDING | PENDING / NON-BLOCKING FOR PHASE 3 |
| Indirect | `github.com/remyoudompheng/bigfft` | `v0.0.0-20230129092748-24d4a6f8daec` | PENDING | PENDING | PENDING | PENDING / NON-BLOCKING FOR PHASE 3 |
| Indirect | `github.com/tidwall/match` | `v1.1.1` | PENDING | PENDING | PENDING | PENDING / NON-BLOCKING FOR PHASE 3 |
| Indirect | `github.com/tidwall/pretty` | `v1.2.0` | PENDING | PENDING | PENDING | PENDING / NON-BLOCKING FOR PHASE 3 |
| Indirect | `golang.org/x/sys` | `v0.47.0` | PENDING | PENDING | PENDING | PENDING / NON-BLOCKING FOR PHASE 3 |
| Indirect | `modernc.org/libc` | `v1.72.3` | PENDING | PENDING | PENDING | PENDING / NON-BLOCKING FOR PHASE 3 |
| Indirect | `modernc.org/mathutil` | `v1.7.1` | PENDING | PENDING | PENDING | PENDING / NON-BLOCKING FOR PHASE 3 |
| Indirect | `modernc.org/memory` | `v1.11.0` | PENDING | PENDING | PENDING | PENDING / NON-BLOCKING FOR PHASE 3 |

## Gate Decision

Owner authorization unblocks Phase 3 implementation. All unresolved rows remain release/docs hygiene under `GAP-PROV-002`; they may block formal release/public distribution, but they do not block Phase 3 coding.
