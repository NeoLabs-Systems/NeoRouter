package processing

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
)

// The texts whose exact bytes matter, copied out of the JavaScript sources by
// gen/gen_shield_texts.js: the instruction a redacted conversation carries and
// the annotations the compression engines insert.
//
//go:embed shield/data/texts.json
var textsJSON []byte

type textBundle struct {
	RedactionInstruction string `json:"redactionInstruction"`
	RepeatedLine         string `json:"repeatedLine"`
	OmittedChars         string `json:"omittedChars"`
	DuplicateBlock       string `json:"duplicateBlock"`
	MaskedTool           string `json:"maskedTool"`
	EarlierMessage       string `json:"earlierMessage"`
}

var (
	textsOnce   sync.Once
	textBundleV textBundle
	textsErr    error
)

// texts is the embedded text bundle.
func texts() textBundle {
	textsOnce.Do(func() {
		if err := json.Unmarshal(textsJSON, &textBundleV); err != nil {
			textsErr = fmt.Errorf("processing: texts.json: %w", err)
			return
		}
		if textBundleV.RedactionInstruction == "" {
			textsErr = fmt.Errorf("processing: texts.json is empty")
		}
	})
	if textsErr != nil {
		panic(textsErr)
	}
	return textBundleV
}
