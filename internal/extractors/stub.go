package extractors

// stubExtractor answers for a language selfdoc has no extractor for.
//
// Discovery returns empty and extraction returns an error marker, so a project
// declaring an unsupported language builds -- with the unresolved directives
// visible on the page -- instead of failing at the first directive. The unsupported-source-language
// lint is what reports the declaration itself, at check time.
type stubExtractor struct {
	Base
	language string
}

// NewStub builds the extractor for a language selfdoc has none for.
func NewStub(language string) Extractor {
	stub := &stubExtractor{language: language}
	stub.Base = NewBase(language, nil)
	return stub
}

// Detect reports that no directory is a project in an unsupported language:
// detection is by marker file, and selfdoc knows none for this language.
func (s *stubExtractor) Detect(string) bool { return false }

// Extract renders the marker naming the language selfdoc cannot read.
func (s *stubExtractor) Extract(
	_ string,
	_ map[string]string,
	_ []string,
	_ []string,
	_ string,
) (string, error) {
	return FormatError("no extractor for '" + s.language + "'"), nil
}
