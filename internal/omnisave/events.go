package omnisave

// Event types published on the server's change feed. The publisher and
// every consumer share these names, so a renamed event is a compile-time
// change on both sides rather than a filter that silently stops matching.
const (
	// LibraryChangedEvent announces movement in the library — a commit, a
	// restore, a fork, a deletion — anything that changes what a device
	// would sync against.
	LibraryChangedEvent = "library.changed"
	// AccessChangedEvent announces a pending access request appearing,
	// resolving, or expiring.
	AccessChangedEvent = "access.changed"
	// DevicesChangedEvent announces a change in device presence — a device
	// starting or stopping play of a game. Its own scope on purpose: a
	// playing report must not read as library movement, or every watching
	// device would answer another device's status report with a full
	// reconcile pass.
	DevicesChangedEvent = "devices.changed"
)
