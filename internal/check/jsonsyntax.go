package check

import (
	"fmt"
	"strconv"
	"strings"
)

// A CPython-compatible JSON syntax checker.
//
// code-block-syntax-error reports a failing JSON block with the decoder's own message and
// the line the decoder blamed. Go's encoding/json says different things in a
// different vocabulary and reports a byte offset rather than a line, so the
// diagnostic is produced here instead: this is a position-tracking port of
// CPython's json.decoder, which answers the two questions the rule asks --
// the message and the line -- and discards the decoded value, which the rule
// never wanted.
//
// Positions are CHARACTER indices, as Python's are, so the scan runs over
// runes. The line is derived the way JSONDecodeError derives it: the newlines
// before the offending position, plus one.

// jsonSyntaxError is a refused JSON document: the decoder's bare message and
// the character position it was raised at.
type jsonSyntaxError struct {
	// Message is the decoder's own message, without the position suffix
	// JSONDecodeError appends -- the "msg" attribute the rule reads.
	Message string
	// Pos is the 0-based character offset the decoder blamed.
	Pos int
}

// Error renders the refusal with the position it was raised at. The message
// alone is what code-block-syntax-error reports; this spelling exists for a caller that
// prints the error whole.
func (e *jsonSyntaxError) Error() string {
	return fmt.Sprintf("%s (char %d)", e.Message, e.Pos)
}

// Line is the 1-based line Pos sits on within document.
func (e *jsonSyntaxError) Line(document []rune) int {
	line := 1
	for index := 0; index < e.Pos && index < len(document); index++ {
		if document[index] == '\n' {
			line++
		}
	}
	return line
}

// jsonWhitespace is the whitespace the decoder skips between tokens --
// WHITESPACE_STR in json.decoder, which is narrower than Unicode whitespace.
const jsonWhitespace = " \t\n\r"

// jsonBackslashEscapes are the one-character escapes the decoder accepts.
var jsonBackslashEscapes = map[rune]bool{
	'"': true, '\\': true, '/': true,
	'b': true, 'f': true, 'n': true, 'r': true, 't': true,
}

// jsonScanner scans one document, tracking its position in characters.
type jsonScanner struct {
	doc []rune
}

// CheckJSONSyntax reports whether text is a JSON document CPython would
// accept, and answers with CPython's own message and line when it is not.
func CheckJSONSyntax(text string) *jsonSyntaxError {
	scanner := &jsonScanner{doc: []rune(text)}
	end := scanner.skipWhitespace(0)
	value, end, err := scanner.scanOnce(end)
	if err != nil {
		return err
	}
	_ = value
	end = scanner.skipWhitespace(end)
	if end != len(scanner.doc) {
		return &jsonSyntaxError{Message: "Extra data", Pos: end}
	}
	return nil
}

// skipWhitespace returns the first index at or after idx that is not
// whitespace the decoder skips.
func (s *jsonScanner) skipWhitespace(idx int) int {
	for idx < len(s.doc) && strings.ContainsRune(jsonWhitespace, s.doc[idx]) {
		idx++
	}
	return idx
}

// at returns the character at idx, and false when idx is past the end.
func (s *jsonScanner) at(idx int) (rune, bool) {
	if idx < 0 || idx >= len(s.doc) {
		return 0, false
	}
	return s.doc[idx], true
}

// has reports whether the document carries literal at idx.
func (s *jsonScanner) has(idx int, literal string) bool {
	runes := []rune(literal)
	if idx+len(runes) > len(s.doc) {
		return false
	}
	for offset, char := range runes {
		if s.doc[idx+offset] != char {
			return false
		}
	}
	return true
}

// scanOnce scans the one value starting at idx.
//
// The "no value here" condition is the decoder's StopIteration, which every
// caller converts into "Expecting value" at the position it stopped at; here
// it comes back as that error directly, because every caller does the same
// thing with it.
func (s *jsonScanner) scanOnce(idx int) (any, int, *jsonSyntaxError) {
	nextChar, present := s.at(idx)
	if !present {
		return nil, idx, &jsonSyntaxError{Message: "Expecting value", Pos: idx}
	}

	switch {
	case nextChar == '"':
		return s.scanString(idx + 1)
	case nextChar == '{':
		return s.scanObject(idx + 1)
	case nextChar == '[':
		return s.scanArray(idx + 1)
	case nextChar == 'n' && s.has(idx, "null"):
		return nil, idx + 4, nil
	case nextChar == 't' && s.has(idx, "true"):
		return true, idx + 4, nil
	case nextChar == 'f' && s.has(idx, "false"):
		return false, idx + 5, nil
	}

	if end, matched := s.matchNumber(idx); matched {
		return nil, end, nil
	}
	switch {
	case nextChar == 'N' && s.has(idx, "NaN"):
		return nil, idx + 3, nil
	case nextChar == 'I' && s.has(idx, "Infinity"):
		return nil, idx + 8, nil
	case nextChar == '-' && s.has(idx, "-Infinity"):
		return nil, idx + 9, nil
	}
	return nil, idx, &jsonSyntaxError{Message: "Expecting value", Pos: idx}
}

// matchNumber matches the decoder's number pattern at idx, returning the index
// after the match and whether one was found.
//
// The pattern is NUMBER_RE: an optional minus, then either "0" or a non-zero
// digit run, then an optional fraction, then an optional exponent. The
// fraction and the exponent are each taken only when complete, exactly as the
// regex's optional groups are.
func (s *jsonScanner) matchNumber(idx int) (int, bool) {
	cursor := idx
	if char, present := s.at(cursor); present && char == '-' {
		cursor++
	}
	char, present := s.at(cursor)
	if !present {
		return idx, false
	}
	switch {
	case char == '0':
		cursor++
	case char >= '1' && char <= '9':
		for {
			next, ok := s.at(cursor)
			if !ok || next < '0' || next > '9' {
				break
			}
			cursor++
		}
	default:
		return idx, false
	}

	if dot, present := s.at(cursor); present && dot == '.' {
		digits := cursor + 1
		for {
			next, ok := s.at(digits)
			if !ok || next < '0' || next > '9' {
				break
			}
			digits++
		}
		if digits > cursor+1 {
			cursor = digits
		}
	}

	if exponent, present := s.at(cursor); present && (exponent == 'e' || exponent == 'E') {
		scan := cursor + 1
		if sign, ok := s.at(scan); ok && (sign == '+' || sign == '-') {
			scan++
		}
		digits := scan
		for {
			next, ok := s.at(digits)
			if !ok || next < '0' || next > '9' {
				break
			}
			digits++
		}
		if digits > scan {
			cursor = digits
		}
	}

	return cursor, true
}

// scanString scans a string whose opening quote sits at idx-1.
func (s *jsonScanner) scanString(idx int) (any, int, *jsonSyntaxError) {
	begin := idx - 1
	end := idx
	for {
		char, present := s.at(end)
		if !present {
			return nil, end, &jsonSyntaxError{
				Message: "Unterminated string starting at", Pos: begin,
			}
		}
		if char == '"' {
			return "", end + 1, nil
		}
		if char < 0x20 {
			return nil, end, &jsonSyntaxError{
				Message: "Invalid control character at", Pos: end,
			}
		}
		if char != '\\' {
			end++
			continue
		}
		// A backslash: the escape it opens starts at end+1.
		esc, present := s.at(end + 1)
		if !present {
			return nil, end + 1, &jsonSyntaxError{
				Message: "Unterminated string starting at", Pos: begin,
			}
		}
		if esc != 'u' {
			if !jsonBackslashEscapes[esc] {
				return nil, end, &jsonSyntaxError{
					Message: `Invalid \escape`, Pos: end,
				}
			}
			end += 2
			continue
		}
		code, err := s.decodeUnicodeEscape(end + 1)
		if err != nil {
			return nil, err.Pos, err
		}
		end += 6
		if code >= 0xd800 && code <= 0xdbff && s.has(end, `\u`) {
			low, lowErr := s.decodeUnicodeEscape(end + 1)
			if lowErr != nil {
				return nil, lowErr.Pos, lowErr
			}
			if low >= 0xdc00 && low <= 0xdfff {
				end += 6
			}
		}
	}
}

// decodeUnicodeEscape reads the four hex digits of a "\uXXXX" escape whose
// backslash sits at pos, reproducing _decode_uXXXX's own refusal.
func (s *jsonScanner) decodeUnicodeEscape(pos int) (int, *jsonSyntaxError) {
	invalid := &jsonSyntaxError{Message: `Invalid \uXXXX escape`, Pos: pos}
	if pos+5 > len(s.doc) {
		return 0, invalid
	}
	esc := string(s.doc[pos+1 : pos+5])
	// int(esc, 16) accepts "0x1f"-style spellings only via a prefix the
	// slice cannot carry, but it DOES accept an "x" as the second
	// character of a four-character slice, which the decoder refuses
	// explicitly before parsing.
	if esc[1] == 'x' || esc[1] == 'X' {
		return 0, invalid
	}
	value, err := strconv.ParseInt(esc, 16, 32)
	if err != nil {
		return 0, invalid
	}
	return int(value), nil
}

// scanObject scans an object whose opening brace sits at idx-1.
func (s *jsonScanner) scanObject(idx int) (any, int, *jsonSyntaxError) {
	end := idx
	nextChar, _ := s.at(end)
	if nextChar != '"' {
		if strings.ContainsRune(jsonWhitespace, nextChar) {
			end = s.skipWhitespace(end)
			nextChar, _ = s.at(end)
		}
		if nextChar == '}' {
			return nil, end + 1, nil
		}
		if nextChar != '"' {
			return nil, end, &jsonSyntaxError{
				Message: "Expecting property name enclosed in double quotes",
				Pos:     end,
			}
		}
	}
	end++

	for {
		_, next, err := s.scanString(end)
		if err != nil {
			return nil, next, err
		}
		end = next

		if char, _ := s.at(end); char != ':' {
			end = s.skipWhitespace(end)
			if char, _ := s.at(end); char != ':' {
				return nil, end, &jsonSyntaxError{
					Message: "Expecting ':' delimiter", Pos: end,
				}
			}
		}
		end++
		end = s.skipWhitespace(end)

		_, next, err = s.scanOnce(end)
		if err != nil {
			return nil, next, err
		}
		end = next

		nextChar, present := s.at(end)
		if present && strings.ContainsRune(jsonWhitespace, nextChar) {
			end = s.skipWhitespace(end + 1)
			nextChar, present = s.at(end)
		}
		if !present {
			nextChar = 0
		}
		end++
		if nextChar == '}' {
			break
		}
		if nextChar != ',' {
			return nil, end - 1, &jsonSyntaxError{
				Message: "Expecting ',' delimiter", Pos: end - 1,
			}
		}
		commaPos := end - 1
		end = s.skipWhitespace(end)
		if closing, present := s.at(end); present && closing == '}' {
			return nil, commaPos, &jsonSyntaxError{
				Message: "Illegal trailing comma before end of object",
				Pos:     commaPos,
			}
		}
		nextChar, _ = s.at(end)
		end++
		if nextChar != '"' {
			return nil, end - 1, &jsonSyntaxError{
				Message: "Expecting property name enclosed in double quotes",
				Pos:     end - 1,
			}
		}
	}
	return nil, end, nil
}

// scanArray scans an array whose opening bracket sits at idx-1.
func (s *jsonScanner) scanArray(idx int) (any, int, *jsonSyntaxError) {
	end := idx
	nextChar, present := s.at(end)
	if present && strings.ContainsRune(jsonWhitespace, nextChar) {
		end = s.skipWhitespace(end + 1)
		nextChar, present = s.at(end)
	}
	if present && nextChar == ']' {
		return nil, end + 1, nil
	}

	for {
		_, next, err := s.scanOnce(end)
		if err != nil {
			return nil, next, err
		}
		end = next

		nextChar, present = s.at(end)
		if present && strings.ContainsRune(jsonWhitespace, nextChar) {
			end = s.skipWhitespace(end + 1)
			nextChar, present = s.at(end)
		}
		if !present {
			nextChar = 0
		}
		end++
		if nextChar == ']' {
			break
		}
		if nextChar != ',' {
			return nil, end - 1, &jsonSyntaxError{
				Message: "Expecting ',' delimiter", Pos: end - 1,
			}
		}
		commaPos := end - 1
		end = s.skipWhitespace(end)
		if closing, present := s.at(end); present && closing == ']' {
			return nil, commaPos, &jsonSyntaxError{
				Message: "Illegal trailing comma before end of array",
				Pos:     commaPos,
			}
		}
	}
	return nil, end, nil
}
