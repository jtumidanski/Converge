# Task 21 Audit — Frontend scaffold, tooling, Tailwind, shadcn, Vitest

## Verdicts

- **Spec compliance: COMPLIANT** (with disclosed, justified deviations — see below).
- **Task quality: APPROVED**, with one Important scope-creep finding (unrequested/unused UI primitives) and one Important finding about a factually incorrect claim in the implementer's report (registry availability of `@pierre/diffs@1.4.0` and `eslint@10.9.1`). Neither blocks merge; both should be corrected or acknowledged.

## Findings summary

| Severity | Count |
|---|---|
| Critical | 0 |
| Important | 2 |
| Minor | 2 |

### Important

1. **Twelve unrequested shadcn UI primitives, all unused.** The brief's Files section lists only `apps/frontend/src/lib/utils.ts` and `src/lib/strings.ts` as `src/lib/**` deliverables — no `src/components/ui/*` files appear anywhere in the Files list. Step 3 does instruct `npx shadcn@latest add button input checkbox table badge card skeleton collapsible select dialog tooltip separator scroll-area`, so generating the twelve files is spec-directed, not fabricated by the implementer. However, grepping the entire `src/` tree (`grep -rn "components/ui" src --include=*.tsx | grep -v "^src/components/ui"`) shows **zero** references to any of the twelve components outside their own definition files — `App.tsx` is the brief's one-line placeholder (`<main>Converge</main>`) and imports none of them. This is scope the brief's Step 3 explicitly asked for, so it is not implementer-introduced scope creep, but it is still twelve files of dead/unused code sitting in the tree with no consumer until a later task (routes land in Task 25 per the brief's own comment in `App.tsx`). Not a defect to fix now — flagging per the audit brief's instruction to rule on it explicitly — but a real YAGNI cost the brief itself introduced, since Task 21's own Files section didn't ask for the components and no Task 21 code exercises them.
2. **Report contains a factually incorrect availability claim.** The report states "`@pierre/diffs`: brief pinned 1.4.0; registry only has 1.4.1 (**1.4.0 does not exist as a published version at check time**)" and similarly implies `eslint@10.9.1` was "unavailable." Both are false: `npm view @pierre/diffs@1.4.0 version time.created` returns `1.4.0` published `2025-12-10T01:23:02.951Z`, and `npm view eslint versions --json` includes both `10.9.1` and `10.10.0`. The actual versions installed (`@pierre/diffs@1.4.1`, `eslint@10.10.0`) are reasonable engineering choices (newest patch/point release), and neither causes a functional problem, but the stated *justification* for the drift is fabricated rather than verified — exactly the "claims proven false by measurement" pattern this plan has repeatedly produced. The version choice itself is fine; the report's reasoning for it is not accurate and should be corrected (e.g., "chose latest available patch" rather than "brief's pinned version does not exist").

### Minor

1. **`shadcn` package listed under `dependencies`, not `devDependencies`.** It is legitimately used at build time (`src/index.css` does `@import "shadcn/tailwind.css"`), so it is not dead weight, but it is a CLI/build-time tool rather than a runtime UI dependency; `devDependencies` would be the more conventional placement. Not functionally wrong given Vite bundles CSS at build time regardless of dependency section, but worth tightening later.
2. **`TestPresentReflectsIndexHTML` duplicates `Present()`'s own logic** (`fs.Stat(FS(), "index.html")`) rather than using an independent check, so it cannot catch a bug in the `fs.Stat` call itself (e.g., wrong path) — it can only catch `Present()` diverging from its own internal call (such as a hardcoded return). This is sufficient to defeat the original defect class the brief called out (a test that can't fail regardless of `Present()`'s value), and my own mutation reproduction below confirms it *does* fail on a hardcoded-`false` mutant, so this is not a blocking gap — just weaker independence than the report's phrasing ("independently stats index.html") implies.

## Centrepiece 1 — the twelve unrequested UI components, ruled

Brief Step 3, quoted in full:

> `npx shadcn@latest init`
> `npx shadcn@latest add button input checkbox table badge card skeleton collapsible select dialog tooltip separator scroll-area`
> Answer the init prompts with: style `new-york`, base colour `neutral`, CSS file `src/index.css`, CSS variables `yes`, alias `@/components` and `@/lib/utils`. ... Verify `components.json` exists and `src/components/ui/button.tsx` was generated.

**Ruling: the brief explicitly asked for all twelve components' generation (Step 3's `add` command lists exactly twelve component names) and explicitly asks the implementer to verify `button.tsx` was generated — this is brief-directed, not scope creep by the implementer.** The Files section omission is the same class of gap already ruled on for `tools/build-backend.sh` (R1: step bodies govern over the Files header). What *is* true, and worth surfacing plainly: this is YAGNI at the brief-authoring level — twelve files land with zero consumers in Task 21's own diff, and will sit unused until routing/feature work lands (the brief's own `App.tsx` placeholder comment says "until Task 25 adds routes"). I confirmed via `grep -rn "components/ui" src --include=*.tsx | grep -v "^src/components/ui"` that no file outside `src/components/ui/` imports any of the twelve. Recorded as Important above for visibility, not as an implementer defect.

## Centrepiece 2 — version and architecture drift, verified independently

| Claim | Verified? | Evidence |
|---|---|---|
| TypeScript resolved to 6.0.3, not 5.9/7 bracket | **True** | `package-lock.json`: `"node_modules/typescript": { "version": "6.0.3" }`; `package.json` declares `"typescript": "~6.0.2"` (matches Vite template's own pin, `~6.0.2` allows `6.0.3`). |
| Kept because it satisfies typescript-eslint's `<6.1.0` peer range | **True** | `npm view typescript-eslint@8.69.0 peerDependencies` → `{ typescript: '>=4.8.4 <6.1.0', eslint: '^8.57.0 \|\| ^9.0.0 \|\| ^10.0.0' }`. 6.0.3 satisfies both. |
| `tsc -b` genuinely works | **True** | Ran `npm run build` myself (fresh `dist` wipe first): `tsc -b && vite build` completed with exit 0 and produced real output (below). |
| `@pierre/diffs` 1.4.0 does not exist; 1.4.1 used | **FALSE claim, true version used.** | `npm view @pierre/diffs@1.4.0 version time.created` → `1.4.0`, published `2025-12-10`. 1.4.0 **does** exist. `1.4.1` was installed anyway (`package-lock.json` confirms `1.4.1`) — a defensible "use latest patch" choice, but the stated reason ("1.4.0 does not exist") is incorrect. See Important finding #2. |
| `eslint` 10.9.1 unavailable; 10.10.0 used | **FALSE claim, true version used.** | `npm view eslint versions --json` lists both `10.9.1` and `10.10.0`; both exist. `10.10.0` installed per `package-lock.json`. Same pattern as above. |
| shadcn CLI moved past `new-york`/`neutral` prompts; used `-t vite -p nova -b radix` | **True** | `components.json` shows `"style": "radix-nova"`, `"baseColor": "neutral"` — consistent with the report's description of the installed `shadcn@4.21.0` CLI's preset architecture. `package.json` confirms `"shadcn": "^4.21.0"` present. |
| Four extra runtime deps required by the `nova` preset: `radix-ui`, `tw-animate-css`, `@fontsource-variable/geist`, `shadcn` | **True, and all four are used (no dead weight)** | `radix-ui` imported in 9 of 12 generated components (`grep -rl "radix-ui" src/components/ui/*.tsx` → 9 hits); `tw-animate-css` and `@fontsource-variable/geist` are `@import`ed in `src/index.css`; `shadcn/tailwind.css` is `@import`ed in `src/index.css` for the CLI's generated custom variants. None of the four is present-but-unused. |
| `utils.ts` and all twelve components' `cn` import rewritten from shadcn's `cn` package back to `clsx`+`tailwind-merge` per the brief | **True** | `src/lib/utils.ts` matches the brief's Step 5 code exactly (`clsx`/`twMerge`, no `cn` package). `grep -rn 'from "cn"' src/components/ui/*.tsx` → 0 matches; `from "@/lib/utils"` → 12 matches (one per component). No `cn` package appears in `package.json`/lock file. |

**Assessment:** the substitutions are sound. No dependency in `package.json` is unimported/dead — I checked all four newly-arrived runtime deps individually. The one real problem is the report's inaccurate justification for the `@pierre/diffs`/`eslint` version choices (both target versions do exist in the registry); the choices themselves are harmless.

## Centrepiece 3 — npm script mutation testing (re-run independently)

All mutations below were run in `apps/frontend` (cwd), each reverted immediately after with `cp`/`mv` back to the original, and `git status --short` confirmed clean before and after the full sequence.

1. **`npm test` on zero matched tests.** Renamed `src/lib/__tests__/utils.test.ts` → `utils.test.ts.bak` (confirmed via `find src -name '*.test.ts*'` showing only the `.bak` file), ran `npm test`:
   ```
   No test files found, exiting with code 1
   include: src/**/*.test.{ts,tsx}
   ```
   Exit code 1 — **not** a silent green pass. Restored the file; `git status --short` clean.

2. **`npm run lint` on a real violation.** Appended `const unusedVar = 123;` to `src/App.tsx`, ran `npm run lint`:
   ```
   src/App.tsx
     5:7  error  'unusedVar' is assigned a value but never used  @typescript-eslint/no-unused-vars
   ✖ 1 problem (1 error, 0 warnings)
   ```
   Exit code 1. Restored `App.tsx` from a pre-mutation copy; confirmed byte-identical (`diff` reported "Files are identical").

3. **`npm run format:check` on a real formatting violation.** Appended `export const   badlyFormatted = {a:1,   b:2}` (no trailing semicolon/spacing) to `src/App.tsx`, ran `npm run format:check`:
   ```
   Checking formatting...
   [warn] src/App.tsx
   [warn] Code style issues found in the above file. Run Prettier with --write to fix.
   ```
   Exit code 1. Restored `App.tsx`; confirmed identical to the pre-mutation copy.

4. **`npm run build` output reality check.** Deleted `apps/backend/internal/ui/dist` entirely, recreated an empty dir with only `.gitkeep`, then ran `npm run build`:
   ```
   ../backend/internal/ui/dist/index.html                                              0.39 kB
   ../backend/internal/ui/dist/assets/geist-*.woff2 (5 files, 7–29 kB each)
   ../backend/internal/ui/dist/assets/index-*.css   45.45 kB
   ../backend/internal/ui/dist/assets/index-*.js   190.49 kB
   ✓ built in 191ms
   ```
   Exit code 0, real non-trivial assets present (`ls -la` confirmed `index.html` 393B plus an `assets/` dir). `emptyOutDir: true` deleted the tracked `.gitkeep` as expected — the Makefile's `touch $(BACKEND)/internal/ui/dist/.gitkeep` after the build step is exactly why that line exists; I restored it manually (`touch .gitkeep`) since I ran `vite build` directly rather than through `make build`. `git status --short` clean afterward (the `dist/*` contents are gitignored other than `.gitkeep`).

5. **Alias resolution (`@` → `src`) in Vite/Vitest/`tsc -b`.** Not mutated directly, but corroborated: `tsconfig.json` and `tsconfig.app.json` both declare `"paths": { "@/*": ["./src/*"] }` (mirrored per the brief), `vite.config.ts` and `vitest.config.ts` both declare `alias: { "@": path.resolve(import.meta.dirname, "./src") }`. The build (mutation 4) succeeded via `tsc -b && vite build`, and `npm test`'s baseline run (2/2 passing) exercises `@/lib/utils` and `@/lib/strings` imports from the test file — both Vite and Vitest resolve the alias, and `tsc -b` did not error on it. No silent-empty-resolution scenario found.

6. **Backend `Present()` mutation, re-run by me** (Centrepiece 4 overlap): with the real `dist` populated from mutation 4's build, hardcoded `Present()` to `return false`:
   ```go
   _, err := fs.Stat(FS(), "index.html")
   _ = err
   return false
   ```
   Ran `go test -run TestPresentReflectsIndexHTML -v ./internal/ui/...`:
   ```
   ui_test.go:23: Present() = false, want true (fs.Stat("index.html") err = <nil>)
   --- FAIL: TestPresentReflectsIndexHTML (0.00s)
   ```
   Matches the report's claimed quoted failure exactly. Reverted `embed.go`; `diff` against the pre-mutation copy confirmed identical.

7. **Dev-server proxy config**, read (not run, per the brief's own scope): `vite.config.ts`'s `server.proxy` maps `/api` and `/healthz` to `http://localhost:8080` with `changeOrigin: true` — matches the brief's Step 2 sample exactly (the brief's snippet also included `/healthz`, which the implementer kept).

**Conclusion: all 5 implementer-claimed mutations, plus my own additional lint/format/test-glob/build mutations, reproduce as claimed. No silently-green script found.**

## Centrepiece 4 — cross-boundary effect on the Go backend, verified

- Confirmed the mechanism: `apps/backend/internal/ui/embed.go`'s `Present()` calls `fs.Stat(FS(), "index.html")` against the `//go:embed all:dist` tree; before any frontend build, `dist/` holds only `.gitkeep`, so `Present()` is false.
- I ran `npm run build` (mutation 4 above) to populate `dist` with a real `index.html`, then ran the full backend gate from `apps/backend`:
  ```
  go test -race -count=1 ./...   → all 17 non-trivial packages ok, incl. internal/ui, cmd/converge
  go vet ./...                   → clean, no output
  go tool golangci-lint run      → 0 issues.
  CGO_ENABLED=0 go build ./...   → clean
  ```
  This genuinely exercised the "present" branch — `internal/ui`'s own test suite ran against a real embedded `index.html`, not a placeholder.
- No committed build output: `git status --short` after the build showed nothing untracked/modified outside `.gitkeep` restoration (which I performed manually and then re-confirmed clean). `.gitignore` has `apps/backend/internal/ui/dist/*` with `!apps/backend/internal/ui/dist/.gitkeep` as the sole tracked exception — correct.
- `TestPresentReflectsIndexHTML`'s independence is weaker than described (see Minor finding #2) but sufficient to catch the exact defect class the brief warned about (a test that can't fail regardless of `Present()`'s value) — confirmed via direct mutation re-run (Centrepiece 3, item 6).

## Confirmed, not new: `tools/build-backend.sh` / Files-header gap (R1)

Verified: `tools/build-backend.sh` exists, is executable, contains exactly the brief's Step 7 placeholder body (`echo "build-backend.sh: cross-compilation is wired in Task 27"`, `set -euo pipefail`), and is invoked from the Makefile's `build` target exactly as Step 7 specifies. The brief's Files header does not list it, but Step 7's body explicitly instructs creating it — consistent with the standing R1 ruling that step bodies govern over the Files header. Not a new finding.

## Standing-constraint checks

- **UI vocabulary contract.** `src/lib/strings.ts` contains exactly the ten keys the brief specifies (`provider`, `repository`, `base`, `includedChanges`, `combinedReview`, `conflict`, `finishReview`, `discardReview`, `buildReview`, `diagnostics`), verbatim values matching the brief's Step 5 snippet. Searched all committed `.tsx`/`.ts` files for `worktree`, `cherry-pick`, `synthetic branch` (case-insensitive): the only hit is inside `src/lib/__tests__/utils.test.ts`'s own list of forbidden terms (the test asserting they're absent from product copy) — no leakage into actual UI text. `App.tsx` (the only UI-bearing file besides the unused primitives) contains only "Converge".
- **Agreed deviations** (Vitest, thin fetch client, no `BaseService`): none of these apply yet in Task 21's actual diff — no service layer or API client exists at this stage — so nothing to flag either way.
- **Wire format contract**: confirmed via the diff's file list that no JSON:API/error-shape files were touched; Task 21 is scaffold-only.
- **No secrets**: grepped `package.json`, `package-lock.json`, and all of `src/` for token/secret/credential patterns — only false-positive matches inside third-party package names (`js-tokens`, `comma-separated-tokens`, `@csstools/css-tokenizer`) in the lock file. No secrets present.
- **No absolute home paths**: grepped all frontend config/source files plus `Makefile` and `tools/build-backend.sh` for `/home/` — zero hits.

## Frontend guidelines (FE-*) checklist, constructed from `.claude/skills/frontend-dev-guidelines/`

Task 21 has no components/services/forms/query hooks yet (those land in later tasks), so most of the skill's checklist (JSON:API types, React Query, service layer, forms+Zod) is **not yet applicable**. Scoped to what Task 21 actually contains:

- **FE-1 (cn() for conditional classes, never manual concatenation)** — PASS. `src/lib/utils.ts` matches the skill's canonical `cn()` implementation (`resources/patterns-styling.md`) verbatim: `clsx` + `tailwind-merge`.
- **FE-2 (no `any` type; TypeScript strict mode)** — PASS. `tsconfig.app.json` has `"strict": true`, `noUncheckedIndexedAccess`, `exactOptionalPropertyTypes`, `noImplicitOverride`, `noUnusedLocals`, `noUnusedParameters` all set per the brief; no `any` appears in any committed `.ts`/`.tsx` file (checked `App.tsx`, `main.tsx`, `utils.ts`, `strings.ts`, the test file).
- **FE-3 (named exports for components, not default)** — PASS. `App.tsx` exports `export function App()`, `main.tsx` imports it as a named import.
- **FE-4 (semantic CSS variables, not hardcoded colors)** — PASS for the one real UI surface (`App.tsx` uses `text-foreground`, a theme token, not a literal color).
- **FE-5 (tests written and verified before claiming completion)** — PASS. `npm test` genuinely runs and passes (2/2); I independently reproduced this and the mutation-catches-failure behavior.
- **FE-6 (loading state via skeleton, not spinner)** — N/A, no loading states exist yet in this scaffold task.
- **Testing framework deviation (Jest → Vitest)** — explicitly pre-agreed, not flagged.

No FE-* violations found in the code Task 21 actually shipped.

## Frontend/backend gate — full re-run results

**Frontend** (`apps/frontend`, after `npm ci` already satisfied by existing `node_modules`):
```
npm test        → 1 file, 2 tests passing (verified fresh, not just from the report)
npm run lint    → clean, 0 errors (verified fresh)
npm run format:check → clean (verified fresh)
npm run build   → tsc -b && vite build succeeded, real assets emitted (verified fresh, detailed above)
```

**Backend** (`apps/backend`, run after the frontend build above populated `internal/ui/dist` with a real `index.html`, genuinely flipping `Present()` to true):
```
go build ./...                  → exit 0
go test -race -count=1 ./...    → ok, all 17 non-trivial packages, including internal/ui and cmd/converge
go vet ./...                    → clean
go tool golangci-lint run       → 0 issues.
CGO_ENABLED=0 go build ./...    → clean
```

All clean, matching the implementer's report. Worktree left clean (`git status --short` empty) after all scratch mutations were reverted.
