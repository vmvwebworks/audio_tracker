package main

import _ "embed"

// The SoundFont is versioned in assets/ and embedded in every build, so the
// executable runs on its own: download, double click, done.
//
//go:embed assets/GeneralUser-GS.sf2
var embeddedSoundFont []byte
