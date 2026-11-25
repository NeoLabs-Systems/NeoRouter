// Package processing is NeoRouter's request processing, ported from
// server/services/processing: the privacy shield (redaction and restore), prompt
// compression, the sensitivity judgement the pool relay reads, and the optional
// on-server privacy filter (openai/privacy-filter through ONNX Runtime).
//
// # Where the data comes from
//
// The shield is data-driven, and its data is the JavaScript data: the rule packs,
// the categories and the word lists are copied mechanically, never retyped.
//
//	shield/data/categories.json     copied from shield/categories.json
//	shield/data/*.txt               copied from shield/data/*.txt
//	shield/data/pack_*.json         written by gen/gen_shield_packs.js from rules/*.js
//	shield/data/pack_gitleaks.json  copied from rules/secrets_gitleaks.json
//	shield/data/texts.json          written by gen/gen_shield_texts.js from
//	                                redaction.js (INSTRUCTION) and compression.js
//	                                (the annotations the engines insert)
//	testdata/*.json                 written by gen/gen_shield_testdata.js from the
//	                                JavaScript engine, so the port is compared with
//	                                the oracle instead of with a hand-written fixture
//
// See gen/README.md for the commands. The upstream generation of those files stays
// scripts/build_shield_data.js (word lists from the US census, Berlin's name lists
// and FrequencyWords, plus the BIP-39 list) and scripts/import_gitleaks.js (the
// gitleaks secret rules); this package consumes their output, it does not replace
// them.
//
// # JavaScript patterns in Go
//
// Go's regular expression engine has no lookaround and no backreferences, and the
// packs use lookaround as a word boundary in about ninety rules. compileJSPattern
// rewrites a JavaScript pattern into an equivalent Go one: a trailing boundary is
// consumed and taken off the span again, a leading one and any inner one become an
// empty marker group whose position is checked after the match, \s, \S and . are
// expanded to the characters JavaScript means, and capture groups are renumbered
// so a rule's group still names the same part of the match. One rule (mac_address)
// used a backreference and is rewritten by the generator into two alternatives.
// jspattern_test.go pins each of those rewrites.
//
// # Known differences from the JavaScript
//
//   - Messages are map[string]any, so a message object's JSON keys come out in
//     alphabetical order instead of the order the model wrote them. Values, keys,
//     nulls and the wire shapes are unchanged.
//   - Minified JSON and tabulated JSON lists (compression) sort object keys, where
//     JSON.stringify kept the insertion order.
//   - The privacy filter needs the ONNX Runtime shared library and the model
//     weights (about 900 MB) in DATA_DIR/models; without them the filter reports
//     that it cannot load and the shield keeps using its pattern detectors, which
//     is what the JavaScript did when the transformers download failed.
package processing
