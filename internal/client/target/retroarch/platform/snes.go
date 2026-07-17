package platform

// SNES returns the RetroArch profile for Super Nintendo saves.
func SNES() Profile {
	return Profile{
		ID:             "snes",
		PlaylistNames:  []string{"Nintendo - Super Nintendo Entertainment System.lpl"},
		SaveExtensions: []string{".srm"},
	}
}
