# Labels Undertale revisions with the game's own save-screen line.
#
#   "ALMA LV 3, Ruins - Mouse Hole, 26:13"
#
# The in-game menu draws Name, LOVE, and play time from undertale.ini's
# [General] section and resolves Room through scr_roomname; file0 carries the
# same fields at fixed line positions (1 name, 2 LOVE, 548 room, 549 time).
# Prefer the ini the menu reads, fall back to file0, and stay unnamed when
# neither is present and readable.

GAME_KEYS = [
    "steam.app:391540",
    "debug.slug:undertale",
]

# Save-point rooms by the PC build's room index, named as the save screen
# names them. Saving only happens at a save point, so a save's room is nearly
# always one of these; any other room degrades to its area below.
_SAVE_ROOMS = {
    6: "Ruins - Entrance",
    12: "Ruins - Leaf Pile",
    18: "Ruins - Mouse Hole",
    31: "Ruins - Home",
    46: "Snowdin - Box Road",
    56: "Snowdin - Spaghetti",
    61: "Snowdin - Dog House",
    68: "Snowdin - Town",
    83: "Waterfall - Checkpoint",
    86: "Waterfall - Hallway",
    94: "Waterfall - Crystal",
    110: "Waterfall - Bridge",
    114: "Waterfall - Trash Zone",
    116: "Waterfall - Quiet Area",
    128: "Waterfall - Temmie Village",
    139: "Hotland - Laboratory Entrance",
    145: "Hotland - Magma Chamber",
    155: "Hotland - Core View",
    164: "Hotland - Bad Opinion Zone",
    176: "Hotland - Spider Entrance",
    183: "Hotland - Hotel Lobby",
    196: "Hotland - Core Branch",
    211: "Hotland - Core End",
    216: "Castle Elevator",
    219: "New Home",
    231: "Last Corridor",
    232: "Throne Entrance",
    235: "Throne Room",
    236: "The End",
    246: "True Laboratory",
    251: "True Lab - Bedroom",
}

_AREAS = [
    (4, 43, "Ruins"),
    (44, 81, "Snowdin"),
    (82, 135, "Waterfall"),
    (136, 215, "Hotland"),
    (216, 243, "New Home"),
    (244, 265, "True Lab"),
]

def _file_named(snapshot, filename):
    for path in snapshot.paths():
        if path == filename or path.endswith("/" + filename):
            return path
    return None

def _int(value):
    """GameMaker writes numbers as "18.000000"; the fraction is always zero."""
    if type(value) != "string":
        return None
    value = value.strip().split(".")[0]
    if not value.isdigit():
        return None
    return int(value)

def _ini_general(text):
    """The [General] section of a GameMaker ini: key="value" lines."""
    fields = {}
    section = ""
    for line in text.split("\n"):
        line = line.strip()
        if line.startswith("[") and line.endswith("]"):
            section = line[1:-1].lower()
        elif section == "general" and "=" in line:
            key, value = line.split("=", 1)
            fields[key.strip().lower()] = value.strip().strip('"')
    return fields

def _from_ini(snapshot):
    path = _file_named(snapshot, "undertale.ini")
    if not path:
        return None
    text = snapshot.text(path)
    if type(text) != "string":
        return None
    fields = _ini_general(text)
    name = fields.get("name", "").strip()
    if not name:
        return None
    return {
        "name": name,
        "love": _int(fields.get("love")),
        "room": _int(fields.get("room")),
        "time": _int(fields.get("time")),
    }

def _from_file0(snapshot):
    path = _file_named(snapshot, "file0")
    if not path:
        return None
    text = snapshot.text(path)
    if type(text) != "string":
        return None
    lines = [line.strip() for line in text.split("\n")]
    if len(lines) < 549:
        return None
    name = lines[0]
    if not name:
        return None
    return {
        "name": name,
        "love": _int(lines[1]),
        "room": _int(lines[547]),
        "time": _int(lines[548]),
    }

def _place(room):
    if room in _SAVE_ROOMS:
        return _SAVE_ROOMS[room]
    for first, last, area in _AREAS:
        if type(room) == "int" and first <= room and room <= last:
            return area
    return None

def _clock(frames):
    """Play time as the save screen shows it: frames at 30fps, rounded to m:ss."""
    if type(frames) != "int" or frames <= 0:
        return None
    minutes = frames // 1800
    seconds = ((frames % 1800) * 60 + 900) // 1800
    if seconds == 60:
        seconds = 59
    return "%d:%s%d" % (minutes, "0" if seconds < 10 else "", seconds)

def label(snapshot):
    save = _from_ini(snapshot) or _from_file0(snapshot)
    if not save:
        return None
    parts = [save["name"]]
    if type(save["love"]) == "int":
        parts[0] += " LV %d" % save["love"]
    place = _place(save["room"])
    if place:
        parts.append(place)
    clock = _clock(save["time"])
    if clock:
        parts.append(clock)
    return ", ".join(parts)
