# Contributing

Thanks for your interest. A few ground rules keep the project's licensing clean.

## License of your contributions (CLA)

mail-archive-tool is released to the public under **AGPL-3.0** (see `LICENSE`),
but the maintainer keeps the option to offer it under other terms as well
(e.g. a commercial license). For that to remain possible, every contribution
must come in under this lightweight Contributor License Agreement.

**By submitting a contribution (a pull request, patch, or any change), you agree
that:**

1. **You may license it.** The contribution is your original work, or you have
   the right to submit it, and it does not knowingly infringe anyone's rights.
2. **Inbound = outbound.** You license your contribution to the project and its
   users under **AGPL-3.0**, the same license as the project.
3. **Relicensing grant.** You *also* grant the project maintainer a perpetual,
   worldwide, non-exclusive, royalty-free, irrevocable license to use,
   reproduce, modify, and **sublicense/relicense** your contribution under any
   terms, **including proprietary or commercial licenses**. You retain your own
   copyright and may use your contribution however you like.

This grant is what lets the project keep both an open AGPL edition and the
option of a commercial one without having to track down every past contributor.
If you cannot agree to it, please open an issue to discuss before contributing.

We recommend also signing off your commits (`git commit -s`) to assert the
[Developer Certificate of Origin](https://developercertificate.org/).

## Testing discipline

This repo follows the assurance-kit methodology (see `CLAUDE.md`,
`docs/scenario-catalog.md`, `../assurance-kit/METHODOLOGY.md`):

- `go test ./...` must be green, including the `assureblock/` gates (block
  meta-test, covers-map, zero-assert).
- Every test carries a `// covers: MA-NN` marker mapping it to a numbered
  invariant; new behaviour adds a catalog row and a covering test.
- Fixes ship prove-fail → prove-pass; never weaken a gate to make it pass.
