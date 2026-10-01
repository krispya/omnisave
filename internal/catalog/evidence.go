package catalog

// Evidence is what identifies a Game: strong identifiers and fingerprints,
// and weak hints that only describe it. At least one identifier or
// fingerprint is required, and each can belong to only one Game.
type Evidence struct {
	Identifiers  []GameIdentifier  `json:"identifiers,omitempty"`
	Fingerprints []GameFingerprint `json:"fingerprints,omitempty"`
	TitleHint    string            `json:"title_hint,omitempty"`
	PlatformHint string            `json:"platform_hint,omitempty"`
}

// GameIdentifier is an external ID qualified by its namespace, such as
// steam.app, igdb.game, or hasheous.game (FDR-001, decision 3).
type GameIdentifier struct {
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
}

// GameFingerprint identifies exact game content by its hash — crc32, md5,
// sha1, or sha256 — without transferring it.
type GameFingerprint struct {
	Platform  string `json:"platform"`
	Algorithm string `json:"algorithm"`
	Value     string `json:"value"`
}

// ResolutionStatus says whether resolution reused or created a Game.
type ResolutionStatus string

const (
	ResolutionExisting ResolutionStatus = "existing"
	ResolutionCreated  ResolutionStatus = "created"
)

// Resolution is the Game some evidence identifies.
type Resolution struct {
	Game   Game             `json:"game"`
	Status ResolutionStatus `json:"status"`
}
