package server

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	pb "github.com/livegrep/livegrep/src/proto/go_proto"
)

var pieceRE = regexp.MustCompile(`\(|(?:^([a-zA-Z0-9-_]+):|\\.)| `)

var knownTags = map[string]bool{
	"file":        true,
	"-file":       true,
	"path":        true,
	"-path":       true,
	"repo":        true,
	"-repo":       true,
	"tags":        true,
	"-tags":       true,
	"case":        true,
	"lit":         true,
	"max_matches": true,
}

func onlyOneSynonym(ops map[string]string, op1 string, op2 string) (string, error) {
	if ops[op1] != "" && ops[op2] != "" {
		return "", fmt.Errorf("Cannot provide both %s: and %s:, because they are synonyms", op1, op2)
	}
	if ops[op1] != "" {
		return ops[op1], nil
	}
	return ops[op2], nil
}

func ParseQuery(query string) (pb.Query, error) {
	ops := make(map[string]string)
	key := ""
	q := strings.TrimSpace(query)

	for {
		m := pieceRE.FindStringSubmatchIndex(q)
		if m == nil {
			ops[key] += q
			break
		}

		ops[key] += q[:m[0]]
		match := q[m[0]:m[1]]
		q = q[m[1]:]

		if match == " " {
			// A space: Ends the operator, if we're in one.
			if key == "" {
				ops[key] += " "
			} else {
				key = ""
			}
		} else if match == "(" {
			// A parenthesis. Nothing is special until the
			// end of a balanced set of parenthesis
			p := 1
			i := 0
			esc := false
			var w bytes.Buffer
			for i < len(q) {
				// We decode runes ourselves instead
				// of using range because exiting the
				// loop with i = len(q) makes the edge
				// cases simpler.
				r, l := utf8.DecodeRuneInString(q[i:])
				i += l
				switch {
				case esc:
					esc = false
				case r == '\\':
					esc = true
				case r == '(':
					p++
				case r == ')':
					p--
				}
				w.WriteRune(r)
				if p == 0 {
					break
				}
			}
			ops[key] += match + w.String()
			q = q[i:]
		} else if match[0] == '\\' {
			ops[key] += match
		} else {
			// An operator. The key is in match group 1
			newKey := match[m[2]-m[0] : m[3]-m[0]]
			if key == "" && knownTags[newKey] {
				key = newKey
			} else {
				ops[key] += match
			}
		}
	}

	var out pb.Query
	var err error
	if out.File, err = onlyOneSynonym(ops, "file", "path"); err != nil {
		return out, err
	}
	out.Repo = ops["repo"]
	out.Tags = ops["tags"]
	if out.NotFile, err = onlyOneSynonym(ops, "-file", "-path"); err != nil {
		return out, err
	}
	out.NotRepo = ops["-repo"]
	out.NotTags = ops["-tags"]
	var bits []string
	for _, k := range []string{"", "case", "lit"} {
		bit := strings.TrimSpace(ops[k])
		if k == "lit" {
			bit = regexp.QuoteMeta(bit)
		}
		if len(bit) != 0 {
			bits = append(bits, bit)
		}
	}

	if len(bits) > 1 {
		return out, errors.New("You cannot provide multiple of case:, lit:, and a bare regex")
	}

	if len(bits) > 0 {
		out.Line = bits[0]
	}

	if out.Line == "" && out.File != "" {
		out.Line = out.File
		out.File = ""
		out.FilenameOnly = true
	}

	if _, ok := ops["case"]; ok {
		out.FoldCase = false
	} else if _, ok := ops["lit"]; ok {
		out.FoldCase = false
	} else {
		out.FoldCase = strings.IndexAny(out.Line, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") == -1
	}
	if v, ok := ops["max_matches"]; ok && v != "" {
		i, err := strconv.Atoi(v)
		if err == nil {
			out.MaxMatches = int32(i)
		} else {
			return out, errors.New("Value given to max_matches: must be a valid integer")
		}
	} else {
		out.MaxMatches = 0
	}

	return out, nil
}
