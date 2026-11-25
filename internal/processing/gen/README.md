# Generating the shield data and the test oracle

The Go port never retypes the shield's data. Every file under
`internal/processing/shield/data/` and `internal/processing/testdata/` comes from
the JavaScript that still owns it, either copied byte for byte or written by one
of the scripts in this folder.

Run these from the repository root after a change in `server/services/processing/`
or in `test/unit/*shield*`:

```sh
# rule packs: rules/*.js -> shield/data/pack_*.json
node internal/processing/gen/gen_shield_packs.js

# instruction and compression annotations -> shield/data/texts.json
node internal/processing/gen/gen_shield_texts.js

# rule/detector examples, span golden, redaction expectations, vault fixtures
node internal/processing/gen/gen_shield_testdata.js
```

Data that is copied as it is (do not edit the copies by hand):

```sh
cp server/services/processing/shield/categories.json            internal/processing/shield/data/categories.json
cp server/services/processing/shield/data/*.txt                 internal/processing/shield/data/
cp server/services/processing/shield/rules/secrets_gitleaks.json internal/processing/shield/data/pack_gitleaks.json
```

The upstream generation of the word lists and of the gitleaks pack is unchanged and
still lives in `scripts/build_shield_data.js` (US census names, Berlin's frequent
first names, FrequencyWords, BIP-39) and `scripts/import_gitleaks.js` (the gitleaks
secret rules, rewritten from RE2 to JavaScript). Those scripts are the source of
truth for the data; these three only carry its output into the Go packages.

## What the scripts write

| Script | Output | Why |
|---|---|---|
| `gen_shield_packs.js` | `shield/data/pack_contact.json`, `pack_crypto.json`, `pack_finance.json`, `pack_ids_de.json`, `pack_ids_eu.json`, `pack_ids_intl.json`, `pack_personal.json`, `pack_secrets.json` | The packs are JavaScript modules with RegExp literals; the Go registry reads the same rules as JSON, with each pattern's source and flags, in the same order the JS registry reads its files. The one rule with a backreference (`mac_address`) is rewritten here, with the reason recorded in the rule's `rewrite` field. |
| `gen_shield_texts.js` | `shield/data/texts.json` | The instruction a redacted conversation carries and the annotations the compression engines insert must match the JavaScript byte for byte, dashes and ellipses included. |
| `gen_shield_testdata.js` | `testdata/shield_rules.json`, `testdata/span_cases.json`, `testdata/corpus.json`, `testdata/vault.json` | The rule examples, the spans the JavaScript engine finds in about a hundred texts, the redaction of the corpus letter, and the vault values of the restore tests. The Go tests assert these, so a divergence shows up as a failed test rather than as a fixture someone typed. |

`span_cases.json` carries the awkward texts as well: digit runs of every length,
phone numbers in three notations, MRZ lines, IBANs with and without valid check
digits, secrets in JSON, in .env lines and in tool arguments, German letters and
ladder lists of everyday text. It is the parity net for the RE2 rewrites.
