package saveprofile

// Outcome names what became of one save-location rule during a resolve pass.
// Resolving keeps only the files it found; every other outcome is invisible in
// the result, which is what leaves a game reading "no save available" with no
// way to tell an absent community rule from a rule pointing somewhere this
// device cannot look.
type Outcome string

const (
	// OutcomeFound means the rule located regular files.
	OutcomeFound Outcome = "found"
	// OutcomeEmpty means the rule's location exists but holds no regular files.
	OutcomeEmpty Outcome = "empty"
	// OutcomeMissing means the expanded path names nothing on this device.
	OutcomeMissing Outcome = "missing"
	// OutcomeInapplicable means the rule's platform or store constraint
	// excluded it from this environment before any path was searched.
	OutcomeInapplicable Outcome = "inapplicable"
	// OutcomeUnexpandable means the template kept a placeholder this
	// environment cannot fill, or resolved to a path that is not absolute.
	OutcomeUnexpandable Outcome = "unexpandable"
	// OutcomeUnreadable means the location exists but could not be read.
	OutcomeUnreadable Outcome = "unreadable"
	// OutcomeLinked means the location is a symbolic link, which discovery
	// never follows.
	OutcomeLinked Outcome = "linked"
	// OutcomeAmbiguous means a literal rule folds onto several on-disk
	// spellings with no exact one among them, so none can be chosen.
	OutcomeAmbiguous Outcome = "ambiguous"
)

// RuleOutcome is what one rule did during a resolve pass. It is recorded by
// the resolve that produced the saves, never by a second pass, so it can only
// ever describe what actually happened.
type RuleOutcome struct {
	Rule    Rule
	Outcome Outcome
	// Path is the expanded absolute path the rule searched, empty when the
	// rule never got that far.
	Path string
	// Files and Bytes count everything this rule located, including files a
	// previous rule had already contributed to the save.
	Files int
	Bytes int64
}

// severity orders outcomes so a rule matching several paths reports the most
// informative one: files found outrank trouble, and trouble outranks absence.
func severity(outcome Outcome) int {
	switch outcome {
	case OutcomeFound:
		return 5
	case OutcomeUnreadable:
		return 4
	case OutcomeAmbiguous:
		return 3
	case OutcomeLinked:
		return 2
	case OutcomeEmpty:
		return 1
	default:
		return 0
	}
}

// outranks reports whether a candidate outcome should replace the one held.
func outranks(candidate, held Outcome) bool {
	return severity(candidate) > severity(held)
}
