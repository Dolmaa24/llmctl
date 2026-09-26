package notes

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode"
)

// draft is one note as the extraction model described it, before it is
// checked against the conversation it came from.
type draft struct {
	kind    string
	content string
	topics  []string

	// Zero-based indexes, or -1 when absent or unreadable: source into the
	// messages, replaces and linksTo into the active notes.
	source   int
	replaces int
	linksTo  int
}

var errNoNotes = errors.New("the reply holds no notes JSON")

// parseReply reads the notes out of the extraction model's reply.
//
// It is tolerant because a small model's formatting drifts even when its
// content is sound: the JSON may be fenced, wrapped in prose, a bare array,
// or carry numbers where strings were asked for. A field that cannot be read
// is dropped rather than failing its note, and a note that cannot be read is
// dropped rather than failing the reply. Only a reply with no notes JSON at
// all is an error.
func parseReply(reply string) ([]draft, error) {
	items, ok := findNotes(reply)
	if !ok {
		return nil, errNoNotes
	}
	var out []draft
	for _, item := range items {
		if d, ok := readDraft(item); ok {
			out = append(out, d)
		}
	}
	return out, nil
}

// findNotes returns the notes list from the first {"notes": ...} object in
// text. Failing that, it accepts the first bare array or lone note object.
func findNotes(text string) ([]any, bool) {
	var fallback []any
	found := false
	for i := 0; i < len(text); i++ {
		if text[i] != '{' && text[i] != '[' {
			continue
		}
		dec := json.NewDecoder(strings.NewReader(text[i:]))
		dec.UseNumber()
		var v any
		if dec.Decode(&v) != nil {
			continue
		}
		switch v := v.(type) {
		case map[string]any:
			if list, ok := v["notes"]; ok {
				items, _ := list.([]any) // null means no notes
				return items, true
			}
			if _, ok := v["content"]; ok && !found {
				fallback, found = []any{v}, true
			}
		case []any:
			if !found && (len(v) == 0 || isObject(v[0])) {
				fallback, found = v, true
			}
		}
		i += int(dec.InputOffset()) - 1
	}
	return fallback, found
}

func isObject(v any) bool {
	_, ok := v.(map[string]any)
	return ok
}

// readDraft reads one note, which a model may give as an object or, inside a
// notes list, as a bare string.
func readDraft(item any) (draft, bool) {
	d := draft{source: -1, replaces: -1, linksTo: -1}
	switch v := item.(type) {
	case string:
		d.content = v
	case map[string]any:
		d.content, _ = v["content"].(string)
		d.kind, _ = v["kind"].(string)
		d.topics = strs(v["topics"])
		d.source = ref(v["source"], "m")
		d.replaces = ref(v["replaces"], "n")
		d.linksTo = ref(v["links_to"], "n")
	default:
		return d, false
	}
	return d, strings.TrimSpace(d.content) != ""
}

// strs reads a list of strings, or one comma-separated string.
func strs(v any) []string {
	switch v := v.(type) {
	case string:
		return strings.Split(v, ",")
	case []any:
		var out []string
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// ref reads a handle such as "M3", "n2", "3" or 3 and returns its zero-based
// index. It returns -1 for anything else, including a handle of the wrong
// kind: a note cannot replace a message. Given several, it takes the first.
func ref(v any, prefix string) int {
	var s string
	switch v := v.(type) {
	case string:
		s = v
	case json.Number:
		s = v.String()
	case []any:
		if len(v) == 0 {
			return -1
		}
		return ref(v[0], prefix)
	default:
		return -1
	}
	fields := strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(fields) == 0 {
		return -1
	}
	n, err := strconv.Atoi(strings.TrimPrefix(strings.ToLower(fields[0]), prefix))
	if err != nil || n < 1 {
		return -1
	}
	return n - 1
}
