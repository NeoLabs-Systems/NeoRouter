// Package processing is NeoRouter's request processing: the privacy shield
// (redaction and restore), prompt compression, the sensitivity judgement the
// pool relay reads, and the optional on-server privacy filter
// (openai/privacy-filter through ONNX Runtime).
//
// # Where the data comes from
//
// The shield is data-driven: the rule packs, categories, word lists and texts in
// shield/data are committed. The word lists come from the US census, Berlin's
// name lists, FrequencyWords and the BIP-39 list, and the secret rules from
// gitleaks (credits in the README).
//
// # Rule patterns
//
// The packs are written in JavaScript regular expression syntax. Go's engine has
// no lookaround and no backreferences, and the packs use lookaround as a word
// boundary in about ninety rules. compileJSPattern rewrites a pattern into an
// equivalent Go one: a trailing boundary is consumed and taken off the span
// again, a leading one and any inner one become an empty marker group whose
// position is checked after the match, \s, \S and . are expanded to the
// characters JavaScript means, and capture groups are renumbered so a rule's
// group still names the same part of the match. jspattern_test.go pins each of
// those rewrites.
//
// # Behaviour to know about
//
//   - Messages are map[string]any, so a message object's JSON keys come out in
//     alphabetical order instead of the order the model wrote them. Values, keys,
//     nulls and the wire shapes are unchanged.
//   - Minified JSON and tabulated JSON lists (compression) sort object keys.
//   - The privacy filter needs the ONNX Runtime shared library and the model
//     weights (about 900 MB) in DATA_DIR/models; without them the filter reports
//     that it cannot load and the shield keeps using its pattern detectors.
package processing
