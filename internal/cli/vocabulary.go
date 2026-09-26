package cli

import (
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/gitcommit"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/vocabulary"
	"github.com/smm-h/strictcli/go/strictcli"
)

// vocabularyAutoCommit is the --auto-commit flag every vocabulary command
// carries: the files it edits are committed unless the caller opts out.
func vocabularyAutoCommit() strictcli.Flag {
	return strictcli.BoolFlag("auto-commit",
		"Commit the vocabulary file the command edited. Omitted, it commits; pass --no-auto-commit to leave the edit uncommitted",
		strictcli.Optional())
}

func (c *cli) registerVocabulary() {
	group := c.app.Group("vocabulary",
		"Edit this project's vocabulary: the words its pages may use that the English word list does not carry, the terms they may not use ("+layout.TermsRel+"), and the words proposed for acceptance awaiting review ("+layout.ReviewRel+"). The spell check reads selfdoc's built-in baseline and these files, and nothing outside the repository")

	group.Command("accept",
		"Accept a word into "+layout.TermsRel+", in sorted position, with its meaning. Matching is case-insensitive, so one entry accepts every casing. Refuses a word already accepted (as a word or an alias, in the project or selfdoc's built-in baseline), a word a rejected term covers, and a word pending review, which 'selfdoc vocabulary approve' resolves instead",
		c.cmdVocabularyAccept,
		strictcli.WithEffect(strictcli.EffectMutating),
		strictcli.WithArgs(
			strictcli.NewArg("word", "The word to accept, spelled as the project spells it (e.g. 'selfdoc')", strictcli.ArgRequired()),
		),
		strictcli.WithFlags(
			strictcli.StringFlag("meaning", "What the word means in this project, in one sentence. Required: an accepted word without a meaning is a word nobody can check", strictcli.Required()),
			vocabularyAutoCommit(),
		),
	)

	group.Command("reject",
		"Reject a term in "+layout.TermsRel+", in sorted position, with the reason: every page whose prose uses it then fails 'selfdoc check' (VOCAB004). Refuses a term already rejected, and one that would reject an accepted word",
		c.cmdVocabularyReject,
		strictcli.WithEffect(strictcli.EffectMutating),
		strictcli.WithArgs(
			strictcli.NewArg("pattern", "The rejected text (e.g. 'leverage', 'in order to', '-ish'). Matched case-insensitively", strictcli.ArgRequired()),
		),
		strictcli.WithFlags(
			strictcli.StringFlag("kind", "How the pattern matches the text of a page, compared case-insensitively", strictcli.Required(), strictcli.Choices(
				strictcli.Ch(vocabulary.KindWord, "a whole word, with a word boundary on both sides"),
				strictcli.Ch(vocabulary.KindPhrase, "a whole phrase, its words separated by any whitespace"),
				strictcli.Ch(vocabulary.KindSuffix, "the end of a longer word"),
				strictcli.Ch(vocabulary.KindPrefix, "the start of a longer word"),
			)),
			strictcli.StringFlag("reason", "Why the term is rejected, shown beside every place a page uses it", strictcli.Required()),
			vocabularyAutoCommit(),
		),
	)

	group.Command("remove",
		"Remove every entry of "+layout.TermsRel+" whose word or pattern is the given one, compared case-insensitively: an accepted word, a rejected term, or both. It is also how a word both accepted and rejected is resolved: remove it, then accept or reject it again. An entry of selfdoc's built-in baseline is not the project's to remove, and is refused",
		c.cmdVocabularyRemove,
		strictcli.WithEffect(strictcli.EffectMutating),
		strictcli.WithArgs(
			strictcli.NewArg("word", "The accepted word or rejected pattern to remove, compared case-insensitively", strictcli.ArgRequired()),
		),
		strictcli.WithFlags(vocabularyAutoCommit()),
	)

	group.Command("approve",
		"Approve a word pending review: move its entry from "+layout.ReviewRel+" into the accepted words of "+layout.TermsRel+", with the proposed meaning or, with --meaning, a corrected one. Refuses a word that is not pending",
		c.cmdVocabularyApprove,
		strictcli.WithEffect(strictcli.EffectMutating),
		strictcli.WithArgs(
			strictcli.NewArg("word", "The pending word, as review.toml spells it, compared case-insensitively", strictcli.ArgRequired()),
		),
		strictcli.WithFlags(
			strictcli.StringFlag("meaning", "A corrected meaning, accepted instead of the proposed one. Omitted, the proposed meaning is accepted as written", strictcli.Optional()),
			vocabularyAutoCommit(),
		),
	)

	group.Command("drop",
		"Drop a word pending review: delete its entry from "+layout.ReviewRel+" without accepting it, so pages using it keep failing the spell check until the spelling is fixed. Refuses a word that is not pending",
		c.cmdVocabularyDrop,
		strictcli.WithEffect(strictcli.EffectMutating),
		strictcli.WithArgs(
			strictcli.NewArg("word", "The pending word, as review.toml spells it, compared case-insensitively", strictcli.ArgRequired()),
		),
		strictcli.WithFlags(vocabularyAutoCommit()),
	)
}

func (c *cli) cmdVocabularyAccept(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	word := strictcli.Get[string](kwargs, "word")
	meaning := strictcli.Get[string](kwargs, "meaning")
	return c.vocabularyEdit(ctx, kwargs, "accept "+word, func(h *effects.Handle) (vocabulary.Edit, error) {
		return vocabulary.Accept(h, c.dir(), word, meaning)
	})
}

func (c *cli) cmdVocabularyReject(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	pattern := strictcli.Get[string](kwargs, "pattern")
	kind := strictcli.Get[string](kwargs, "kind")
	reason := strictcli.Get[string](kwargs, "reason")
	return c.vocabularyEdit(ctx, kwargs, "reject "+pattern, func(h *effects.Handle) (vocabulary.Edit, error) {
		return vocabulary.Reject(h, c.dir(), pattern, kind, reason)
	})
}

func (c *cli) cmdVocabularyRemove(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	word := strictcli.Get[string](kwargs, "word")
	return c.vocabularyEdit(ctx, kwargs, "remove "+word, func(h *effects.Handle) (vocabulary.Edit, error) {
		return vocabulary.Remove(h, c.dir(), word)
	})
}

func (c *cli) cmdVocabularyApprove(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	word := strictcli.Get[string](kwargs, "word")
	meaning := optString(kwargs, "meaning")
	return c.vocabularyEdit(ctx, kwargs, "approve "+word, func(h *effects.Handle) (vocabulary.Edit, error) {
		return vocabulary.Approve(h, c.dir(), word, meaning)
	})
}

func (c *cli) cmdVocabularyDrop(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	word := strictcli.Get[string](kwargs, "word")
	return c.vocabularyEdit(ctx, kwargs, "drop "+word, func(h *effects.Handle) (vocabulary.Edit, error) {
		return vocabulary.Drop(h, c.dir(), word)
	})
}

// vocabularyEdit runs one vocabulary command: the project must be a selfdoc
// project on this layout, the edit runs through the command's effects handle,
// each change is printed, and the edited files are committed unless the caller
// opted out.
func (c *cli) vocabularyEdit(
	ctx *strictcli.Context, kwargs map[string]any, what string,
	edit func(*effects.Handle) (vocabulary.Edit, error),
) strictcli.Outcome {
	autoCommit := absentMeans(kwargs, "auto_commit", true)
	handle := effects.FromContext(ctx)
	if _, outcome, ok := c.requireConfig(); !ok {
		return outcome
	}
	result, err := edit(handle)
	if err != nil {
		return c.fail(err)
	}
	for _, change := range result.Changes {
		c.println(change)
	}
	if autoCommit {
		if _, _, err := gitcommit.AutoCommit(
			result.Files, "selfdoc vocabulary "+what, c.dir(), handle,
		); err != nil {
			return c.fail(err)
		}
	}
	return strictcli.Exit(0)
}
