# Vanilla profiles in STS2 v0.107.1 are its save slots. Shared selector/settings stay account-local.
ADAPTER_ID = "sts2.vanilla"
GAME_KEYS = ["steam.app:2868840"]

# Files the game never loads (ADR-021). Top-level *.save.backup files stay:
# the game falls back to them when a save is missing or unreadable.
IGNORED = [
    # History lists skip run backups; each repeats its finished run.
    "saves/history/*.run.backup",
    # The last combat's replay, read only into bug reports.
    "replays/latest.mcr",
    # Interrupted writes and unreadable saves the game has set aside.
    "*.tmp",
    "*.corrupt",
]

def discover(snapshot):
    # Only Steam is verified; other launchers keep the whole save.
    if snapshot.target != "steam":
        return []
    accounts = []
    for root in snapshot.roots:
        if root.name == "steam" and root.parent == "SlayTheSpire2":
            for child in snapshot.directories(root.id):
                if child and len(child) <= 20 and all([digit in "0123456789" for digit in child.elems()]) and int(child) > 0 and int(child) <= 18446744073709551615:
                    accounts.append((root.id, child))
    profiles = []
    for index, account in enumerate(accounts):
        for slot in range(1, 4):
            label = "Profile %d" % slot
            if len(accounts) > 1:
                label += " (account %d)" % (index + 1)
            profiles.append({
                "root": account[0],
                "parent": account[1],
                "directory": "profile%d" % slot,
                "key": str(slot),
                "label": label,
                "location": "sts2-profile",
                # Backups and sidecars belong to the snapshot, but not Steam.
                "cloud": {
                    "account_id": account[1],
                    "files": ["saves/progress.save", "saves/prefs.save", "saves/current_run.save", "saves/current_run_mp.save"],
                    "directories": [{"path": "saves/history", "extension": ".run"}],
                },
            })
    return profiles
