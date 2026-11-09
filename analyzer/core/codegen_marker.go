package core

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

// markerRe matches a nexssp:hash marker in either comment style.
//
// Examples that match:
//
//	// nexssp:hash=<64-hex>
//	<!-- nexssp:hash=<64-hex> -->
//
// The marker is emitted on its own line at the end of write-once files so
// the generator can distinguish "this file is still the template I wrote"
// from "the user edited this file".
var markerRe = regexp.MustCompile(
	`(?m)^[ \t]*(?://[ \t]*nexssp:hash=([0-9a-f]{64})|<!--[ \t]*nexssp:hash=([0-9a-f]{64})[ \t]*-->)[ \t]*\r?\n?`,
)

// StampContent returns content with a nexssp:hash marker appended. The
// marker carries the SHA-256 of content itself, so ContentIsUnmodified can
// later verify the file hasn't been touched since we wrote it.
//
// The path determines the comment style (markdown vs everything else).
func StampContent(content []byte, path string) []byte {
	sum := sha256.Sum256(content)
	marker := commentFor(path, hex.EncodeToString(sum[:]))

	out := make([]byte, 0, len(content)+len(marker)+2)
	out = append(out, content...)
	if len(content) > 0 && content[len(content)-1] != '\n' {
		out = append(out, '\n')
	}
	out = append(out, marker...)
	out = append(out, '\n')
	return out
}

// ContentIsUnmodified reports whether an on-disk write-once file still
// matches the content that last produced it.
//
// Algorithm:
//  1. Find the marker. No marker → not ours → modified (preserve).
//  2. Strip the marker line from the body.
//  3. SHA-256 the remaining bytes and compare against the stored hash.
//
// Returning true means "safe to overwrite with a new template". Returning
// false means "user edited this file — leave it alone" (unless --force).
func ContentIsUnmodified(onDisk []byte) bool {
	matches := markerRe.FindSubmatch(onDisk)
	if matches == nil {
		return false
	}

	var storedHash string
	for i := 1; i < len(matches); i++ {
		if len(matches[i]) > 0 {
			storedHash = string(matches[i])
			break
		}
	}
	if storedHash == "" {
		return false
	}

	body := markerRe.ReplaceAll(onDisk, nil)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]) == storedHash
}

// commentFor returns the marker string appropriate for a file extension.
// Markdown gets an HTML comment; everything else gets a line comment.
func commentFor(path, hash string) string {
	if strings.HasSuffix(path, ".md") {
		return "<!-- nexssp:hash=" + hash + " -->"
	}
	return "// nexssp:hash=" + hash
}
