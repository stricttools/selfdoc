package testproject

// The fixture projects declare a Python source entry, and a declared language
// whose extractor is not linked is a hard error at resolution time -- the
// registry is populated by each extractor package registering itself. The
// binary links every language; a fixture links the one it declares.
//
// A suite whose fixtures declare another language blank-imports that
// language's package the same way.
import _ "github.com/stricttools/selfdoc/internal/extractors/python"
