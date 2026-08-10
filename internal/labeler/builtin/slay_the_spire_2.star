# Labels Slay the Spire II revisions from the run state in the save.
#
#   Mid-run:  "Necrobinder A5, Underdocks flr 12, 11/66 HP"
#   Run over: "Necrobinder A4 win, 45 flrs, 1h02m"
#             "Necrobinder A5 died to Decimillipede, Hive flr 23"
#             "Necrobinder A5 abandoned, Hive flr 23"
#
# A snapshot mid-run carries saves/current_run.save; a finished run deletes it
# and appends saves/history/<start_time>.run. A snapshot with neither is a
# fresh profile and stays unnamed. Field knowledge covers run schemas 8-9 and
# save schema 16 (game builds v0.99-v0.107); anything unrecognized degrades to
# a shorter name or none at all.

GAME_KEYS = [
    "steam.app:2868840",
    "debug.slug:slay-the-spire-2",
]

# Identifier tiers and stop words for prettifying constants like
# ENCOUNTER.TERROR_EEL_ELITE into "Terror Eel".
_TIER_SUFFIXES = ["_EVENT_ENCOUNTER", "_WEAK", "_NORMAL", "_ELITE", "_BOSS"]
_SMALL_WORDS = ["of", "the", "and", "in", "to"]

def _pretty(ident):
    """CHARACTER.NECROBINDER -> Necrobinder; ENCOUNTER.TERROR_EEL_ELITE -> Terror Eel."""
    if type(ident) != "string" or not ident or ident == "NONE.NONE":
        return None
    name = ident.split(".", 1)[-1]
    for suffix in _TIER_SUFFIXES:
        if name.endswith(suffix):
            name = name[:len(name) - len(suffix)]
            break
    words = []
    for index, word in enumerate(name.split("_")):
        lower = word.lower()
        if not lower:
            continue
        words.append(lower if index > 0 and lower in _SMALL_WORDS else lower.capitalize())
    return " ".join(words) if words else None

def _floors(history):
    """Floors climbed so far; each act's leading ancient node is floor 0 of that act."""
    total = 0
    for act in history if type(history) == "list" else []:
        if type(act) == "list" and len(act) > 1:
            total += len(act) - 1
    return total

def _act_name(doc):
    """Name of the deepest act reached. Acts are dicts mid-run, strings in history."""
    acts = doc.get("acts")
    if type(acts) != "list" or not acts:
        return None
    index = doc.get("current_act_index")
    if type(index) != "int":
        history = doc.get("map_point_history")
        index = len(history) - 1 if type(history) == "list" and history else 0
    index = min(max(index, 0), len(acts) - 1)
    act = acts[index]
    if type(act) == "dict":
        act = act.get("id")
    return _pretty(act)

def _ascension(doc):
    level = doc.get("ascension")
    return " A%d" % level if type(level) == "int" and level > 0 else ""

def _duration(seconds):
    if type(seconds) != "int" or seconds <= 0:
        return None
    minutes = seconds // 60
    if minutes >= 60:
        # Starlark % has no width flags, so pad the minutes by hand.
        remainder = minutes % 60
        return "%dh%s%dm" % (minutes // 60, "0" if remainder < 10 else "", remainder)
    return "%dm" % minutes

def _player(doc):
    players = doc.get("players")
    if type(players) == "list" and players and type(players[0]) == "dict":
        return players[0]
    return {}

def _character(doc):
    player = _player(doc)
    return _pretty(player.get("character_id") or player.get("character")) or "Unknown"

def _place(doc):
    act = _act_name(doc)
    floors = _floors(doc.get("map_point_history"))
    if act and floors:
        return "%s flr %d" % (act, floors)
    return act

def _after_place(doc):
    place = _place(doc)
    return (", " + place) if place else ""

def _run_over(doc):
    """Label for a finished run: outcome first, then where it ended."""
    who = _character(doc) + _ascension(doc)
    if doc.get("win"):
        name = "%s win, %d flrs" % (who, _floors(doc.get("map_point_history")))
        time = _duration(doc.get("run_time"))
        return (name + ", " + time) if time else name
    # An abandon mid-fight also records the encounter; quitting is still the outcome.
    if doc.get("was_abandoned"):
        return "%s abandoned%s" % (who, _after_place(doc))
    killer = _pretty(doc.get("killed_by_encounter")) or _pretty(doc.get("killed_by_event"))
    if killer:
        return "%s died to %s%s" % (who, killer, _after_place(doc))
    return "%s run over%s" % (who, _after_place(doc))

def _mid_run(doc):
    """Label for a run in progress: who, where, and how close to death."""
    parts = [_character(doc) + _ascension(doc)]
    place = _place(doc)
    if place:
        parts.append(place)
    player = _player(doc)
    hp = player.get("current_hp")
    max_hp = player.get("max_hp")
    if type(hp) == "int" and type(max_hp) == "int" and max_hp > 0:
        parts.append("%d/%d HP" % (hp, max_hp))
    return ", ".join(parts)

def _within(path, profile):
    return path.startswith(profile) or ("/" + profile) in path

def _file_named(snapshot, filename, profile):
    for path in snapshot.paths():
        if profile and not _within(path, profile):
            continue
        if path == filename or path.endswith("/" + filename):
            return path
    return None

def _active_profile(snapshot):
    """The profile directory the game last had open, as a path segment.

    A revision carries the whole cloud save tree, so every profile is present
    at once and none of them is distinguishable by recency — profile.save is
    the game's own record of which one is live. When it is missing or
    unreadable there is nothing to scope by, and the single-profile tree that
    is the common case needs no scoping anyway.
    """
    pointer = _file_named(snapshot, "profile.save", None)
    if not pointer:
        return None
    doc = snapshot.json(pointer)
    if type(doc) != "dict":
        return None
    profile_id = doc.get("last_profile_id")
    if type(profile_id) != "int":
        return None
    return "profile%d/" % profile_id

def label(snapshot):
    # Only the live profile describes what the player just did: an idle one
    # keeps its own current_run.save and history, which would otherwise
    # decide both the branch below and the run picked out of it.
    profile = _active_profile(snapshot)
    current = _file_named(snapshot, "current_run.save", profile)
    if current:
        doc = snapshot.json(current)
        if type(doc) == "dict":
            return _mid_run(doc)
    runs = snapshot.paths("**/history/*.run")
    if profile:
        runs = [path for path in runs if _within(path, profile)]
    # Within one profile the paths differ only in the epoch start time, so
    # the lexicographic maximum is the run that just ended.
    if runs:
        doc = snapshot.json(runs[-1])
        if type(doc) == "dict":
            return _run_over(doc)
    return None
