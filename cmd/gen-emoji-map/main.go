// Command gen-emoji-map (re)generates internal/webapp/static/emoji-map.json,
// the :shortcode: -> emoji lookup table used by special-dates.js's live
// preview. It's generated from github.com/kyokomi/emoji/v2's code map --
// the exact same table internal/webapp/domain.go's resolveIconShortcode
// uses on the Go side -- so both stay in sync automatically. Run it with
// `go run ./cmd/gen-emoji-map` after upgrading the emoji dependency.
package main

import (
	"encoding/json"
	"log"
	"os"

	"github.com/kyokomi/emoji/v2"
)

func main() {
	const out = "internal/webapp/static/emoji-map.json"
	b, err := json.Marshal(emoji.CodeMap())
	if err != nil {
		log.Fatalf("marshaling emoji code map: %v", err)
	}
	if err := os.WriteFile(out, b, 0o644); err != nil {
		log.Fatalf("writing %s: %v", out, err)
	}
	log.Printf("wrote %s (%d entries)", out, len(emoji.CodeMap()))
}
