// Package savesync is the client's Save Sync domain (FDR-005). One pass
// registers this Device's tracked games with the Library, binds each Local
// Save to an Omnisave, and decides for every bound save whether to push,
// pull, do nothing, or wait on a lineage decision. It then carries that
// decision out: forks and jumps, the preservation that keeps both sides of a
// divergence recoverable, path-format migration holds, placement onto a
// Device with no save, store registration after a placement, achievement
// reports, and the process sweep behind playing presence and deferred pulls.
//
// The package owns the decisions and the tracking state they leave behind.
// Everything else arrives through ports:
//
//   - Server is the authority holding the Library and every lineage
//     (*remote.Client in the client binary).
//   - Adapters knows this Device's targets, games, and saves
//     (*client.Scanner).
//   - Reporter hears what the pass decided, in domain terms, and presents it
//     however the caller shows a pass (*tui.TrackReport).
//   - Prompts asks a person the questions only a person can answer. A
//     question without a prompt leaves its save waiting, reported, and never
//     blocks the pass.
//
// Pass is the whole of one sync or watch pass. A run that interleaves its own
// interaction — track, which asks which games to protect before binding
// anything — calls its parts in order: ReconcileDeletedGames, SyncTracking,
// then Reconcile.
package savesync
