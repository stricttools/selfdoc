package assembly

import (
	"github.com/stricttools/selfdoc/internal/manifest"
	"github.com/stricttools/selfdoc/internal/util"
	"github.com/stricttools/selfdoc/internal/vocabulary"
)

// PublishedVocabularies reads the vocabulary every manifest document carries,
// under the slug each declares, skipping the project named except: the one
// arriving, whose own document on the site is about to be replaced.
//
// Every document goes through the manifest reader, so one an older selfdoc
// wrote is refused rather than read as a project with no vocabulary.
func PublishedVocabularies(documents []map[string]any, except string) ([]vocabulary.Published, error) {
	var published []vocabulary.Published
	for _, document := range documents {
		slug := util.PythonStrOrEmpty(document["slug"])
		if slug == "" || slug == except {
			continue
		}
		record, err := manifest.Compat(document, "the manifest of "+util.PythonRepr(slug))
		if err != nil {
			return nil, err
		}
		published = append(published, record.Vocabulary.Published(slug))
	}
	return published, nil
}

// checkIncomingVocabulary refuses a project arriving on a site whose other
// projects' vocabularies, or selfdoc's baseline, disagree with its own. The
// project's vocabulary is read from the manifest it publishes; a publish that
// carries no manifest carries no vocabulary, and has nothing to check.
func checkIncomingVocabulary(manifestPath, slug string, peers []vocabulary.Published, action string) error {
	if manifestPath == "" || !isFile(manifestPath) {
		return nil
	}
	record, err := manifest.Load(manifestPath)
	if err != nil {
		return err
	}
	return vocabulary.CheckIncoming(record.Vocabulary.Published(slug), peers, action)
}
