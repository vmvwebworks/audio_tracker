// Package gp reads Guitar Pro 7/8 (.gp) files into a simplified score model
// suited for playback and tablature display.
package gp

// TicksPerQuarter is the timing resolution used throughout the model.
const TicksPerQuarter = 960

// Song is a parsed score.
type Song struct {
	Title, Artist, Album string
	Tracks               []*Track
	MasterBars           []*MasterBar
}

// Track is one instrument.
type Track struct {
	Index      int
	Name       string
	ShortName  string
	Program    int // General MIDI program
	Percussion bool
	Volume     float64 // 0..1
	Pan        float64 // 0..1, 0.5 is centre
	Staves     []*Staff
	// DrumMap maps a note's InstrumentArticulation index to a MIDI key.
	DrumMap []DrumArticulation
}

// DrumArticulation describes how a percussion note is played and drawn.
type DrumArticulation struct {
	Name      string
	MidiKey   int
	StaffLine int  // vertical position in half staff spaces from the top line
	Cross     bool // x notehead (cymbals, hi-hat)
}

// Staff holds the bars of one staff of a track, one per master bar.
type Staff struct {
	Tuning []int // MIDI pitch per string, lowest string first
	Capo   int
	Bars   []*Bar
}

// MasterBar holds bar properties shared by all tracks.
type MasterBar struct {
	Index        int
	Num, Den     int
	Duration     int // ticks
	RepeatStart  bool
	RepeatEnd    bool
	RepeatCount  int    // total times the repeated section is played
	Alternate    uint32 // bitmask of alternate endings (bit 0 = ending 1)
	Section      string
	TempoChanges []TempoChange
	TripletFeel  bool
}

// TempoChange is a tempo automation within a master bar.
type TempoChange struct {
	Position float64 // fraction of the bar, 0..1
	BPM      float64 // quarter notes per minute
}

// Bar is the content of one staff in one master bar.
type Bar struct {
	Voices [][]*Beat
}

// Beat is a rhythmic event in a voice: a rest, a single note or a chord.
type Beat struct {
	Start    int // ticks from the start of the bar
	Duration int // ticks
	Value    int // 1 = whole, 2 = half, 4 = quarter ...
	Dots     int
	Tuplet   int // tuplet numerator, 0 if none
	Grace    bool
	Velocity int
	Notes    []*Note
}

// Note is a single sounding note.
type Note struct {
	String    int // 0 = lowest string
	Fret      int
	Key       int // MIDI key that sounds
	TieOrigin bool
	TieDest   bool
	Dead      bool
	Ghost     bool
	Accent    bool
	PalmMute  bool
	LetRing   bool
	Drum      int // articulation index for percussion, -1 otherwise
}
